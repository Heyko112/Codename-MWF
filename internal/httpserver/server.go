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
)

// Server bündelt die Abhängigkeiten der Handler.
type Server struct {
	db        *sql.DB
	sessions  *scs.SessionManager
	templates Templates
	static    fs.FS
	users     *auth.Store
	secrets   *auth.SecretBox
}

func New(db *sql.DB, sessions *scs.SessionManager, templates Templates, static fs.FS, secrets *auth.SecretBox) *Server {
	return &Server{db: db, sessions: sessions, templates: templates, static: static, users: auth.NewStore(db), secrets: secrets}
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
	pages.HandleFunc("GET /admin/benutzer", s.requireAdmin(s.adminUsers))
	pages.HandleFunc("POST /admin/benutzer", s.requireAdmin(s.adminCreateUser))
	pages.HandleFunc("POST /admin/benutzer/{id}/sperren", s.requireAdmin(s.adminLockUser))
	pages.HandleFunc("POST /admin/benutzer/{id}/entsperren", s.requireAdmin(s.adminUnlockUser))
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
