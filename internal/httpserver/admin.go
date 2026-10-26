package httpserver

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
)

const (
	sessFlash    = "flash"
	sessFlashErr = "flash_error"
)

// flashRedirect merkt sich eine Meldung für die nächste Seite und leitet um.
func (s *Server) flashRedirect(w http.ResponseWriter, r *http.Request, msg, to string) {
	s.sessions.Put(r.Context(), sessFlash, msg)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// errorRedirect merkt sich eine Fehlermeldung für die nächste Seite und leitet um.
func (s *Server) errorRedirect(w http.ResponseWriter, r *http.Request, msg, to string) {
	s.sessions.Put(r.Context(), sessFlashErr, msg)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil
}

// formIDs liest alle Werte eines Formularfelds als IDs.
func formIDs(r *http.Request, name string) []int64 {
	r.ParseForm()
	var ids []int64
	for _, v := range r.PostForm[name] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// forbidden zeigt die 403-Seite mit einer Begründung.
func (s *Server) forbidden(w http.ResponseWriter, r *http.Request, msg string) {
	s.render(w, r, http.StatusForbidden, "error", map[string]any{
		"User": CurrentUser(r.Context()), "Status": "403", "Message": msg,
	})
}

// touchesSystemRole meldet, ob sich zwischen before und after die Mitgliedschaft in der Systemrolle ändert.
func touchesSystemRole(g *authz.Graph, before, after []int64) bool {
	in := func(ids []int64) bool {
		for _, id := range ids {
			if r := g.Roles[id]; r != nil && r.System {
				return true
			}
		}
		return false
	}
	return in(before) != in(after)
}

// guardAdminTarget verhindert, dass jemand ohne Administratoren-Rolle einen Administrator ändert oder sperrt.
func (s *Server) guardAdminTarget(w http.ResponseWriter, r *http.Request, targetID int64) bool {
	g, acc := CurrentAccess(r.Context())
	if acc.IsAdmin(g) {
		return true
	}
	tg, targetAcc, err := s.roles.Access(r.Context(), targetID)
	if err != nil {
		s.serverError(w, r, err)
		return false
	}
	if targetAcc.IsAdmin(tg) {
		s.forbidden(w, r, "Nur Mitglieder der Rolle „Administratoren“ dürfen Administratoren ändern oder sperren.")
		return false
	}
	return true
}

const msgOnlyAdmins = "Nur Mitglieder der Rolle „Administratoren“ dürfen diese Rolle vergeben oder entziehen."

// userRow ist eine Zeile der Benutzerliste.
type userRow struct {
	*auth.User
	Roles []string
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	s.renderAdminUsers(w, r, http.StatusOK, nil)
}

// renderAdminUsers zeigt die Liste. form enthält bei einem Fehler die eingegebenen Werte.
func (s *Server) renderAdminUsers(w http.ResponseWriter, r *http.Request, status int, form map[string]any) {
	ctx := r.Context()
	users, err := s.users.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	g, _ := CurrentAccess(ctx)
	memberships, err := s.roles.AllUserRoleIDs(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	showAll := r.URL.Query().Get("alle") == "1"
	var rows []userRow
	hiddenCount := 0
	for _, u := range users {
		if u.IsHidden && !showAll {
			hiddenCount++
			continue
		}
		rows = append(rows, userRow{User: u, Roles: g.PathNames(memberships[u.ID])})
	}
	selected, _ := form["Roles"].(map[int64]bool)
	s.render(w, r, status, "admin_users", map[string]any{
		"Selected":    selected,
		"User":        CurrentUser(ctx),
		"Users":       rows,
		"AllRoles":    roles,
		"ShowAll":     showAll,
		"HiddenCount": hiddenCount,
		"Flash":       s.sessions.PopString(ctx, sessFlash),
		"Form":        form,
		"MinLength":   auth.MinPasswordLen,
	})
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	admin := CurrentUser(ctx)
	g, acc := CurrentAccess(ctx)
	username := strings.TrimSpace(r.PostFormValue("username"))
	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	password := r.PostFormValue("password")
	hidden := r.PostFormValue("is_hidden") == "on"
	service := r.PostFormValue("is_service") == "on"
	roleIDs := formIDs(r, "role")

	selected := map[int64]bool{}
	for _, id := range roleIDs {
		selected[id] = true
	}
	form := map[string]any{"Username": username, "DisplayName": displayName, "IsHidden": hidden, "IsService": service, "Roles": selected}
	fail := func(msg string) {
		form["Error"] = msg
		s.renderAdminUsers(w, r, http.StatusUnprocessableEntity, form)
	}
	switch {
	case username == "" || utf8.RuneCountInString(username) > 64:
		fail("Der Benutzername muss 1 bis 64 Zeichen lang sein.")
		return
	case utf8.RuneCountInString(displayName) > 128:
		fail("Der Anzeigename darf höchstens 128 Zeichen lang sein.")
		return
	case !service && utf8.RuneCountInString(password) < auth.MinPasswordLen:
		fail("Das Passwort muss mindestens " + strconv.Itoa(auth.MinPasswordLen) + " Zeichen lang sein.")
		return
	case touchesSystemRole(g, nil, roleIDs) && !acc.IsAdmin(g):
		fail(msgOnlyAdmins)
		return
	}

	var hash string
	var err error
	if service {
		hash, err = auth.UnusablePasswordHash()
	} else {
		hash, err = auth.HashPassword(password)
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	id, err := s.users.Create(ctx, username, displayName, hash, hidden, service)
	if errors.Is(err, auth.ErrUsernameTaken) {
		fail("Den Benutzernamen „" + username + "“ gibt es schon.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(roleIDs) > 0 {
		if err := s.roles.SetUserRoles(ctx, id, roleIDs); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	s.auditDetail(r, id, "user_created", "durch "+admin.Username)
	msg := "Benutzer „" + username + "“ angelegt. Die 2FA wird beim ersten Login eingerichtet."
	if service {
		msg = "Service-User „" + username + "“ angelegt. Kein Web-Login möglich."
	}
	s.flashRedirect(w, r, msg, "/admin/benutzer")
}

// permRow ist eine Zeile in „Warum hat X Zugriff?“.
type permRow struct {
	Label string
	Path  []string
}

func (s *Server) adminUserDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}
	target, err := s.users.ByID(ctx, id)
	if errors.Is(err, auth.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	g, targetAccess, err := s.roles.Access(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	direct, err := s.roles.UserRoleIDs(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	directSet := map[int64]bool{}
	for _, rid := range direct {
		directSet[rid] = true
	}

	var perms []permRow
	for _, p := range authz.AllPermissions {
		if path, ok := targetAccess.Permissions[p.Code]; ok {
			perms = append(perms, permRow{Label: p.Label, Path: g.PathNames(path)})
		}
	}
	var effective []permRow
	for rid, path := range targetAccess.Roles {
		if !directSet[rid] {
			effective = append(effective, permRow{Label: g.Roles[rid].Name, Path: g.PathNames(path)})
		}
	}
	sort.Slice(effective, func(i, j int) bool { return effective[i].Label < effective[j].Label })

	s.render(w, r, http.StatusOK, "admin_user", map[string]any{
		"User":     CurrentUser(ctx),
		"Target":   target,
		"AllRoles": roles,
		"Direct":   directSet,
		"Indirect": effective,
		"Perms":    perms,
		"IsSelf":   target.ID == CurrentUser(ctx).ID,
		"Flash":    s.sessions.PopString(ctx, sessFlash),
	})
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}
	target, err := s.users.ByID(ctx, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !s.guardAdminTarget(w, r, id) {
		return
	}
	back := "/admin/benutzer/" + strconv.FormatInt(id, 10)
	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	hidden := r.PostFormValue("is_hidden") == "on"
	service := r.PostFormValue("is_service") == "on"
	if utf8.RuneCountInString(displayName) > 128 {
		s.flashRedirect(w, r, "Der Anzeigename darf höchstens 128 Zeichen lang sein.", back)
		return
	}
	if service && id == CurrentUser(ctx).ID {
		s.flashRedirect(w, r, "Du kannst dich nicht selbst zum Service-User machen, sonst sperrst du dich aus.", back)
		return
	}
	if err := s.users.SetFlags(ctx, id, displayName, hidden, service); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, id, "user_updated", "durch "+CurrentUser(ctx).Username)
	s.flashRedirect(w, r, "Benutzer „"+target.Username+"“ gespeichert.", back)
}

func (s *Server) adminSetUserRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}
	target, err := s.users.ByID(ctx, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !s.guardAdminTarget(w, r, id) {
		return
	}
	back := "/admin/benutzer/" + strconv.FormatInt(id, 10)
	g, acc := CurrentAccess(ctx)
	before, err := s.roles.UserRoleIDs(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	after := formIDs(r, "role")
	if touchesSystemRole(g, before, after) && !acc.IsAdmin(g) {
		s.forbidden(w, r, msgOnlyAdmins)
		return
	}
	err = s.roles.SetUserRoles(ctx, id, after)
	if errors.Is(err, authz.ErrLastAdmin) {
		s.flashRedirect(w, r, err.Error(), back)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, id, "user_roles_changed", "durch "+CurrentUser(ctx).Username+": "+strings.Join(g.PathNames(after), ", "))
	s.flashRedirect(w, r, "Rollen von „"+target.Username+"“ gespeichert.", back)
}

func (s *Server) adminLockUser(w http.ResponseWriter, r *http.Request) {
	s.adminSetActive(w, r, false)
}

func (s *Server) adminUnlockUser(w http.ResponseWriter, r *http.Request) {
	s.adminSetActive(w, r, true)
}

func (s *Server) adminSetActive(w http.ResponseWriter, r *http.Request, active bool) {
	admin := CurrentUser(r.Context())
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}
	if id == admin.ID && !active {
		s.flashRedirect(w, r, "Du kannst dich nicht selbst sperren.", "/admin/benutzer")
		return
	}
	target, err := s.users.ByID(r.Context(), id)
	if errors.Is(err, auth.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !s.guardAdminTarget(w, r, id) {
		return
	}
	if err := s.users.SetActive(r.Context(), id, active); err != nil {
		s.serverError(w, r, err)
		return
	}
	event, msg := "user_locked", "Benutzer „"+target.Username+"“ gesperrt. Anmelden ist nicht mehr möglich, offene Sitzungen enden sofort."
	if active {
		event, msg = "user_unlocked", "Benutzer „"+target.Username+"“ entsperrt."
	}
	s.auditDetail(r, id, event, "durch "+admin.Username)
	s.flashRedirect(w, r, msg, "/admin/benutzer")
}
