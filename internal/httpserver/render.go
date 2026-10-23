package httpserver

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
)

// Templates hält pro Seite ein fertig geparstes Template (Layout + Seite).
type Templates map[string]*template.Template

// LoadTemplates parst jede Seite zusammen mit dem Layout.
func LoadTemplates(fsys fs.FS) (Templates, error) {
	pages, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}
	t := Templates{}
	for _, p := range pages {
		if p == "templates/layout.html" {
			continue
		}
		tmpl, err := template.ParseFS(fsys, "templates/layout.html", p)
		if err != nil {
			return nil, fmt.Errorf("Template %s: %w", p, err)
		}
		t[p[len("templates/"):len(p)-len(".html")]] = tmpl
	}
	return t, nil
}

// render rendert erst in einen Puffer, damit bei Fehlern keine halbe Seite ausgeliefert wird.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	tmpl, ok := s.templates[page]
	if !ok {
		slog.Error("Template fehlt", "id", RequestIDFrom(r.Context()), "seite", page)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	// Für die Navigation: welche Rechte hat der angemeldete Benutzer?
	if m, ok := data.(map[string]any); ok {
		can := map[string]bool{}
		if _, acc := CurrentAccess(r.Context()); acc != nil {
			for p := range acc.Permissions {
				can[p] = true
			}
		}
		m["Can"] = can
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Error("Template-Fehler", "id", RequestIDFrom(r.Context()), "seite", page, "fehler", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, "error", map[string]any{
		"Status":  "404",
		"Message": "Diese Seite gibt es nicht.",
	})
}
