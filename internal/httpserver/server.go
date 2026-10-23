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
)

// Server bündelt die Abhängigkeiten der Handler.
type Server struct {
	db        *sql.DB
	sessions  *scs.SessionManager
	templates Templates
	static    fs.FS
}

func New(db *sql.DB, sessions *scs.SessionManager, templates Templates, static fs.FS) *Server {
	return &Server{db: db, sessions: sessions, templates: templates, static: static}
}

// Handler registriert alle Routen und legt die Middleware-Kette darum.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.static)))

	// Seiten mit Session
	pages := http.NewServeMux()
	pages.HandleFunc("GET /{$}", s.home)
	pages.HandleFunc("/", s.notFound)
	mux.Handle("/", s.sessions.LoadAndSave(pages))

	return Chain(mux, RequestID, Logging, Recover, SecurityHeaders, CSRF)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", nil)
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
