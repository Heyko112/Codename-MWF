package httpserver

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

// Benutzer und Rollen für Widgets (Schritt E1). Ein Widget sieht nur, was es im Manifest
// mit "users": "read" anfordert. Versteckte Rollen und Benutzer, Service-User und
// gesperrte Konten bleiben unsichtbar, wie in den Auswahllisten von MWF.

// visibleRoleNames liefert die Namen der wirksamen, nicht versteckten Rollen (auch über enthaltene Rollen), sortiert.
func visibleRoleNames(g *authz.Graph, a *authz.Access) []string {
	names := []string{}
	if g == nil || a == nil {
		return names
	}
	for id := range a.Roles {
		if r := g.Roles[id]; r != nil && !r.Hidden {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	return names
}

type apiUser struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Roles       []string `json:"roles"`
}

type apiUsersParams struct {
	Role   string `json:"role"`
	Search string `json:"search"`
}

// widgetUserList filtert und beschreibt die Benutzer für ein Widget.
func widgetUserList(users []*auth.User, g *authz.Graph, direct map[int64][]int64, p apiUsersParams) []apiUser {
	search := strings.ToLower(strings.TrimSpace(p.Search))
	out := []apiUser{}
	for _, u := range users {
		if u.IsHidden || u.IsService || !u.IsActive {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(u.Username), search) && !strings.Contains(strings.ToLower(u.DisplayName), search) {
			continue
		}
		roles := visibleRoleNames(g, g.Resolve(direct[u.ID]))
		if p.Role != "" && !containsFold(roles, p.Role) {
			continue
		}
		out = append(out, apiUser{Username: u.Username, DisplayName: u.DisplayName, Roles: roles})
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

var errAPINoUsers = apiErr(http.StatusForbidden, "forbidden", "Das Widget hat keinen Zugriff auf Benutzer und Rollen angefordert (manifest.json: \"users\": \"read\").")

func (s *Server) apiUsers(r *http.Request, wd *widgets.Widget, raw json.RawMessage) (any, error) {
	if widgets.ParseManifest(wd.Manifest).Users != "read" {
		return nil, errAPINoUsers
	}
	var p apiUsersParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errAPIBadParams
	}
	ctx := r.Context()
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, err
	}
	g, err := s.roles.Graph(ctx)
	if err != nil {
		return nil, err
	}
	direct, err := s.roles.AllUserRoleIDs(ctx)
	if err != nil {
		return nil, err
	}
	return widgetUserList(users, g, direct, p), nil
}

type apiRole struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) apiRoles(r *http.Request, wd *widgets.Widget) (any, error) {
	if widgets.ParseManifest(wd.Manifest).Users != "read" {
		return nil, errAPINoUsers
	}
	roles, err := s.roles.ListRoles(r.Context())
	if err != nil {
		return nil, err
	}
	out := []apiRole{}
	for _, ro := range roles {
		if !ro.Hidden {
			out = append(out, apiRole{Name: ro.Name, Description: ro.Description})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
