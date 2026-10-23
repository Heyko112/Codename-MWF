package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Heyko112/codename-mwf/internal/auth"
)

const sessFlash = "flash"

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	s.renderAdminUsers(w, r, http.StatusOK, nil)
}

// renderAdminUsers zeigt die Liste. form enthält bei einem Fehler die eingegebenen Werte.
func (s *Server) renderAdminUsers(w http.ResponseWriter, r *http.Request, status int, form map[string]any) {
	users, err := s.users.List(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, status, "admin_users", map[string]any{
		"User":      CurrentUser(r.Context()),
		"Users":     users,
		"Flash":     s.sessions.PopString(r.Context(), sessFlash),
		"Form":      form,
		"MinLength": auth.MinPasswordLen,
	})
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	admin := CurrentUser(r.Context())
	username := strings.TrimSpace(r.PostFormValue("username"))
	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	password := r.PostFormValue("password")
	isAdmin := r.PostFormValue("is_admin") == "on"

	form := map[string]any{"Username": username, "DisplayName": displayName, "IsAdmin": isAdmin}
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
	case utf8.RuneCountInString(password) < auth.MinPasswordLen:
		fail("Das Passwort muss mindestens " + strconv.Itoa(auth.MinPasswordLen) + " Zeichen lang sein.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	id, err := s.users.Create(r.Context(), username, displayName, hash, isAdmin)
	if errors.Is(err, auth.ErrUsernameTaken) {
		fail("Den Benutzernamen „" + username + "“ gibt es schon.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, id, "user_created", "durch "+admin.Username)
	s.sessions.Put(r.Context(), sessFlash, "Benutzer „"+username+"“ angelegt. Die 2FA wird beim ersten Login eingerichtet.")
	http.Redirect(w, r, "/admin/benutzer", http.StatusSeeOther)
}

func (s *Server) adminLockUser(w http.ResponseWriter, r *http.Request) {
	s.adminSetActive(w, r, false)
}

func (s *Server) adminUnlockUser(w http.ResponseWriter, r *http.Request) {
	s.adminSetActive(w, r, true)
}

func (s *Server) adminSetActive(w http.ResponseWriter, r *http.Request, active bool) {
	admin := CurrentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if id == admin.ID && !active {
		s.sessions.Put(r.Context(), sessFlash, "Du kannst dich nicht selbst sperren.")
		http.Redirect(w, r, "/admin/benutzer", http.StatusSeeOther)
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
	if err := s.users.SetActive(r.Context(), id, active); err != nil {
		s.serverError(w, r, err)
		return
	}
	event, msg := "user_locked", "Benutzer „"+target.Username+"“ gesperrt. Anmelden ist nicht mehr möglich, offene Sitzungen enden sofort."
	if active {
		event, msg = "user_unlocked", "Benutzer „"+target.Username+"“ entsperrt."
	}
	s.auditDetail(r, id, event, "durch "+admin.Username)
	s.sessions.Put(r.Context(), sessFlash, msg)
	http.Redirect(w, r, "/admin/benutzer", http.StatusSeeOther)
}
