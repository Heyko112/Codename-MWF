// Package httpserver enthält Router, Middleware und Handler der App.
package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
	"github.com/Heyko112/codename-mwf/internal/tables"
)

// Server bündelt die Abhängigkeiten der Handler.
type Server struct {
	db        *sql.DB
	sessions  *scs.SessionManager
	templates Templates
	static    fs.FS
	users     *auth.Store
	roles     *authz.Store
	tables    *tables.Store
	secrets   *auth.SecretBox
}

func New(db *sql.DB, sessions *scs.SessionManager, templates Templates, static fs.FS, secrets *auth.SecretBox) *Server {
	return &Server{db: db, sessions: sessions, templates: templates, static: static, users: auth.NewStore(db), roles: authz.NewStore(db), tables: tables.NewStore(db), secrets: secrets}
}

// Handler registriert alle Routen und legt die Middleware-Kette darum.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.static)))

	// Seiten mit Session
	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}", s.requireLogin(s.home))
	pages.HandleFunc("GET /login", s.loginForm)
	pages.HandleFunc("POST /login", s.loginSubmit)
	pages.HandleFunc("GET /login/2fa", s.totpForm)
	pages.HandleFunc("POST /login/2fa", s.totpSubmit)
	pages.HandleFunc("GET /login/2fa/einrichten", s.totpSetupForm)
	pages.HandleFunc("POST /login/2fa/einrichten", s.totpSetupSubmit)
	pages.HandleFunc("POST /logout", s.logout)
	users := func(h http.HandlerFunc) http.HandlerFunc { return s.requirePerm(authz.PermUsersManage, h) }
	pages.HandleFunc("GET /admin/benutzer", users(s.adminUsers))
	pages.HandleFunc("POST /admin/benutzer", users(s.adminCreateUser))
	pages.HandleFunc("GET /admin/benutzer/{id}", users(s.adminUserDetail))
	pages.HandleFunc("POST /admin/benutzer/{id}", users(s.adminUpdateUser))
	pages.HandleFunc("POST /admin/benutzer/{id}/rollen", users(s.adminSetUserRoles))
	pages.HandleFunc("POST /admin/benutzer/{id}/sperren", users(s.adminLockUser))
	pages.HandleFunc("POST /admin/benutzer/{id}/entsperren", users(s.adminUnlockUser))

	roles := func(h http.HandlerFunc) http.HandlerFunc { return s.requirePerm(authz.PermRolesManage, h) }
	pages.HandleFunc("GET /admin/rollen", roles(s.adminRoles))
	pages.HandleFunc("POST /admin/rollen", roles(s.adminCreateRole))
	pages.HandleFunc("GET /admin/rollen/{id}", roles(s.adminRoleDetail))
	pages.HandleFunc("POST /admin/rollen/{id}", roles(s.adminUpdateRole))
	pages.HandleFunc("POST /admin/rollen/{id}/rechte", roles(s.adminSetRolePerms))
	pages.HandleFunc("POST /admin/rollen/{id}/enthaelt", roles(s.adminAddInclude))
	pages.HandleFunc("POST /admin/rollen/{id}/enthaelt/{child}/entfernen", roles(s.adminRemoveInclude))
	pages.HandleFunc("POST /admin/rollen/{id}/loeschen", roles(s.adminDeleteRole))

	tbl := func(h http.HandlerFunc) http.HandlerFunc { return s.requirePerm(authz.PermTablesDefine, h) }
	pages.HandleFunc("GET /admin/tabellen", tbl(s.adminTables))
	pages.HandleFunc("POST /admin/tabellen", tbl(s.adminCreateTable))
	pages.HandleFunc("GET /admin/tabellen/{id}", tbl(s.adminTableDetail))
	pages.HandleFunc("POST /admin/tabellen/{id}", tbl(s.adminUpdateTable))
	pages.HandleFunc("POST /admin/tabellen/{id}/loeschen", tbl(s.adminDeleteTable))
	pages.HandleFunc("POST /admin/tabellen/{id}/spalten", tbl(s.adminAddColumn))
	pages.HandleFunc("POST /admin/tabellen/{id}/spalten/{col}", tbl(s.adminUpdateColumn))
	pages.HandleFunc("POST /admin/tabellen/{id}/spalten/{col}/hoch", tbl(s.adminMoveColumnUp))
	pages.HandleFunc("POST /admin/tabellen/{id}/spalten/{col}/runter", tbl(s.adminMoveColumnDown))
	pages.HandleFunc("POST /admin/tabellen/{id}/spalten/{col}/loeschen", tbl(s.adminDeleteColumn))
	pages.HandleFunc("POST /admin/tabellen/{id}/rechte", tbl(s.adminSetTablePerms))

	// Datensätze: Rechte je Tabelle prüfen die Handler selbst
	pages.HandleFunc("GET /daten", s.requireLogin(s.dataTables))
	pages.HandleFunc("GET /daten/{id}", s.requireLogin(s.dataRecords))
	pages.HandleFunc("GET /daten/{id}/neu", s.requireLogin(s.dataNewForm))
	pages.HandleFunc("POST /daten/{id}/neu", s.requireLogin(s.dataCreate))
	pages.HandleFunc("GET /daten/{id}/{rec}", s.requireLogin(s.dataRecord))
	pages.HandleFunc("POST /daten/{id}/{rec}", s.requireLogin(s.dataUpdate))
	pages.HandleFunc("POST /daten/{id}/{rec}/loeschen", s.requireLogin(s.dataDelete))
	pages.HandleFunc("/", s.notFound)
	mux.Handle("/", s.sessions.LoadAndSave(pages))

	return Chain(mux, RequestID, Logging, Recover, SecurityHeaders, CSRF)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", map[string]any{"User": CurrentUser(r.Context())})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	status, code := "ok", http.StatusOK
	dbStatus := "ok"
	if err := s.db.PingContext(ctx); err != nil {
		status, code, dbStatus = "error", http.StatusServiceUnavailable, "unreachable"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"status": status, "db": dbStatus})
}
