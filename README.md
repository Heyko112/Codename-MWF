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
- `internal/httpserver/` Router und Handler

## Stack

Go 1.25 (`net/http`), MariaDB 11.4, `go-sql-driver/mysql`. Geplant: `goose` (Migrationen), Argon2id, TOTP (`pquerna/otp`), `scs` (Sessions).
