// Package db stellt die Verbindung zu MariaDB her.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/Heyko112/codename-mwf/internal/config"
)

// Open öffnet den Verbindungspool und wartet, bis MariaDB erreichbar ist.
func Open(ctx context.Context, c config.Config) (*sql.DB, error) {
	mc := mysql.NewConfig()
	mc.Net = "tcp"
	mc.Addr = c.DBHost + ":" + c.DBPort
	mc.DBName = c.DBName
	mc.User = c.DBUser
	mc.Passwd = c.DBPassword
	mc.ParseTime = true
	mc.Loc = time.UTC
	mc.Params = map[string]string{"charset": "utf8mb4"}

	db, err := sql.Open("mysql", mc.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// MariaDB braucht beim ersten Start einige Sekunden.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for {
		err = db.PingContext(ctx)
		if err == nil {
			return db, nil
		}
		slog.Info("warte auf Datenbank", "fehler", err)
		select {
		case <-ctx.Done():
			db.Close()
			return nil, fmt.Errorf("Datenbank nicht erreichbar: %w", err)
		case <-time.After(2 * time.Second):
		}
	}
}
