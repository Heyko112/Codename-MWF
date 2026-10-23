// Package httpserver enthält Router und Handler der App.
package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// NewRouter registriert alle Routen.
func NewRouter(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health(db))
	return mux
}

func health(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		status, code := "ok", http.StatusOK
		dbStatus := "ok"
		if err := db.PingContext(ctx); err != nil {
			status, code, dbStatus = "error", http.StatusServiceUnavailable, "unreachable"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"status": status, "db": dbStatus})
	}
}
