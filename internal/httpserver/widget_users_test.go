package httpserver

import (
	"reflect"
	"testing"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
)

func TestWidgetUserList(t *testing.T) {
	// HR (1) liegt in Mitarbeiter (2), Service (3) ist versteckt
	g := authz.NewGraph([]*authz.RoleInfo{
		{ID: 1, Name: "HR"}, {ID: 2, Name: "Mitarbeiter"}, {ID: 3, Name: "Service", Hidden: true},
	}, [][2]int64{{2, 1}})
	users := []*auth.User{
		{ID: 1, Username: "anna", DisplayName: "Anna Berg", IsActive: true},
		{ID: 2, Username: "bert", DisplayName: "Bert Kurz", IsActive: true},
		{ID: 3, Username: "geheim", IsActive: true, IsHidden: true},
		{ID: 4, Username: "dienst", IsActive: true, IsService: true},
		{ID: 5, Username: "alt", IsActive: false},
	}
	direct := map[int64][]int64{1: {1, 3}, 2: {2}, 3: {1}, 4: {1}, 5: {1}}

	all := widgetUserList(users, g, direct, apiUsersParams{})
	want := []apiUser{
		{Username: "anna", DisplayName: "Anna Berg", Roles: []string{"HR", "Mitarbeiter"}},
		{Username: "bert", DisplayName: "Bert Kurz", Roles: []string{"Mitarbeiter"}},
	}
	if !reflect.DeepEqual(all, want) {
		t.Fatalf("%+v", all)
	}
	if got := widgetUserList(users, g, direct, apiUsersParams{Role: "hr"}); len(got) != 1 || got[0].Username != "anna" {
		t.Fatalf("Rolle: %+v", got)
	}
	if got := widgetUserList(users, g, direct, apiUsersParams{Role: "Service"}); len(got) != 0 {
		t.Fatalf("versteckte Rolle: %+v", got)
	}
	if got := widgetUserList(users, g, direct, apiUsersParams{Search: "KURZ"}); len(got) != 1 || got[0].Username != "bert" {
		t.Fatalf("Suche: %+v", got)
	}
}
