package authz

import (
	"errors"
	"reflect"
	"testing"
)

// Einkauf(1) enthält Nord(2), Nord enthält Azubis(3). Admins(9) ist Systemrolle.
func testGraph() *Graph {
	return NewGraph([]*RoleInfo{
		{ID: 1, Name: "Einkauf", Permissions: []string{PermTablesDefine}},
		{ID: 2, Name: "Nord", Permissions: []string{PermUsersManage}},
		{ID: 3, Name: "Azubis"},
		{ID: 4, Name: "Lager"},
		{ID: 9, Name: "Administratoren", System: true},
	}, [][2]int64{{1, 2}, {2, 3}})
}

func TestResolveInheritsUpward(t *testing.T) {
	g := testGraph()

	a := g.Resolve([]int64{3}) // Azubi
	if !a.Has(PermTablesDefine) || !a.Has(PermUsersManage) {
		t.Fatalf("Azubi erbt nicht alle Rechte von oben: %v", a.Permissions)
	}
	if got := g.PathNames(a.Permissions[PermTablesDefine]); !reflect.DeepEqual(got, []string{"Azubis", "Nord", "Einkauf"}) {
		t.Fatalf("Pfad %v", got)
	}

	a = g.Resolve([]int64{1}) // nur Einkauf
	if a.Has(PermUsersManage) {
		t.Fatal("Recht der tieferen Rolle wurde nach oben vererbt")
	}
	if _, ok := a.Roles[2]; ok {
		t.Fatal("Mitglied von Einkauf gilt fälschlich als Mitglied von Nord")
	}
}

func TestResolveSystemRoleHasAll(t *testing.T) {
	g := testGraph()
	a := g.Resolve([]int64{9})
	for _, p := range AllPermissions {
		if !a.Has(p.Code) {
			t.Fatalf("Administratoren fehlt %s", p.Code)
		}
	}
	if !a.IsAdmin(g) || g.Resolve([]int64{4}).IsAdmin(g) {
		t.Fatal("IsAdmin falsch")
	}
}

func TestCheckInclude(t *testing.T) {
	g := testGraph()
	cases := []struct {
		parent, child int64
		want          error
	}{
		{4, 4, ErrSelfInclude},
		{3, 1, ErrCycle},   // Einkauf in Azubis: Azubis liegt schon in Einkauf
		{2, 1, ErrCycle},   // direkter Rückweg
		{3, 4, ErrTooDeep}, // Lager unter Azubis wäre Ebene 4
		{4, 1, ErrTooDeep}, // Einkauf-Kette unter Lager wäre Ebene 4
		{1, 4, nil},        // Lager direkt in Einkauf: 2 Ebenen
		{2, 4, nil},        // Lager in Nord: 3 Ebenen
	}
	for _, c := range cases {
		if err := g.CheckInclude(c.parent, c.child); !errors.Is(err, c.want) {
			t.Errorf("CheckInclude(%d, %d) = %v, erwartet %v", c.parent, c.child, err, c.want)
		}
	}
}
