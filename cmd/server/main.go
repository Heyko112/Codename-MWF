package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // Zeitzonen-Daten im Binary, distroless hat keine

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/config"
	"github.com/Heyko112/codename-mwf/internal/db"
	"github.com/Heyko112/codename-mwf/internal/httpserver"
	"github.com/Heyko112/codename-mwf/internal/tables"
	"github.com/Heyko112/codename-mwf/internal/widgets"
	"github.com/Heyko112/codename-mwf/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	// Distroless hat weder curl noch wget, daher prüft das Binary sich selbst.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if len(os.Args) > 1 && os.Args[1] == "create-user" {
		if err := createUser(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("Start fehlgeschlagen", "fehler", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := db.Migrate(ctx, database); err != nil {
		return err
	}
	ts := tables.NewStore(database)
	if err := ts.BackfillKeys(ctx); err != nil {
		return fmt.Errorf("Spaltenschlüssel nachtragen: %w", err)
	}
	// Widgets, die vor den Einstellungs-Tabellen installiert wurden, bekommen ihre Tabelle jetzt
	ws := widgets.NewStore(database)
	all, err := ws.List(ctx)
	if err != nil {
		return err
	}
	for _, wd := range all {
		if _, err := ws.SyncSettings(ctx, ts, wd, 0); err != nil {
			return fmt.Errorf("Einstellungen von Widget %s: %w", wd.Slug, err)
		}
	}

	sessions := scs.New()
	sessions.Store = mysqlstore.New(database)
	sessions.Lifetime = 12 * time.Hour
	sessions.IdleTimeout = 1 * time.Hour
	sessions.Cookie.Name = "mwf_session"
	sessions.Cookie.HttpOnly = true
	sessions.Cookie.SameSite = http.SameSiteLaxMode
	sessions.Cookie.Secure = cfg.CookieSecure

	templates, err := httpserver.LoadTemplates(web.FS)
	if err != nil {
		return err
	}
	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		return err
	}
	secrets, err := auth.NewSecretBox(cfg.TOTPKey)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpserver.New(database, sessions, templates, static, secrets).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("Server gestartet", "addr", cfg.Addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("fahre herunter")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func healthcheck() int {
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + addr[strings.LastIndex(addr, ":"):] + "/health")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
