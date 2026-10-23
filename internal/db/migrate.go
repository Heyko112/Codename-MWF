package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"

	"github.com/Heyko112/codename-mwf/migrations"
)

// Migrate spielt alle noch fehlenden Migrationen ein.
func Migrate(ctx context.Context, db *sql.DB) error {
	provider, err := goose.NewProvider(goose.DialectMySQL, db, migrations.FS)
	if err != nil {
		return err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("Migration fehlgeschlagen: %w", err)
	}
	for _, r := range results {
		slog.Info("Migration eingespielt", "datei", r.Source.Path, "dauer", r.Duration)
	}
	return nil
}
