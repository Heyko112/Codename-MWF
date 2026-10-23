package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
)

const msgSystemInclude = "Nur Mitglieder der Rolle „Administratoren“ dürfen diese Rolle verschachteln."

// roleRow ist eine Zeile der Rollenliste.
type roleRow struct {
	*authz.Role
	PermLabels []string
	Parents    []string
	Children   []string
}

func (s *Server) adminRoles(w http.ResponseWriter, r *http.Request) {
	s.renderAdminRoles(w, r, http.StatusOK, nil)
}

func (s *Server) renderAdminRoles(w http.ResponseWriter, r *http.Request, status int, form map[string]any) {
	ctx := r.Context()
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	g, err := s.roles.Graph(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	rows := make([]roleRow, len(roles))
	for i, role := range roles {
		row := roleRow{Role: role, Parents: g.PathNames(g.Parents(role.ID)), Children: g.PathNames(g.Children(role.ID))}
		if role.System {
			row.PermLabels = []string{"alle Rechte"}
		}
		for _, p := range role.Permissions {
			row.PermLabels = append(row.PermLabels, authz.PermissionLabel(p))
		}
		rows[i] = row
	}
	s.render(w, r, status, "admin_roles", map[string]any{
		"User":  CurrentUser(ctx),
		"Roles": rows,
		"Flash": s.sessions.PopString(ctx, sessFlash),
		"Form":  form,
	})
}

func roleFormValues(r *http.Request) (name, description string, hidden bool, problem string) {
	name = strings.TrimSpace(r.PostFormValue("name"))
	description = strings.TrimSpace(r.PostFormValue("description"))
	hidden = r.PostFormValue("is_hidden") == "on"
	switch {
	case name == "" || utf8.RuneCountInString(name) > 64:
		problem = "Der Rollenname muss 1 bis 64 Zeichen lang sein."
	case utf8.RuneCountInString(description) > 255:
		problem = "Die Beschreibung darf höchstens 255 Zeichen lang sein."
	}
	return
}

func (s *Server) adminCreateRole(w http.ResponseWriter, r *http.Request) {
	name, description, hidden, problem := roleFormValues(r)
	form := map[string]any{"Name": name, "Description": description, "IsHidden": hidden}
	if problem != "" {
		form["Error"] = problem
		s.renderAdminRoles(w, r, http.StatusUnprocessableEntity, form)
		return
	}
	id, err := s.roles.CreateRole(r.Context(), name, description, hidden)
	if errors.Is(err, authz.ErrNameTaken) {
		form["Error"] = err.Error()
		s.renderAdminRoles(w, r, http.StatusUnprocessableEntity, form)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "role_created", name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Rolle „"+name+"“ angelegt. Lege jetzt Rechte und Verschachtelung fest.", roleURL(id))
}

func roleURL(id int64) string { return "/admin/rollen/" + strconv.FormatInt(id, 10) }

// loadRole liest die Rolle aus dem Pfad oder antwortet mit 404.
func (s *Server) loadRole(w http.ResponseWriter, r *http.Request) (*authz.Role, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return nil, false
	}
	role, err := s.roles.RoleByID(r.Context(), id)
	if errors.Is(err, authz.ErrNotFound) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, false
	}
	return role, true
}

type namedRole struct {
	ID   int64
	Name string
}

func (s *Server) adminRoleDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	role, ok := s.loadRole(w, r)
	if !ok {
		return
	}
	g, err := s.roles.Graph(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	named := func(ids []int64) []namedRole {
		out := make([]namedRole, 0, len(ids))
		for _, id := range ids {
			out = append(out, namedRole{ID: id, Name: g.Roles[id].Name})
		}
		return out
	}
	// Rollen, die man noch hineinlegen kann, ohne Kreis und ohne mehr als 3 Ebenen
	var candidates []namedRole
	all, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	isChild := map[int64]bool{}
	for _, c := range g.Children(role.ID) {
		isChild[c] = true
	}
	for _, other := range all {
		if !isChild[other.ID] && g.CheckInclude(role.ID, other.ID) == nil {
			candidates = append(candidates, namedRole{ID: other.ID, Name: other.Name})
		}
	}

	members, err := s.users.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	memberships, err := s.roles.AllUserRoleIDs(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var direct []*auth.User
	for _, u := range members {
		for _, rid := range memberships[u.ID] {
			if rid == role.ID {
				direct = append(direct, u)
			}
		}
	}

	has := map[string]bool{}
	for _, p := range role.Permissions {
		has[p] = true
	}
	s.render(w, r, http.StatusOK, "admin_role", map[string]any{
		"User":       CurrentUser(ctx),
		"Role":       role,
		"AllPerms":   authz.AllPermissions,
		"Has":        has,
		"Parents":    named(g.Parents(role.ID)),
		"Children":   named(g.Children(role.ID)),
		"Candidates": candidates,
		"Members":    direct,
		"MaxDepth":   authz.MaxDepth,
		"Flash":      s.sessions.PopString(ctx, sessFlash),
	})
}

func (s *Server) adminUpdateRole(w http.ResponseWriter, r *http.Request) {
	role, ok := s.loadRole(w, r)
	if !ok {
		return
	}
	name, description, hidden, problem := roleFormValues(r)
	if role.System {
		name = role.Name
		problem = ""
	}
	if problem != "" {
		s.flashRedirect(w, r, problem, roleURL(role.ID))
		return
	}
	err := s.roles.UpdateRole(r.Context(), role.ID, name, description, hidden)
	if errors.Is(err, authz.ErrNameTaken) {
		s.flashRedirect(w, r, err.Error(), roleURL(role.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "role_updated", name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Rolle „"+name+"“ gespeichert.", roleURL(role.ID))
}

func (s *Server) adminSetRolePerms(w http.ResponseWriter, r *http.Request) {
	role, ok := s.loadRole(w, r)
	if !ok {
		return
	}
	if role.System {
		s.flashRedirect(w, r, "„Administratoren“ hat immer alle Rechte.", roleURL(role.ID))
		return
	}
	r.ParseForm()
	perms := r.PostForm["perm"]
	err := s.roles.SetPermissions(r.Context(), role.ID, perms)
	if errors.Is(err, authz.ErrUnknownPerm) {
		s.flashRedirect(w, r, "Unbekanntes Recht.", roleURL(role.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	labels := make([]string, len(perms))
	for i, p := range perms {
		labels[i] = authz.PermissionLabel(p)
	}
	s.auditDetail(r, 0, "role_permissions_changed", role.Name+" durch "+CurrentUser(r.Context()).Username+": "+strings.Join(labels, ", "))
	s.flashRedirect(w, r, "Rechte von „"+role.Name+"“ gespeichert.", roleURL(role.ID))
}

// systemGuard verhindert, dass jemand ohne Administratoren-Rolle die Systemrolle verschachtelt.
func (s *Server) systemGuard(w http.ResponseWriter, r *http.Request, ids ...int64) bool {
	g, acc := CurrentAccess(r.Context())
	for _, id := range ids {
		if role := g.Roles[id]; role != nil && role.System && !acc.IsAdmin(g) {
			s.forbidden(w, r, msgSystemInclude)
			return false
		}
	}
	return true
}

func (s *Server) adminAddInclude(w http.ResponseWriter, r *http.Request) {
	role, ok := s.loadRole(w, r)
	if !ok {
		return
	}
	child, err := strconv.ParseInt(r.PostFormValue("child_id"), 10, 64)
	if err != nil {
		s.flashRedirect(w, r, "Bitte eine Rolle auswählen.", roleURL(role.ID))
		return
	}
	if !s.systemGuard(w, r, role.ID, child) {
		return
	}
	err = s.roles.AddInclude(r.Context(), role.ID, child)
	if errors.Is(err, authz.ErrCycle) || errors.Is(err, authz.ErrTooDeep) || errors.Is(err, authz.ErrSelfInclude) || errors.Is(err, authz.ErrNotFound) {
		s.flashRedirect(w, r, err.Error(), roleURL(role.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	g, _ := CurrentAccess(r.Context())
	childName := strings.Join(g.PathNames([]int64{child}), "")
	s.auditDetail(r, 0, "role_include_added", childName+" in "+role.Name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "„"+childName+"“ liegt jetzt in „"+role.Name+"“. Ihre Mitglieder erben die Rechte von „"+role.Name+"“.", roleURL(role.ID))
}

func (s *Server) adminRemoveInclude(w http.ResponseWriter, r *http.Request) {
	role, ok := s.loadRole(w, r)
	if !ok {
		return
	}
	child, ok := pathID(r, "child")
	if !ok {
		s.notFound(w, r)
		return
	}
	if !s.systemGuard(w, r, role.ID, child) {
		return
	}
	if err := s.roles.RemoveInclude(r.Context(), role.ID, child); err != nil {
		s.serverError(w, r, err)
		return
	}
	g, _ := CurrentAccess(r.Context())
	childName := strings.Join(g.PathNames([]int64{child}), "")
	s.auditDetail(r, 0, "role_include_removed", childName+" aus "+role.Name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "„"+childName+"“ liegt nicht mehr in „"+role.Name+"“.", roleURL(role.ID))
}

func (s *Server) adminDeleteRole(w http.ResponseWriter, r *http.Request) {
	role, ok := s.loadRole(w, r)
	if !ok {
		return
	}
	err := s.roles.DeleteRole(r.Context(), role.ID)
	if errors.Is(err, authz.ErrSystemRole) {
		s.flashRedirect(w, r, err.Error(), roleURL(role.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "role_deleted", role.Name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Rolle „"+role.Name+"“ gelöscht.", "/admin/rollen")
}
