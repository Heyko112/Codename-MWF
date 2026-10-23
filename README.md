# Codename MWF

Basissystem für Workflows, angelehnt an JobRouter. Go-App mit eigenem Routing (kein nginx/Apache) und MariaDB, beides in Docker.

## Starten

```sh
cp .env.example .env   # Passwörter anpassen
docker compose up -d --build
curl http://localhost:8080/health   # {"db":"ok","status":"ok"}
```

Die Datenbank liegt im Volume `db-data` und bleibt bei Neustarts erhalten.

## Struktur

- `cmd/server/` Einstiegspunkt der App
- `internal/config/` Konfiguration aus Umgebungsvariablen
- `internal/db/` Verbindung zu MariaDB
- `internal/httpserver/` Router, Middleware (Request-ID, Logging, Panic-Schutz, Security-Header, CSRF) und Handler
- `web/` Templates und statische Dateien (ins Binary eingebettet)
- `migrations/` SQL-Migrationen (ins Binary eingebettet)

## Migrationen

Die App spielt beim Start alle fehlenden Migrationen aus `migrations/` ein (goose). Neue Änderung = neue Datei mit der nächsten Nummer, z. B. `00004_roles.sql`, mit `-- +goose Up` und `-- +goose Down`. Bestehende Dateien nie nachträglich ändern.

## Stack

Go 1.25 (`net/http`), MariaDB 11.4, `go-sql-driver/mysql`, `goose` (Migrationen). `scs` (Sessions in MariaDB). Geplant: Argon2id, TOTP (`pquerna/otp`).
