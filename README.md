# Codename MWF

Basissystem für Workflows, angelehnt an JobRouter. Go-App mit eigenem Routing (kein nginx/Apache) und MariaDB, beides in Docker.

## Starten

```sh
cp .env.example .env   # Passwörter anpassen und TOTP_KEY setzen (openssl rand -base64 32)
docker compose up -d --build
curl http://localhost:8080/health   # {"db":"ok","status":"ok"}
```

Die Datenbank liegt im Volume `db-data` und bleibt bei Neustarts erhalten.

## Ersten Benutzer anlegen

```sh
docker compose exec app /server create-user <benutzername> "<Anzeigename>"
```

Das Passwort (mindestens 12 Zeichen) wird abgefragt. So angelegte Benutzer kommen in die Rolle „Administratoren“. Danach unter http://localhost:8080 anmelden. Beim ersten Login wird die 2FA mit einer Authenticator-App eingerichtet, sie ist Pflicht.

## Benutzer, Rollen und Rechte

Oben im Menü stehen **Benutzer** (`/admin/benutzer`) und **Rollen** (`/admin/rollen`), je nach Recht.

- **Rechte** hängen nur an Rollen: „Benutzer verwalten“, „Rollen und Rechte verwalten“, „Tabellen definieren“. Die Rolle „Administratoren“ hat immer alle Rechte und braucht mindestens ein Mitglied.
- **Verschachtelung:** Liegt Rolle B in Rolle A, sind die Mitglieder von B auch Mitglieder von A und erben deren Rechte. Rein additiv, keine Kreise, höchstens 3 Ebenen.
- **Versteckt:** Rollen und Benutzer lassen sich verstecken. Eine Service-Rolle ist eine versteckte Rolle.
- **Service-User** haben keinen Web-Login (später API-Key).
- Auf der Benutzerseite zeigt „Warum hat X Zugriff?“, über welche Rollen jedes Recht ankommt.
- Nur Administratoren dürfen die Rolle „Administratoren“ vergeben oder verschachteln und Administratoren ändern oder sperren.
- Entsperren hebt auch eine Sperre nach Fehlversuchen auf, gesperrte Benutzer werden sofort abgemeldet. Jede Aktion landet im `audit_log`.

Die App ist standardmäßig nur auf dem eigenen Rechner erreichbar (`APP_BIND=127.0.0.1`). Zeiten werden in `Europe/Berlin` angezeigt (änderbar über `TZ` in der `.env`).

## Login und Sicherheit

- Passwörter: Argon2id (64 MiB, 3 Durchläufe), nie im Klartext gespeichert
- 2FA: TOTP (6 Ziffern, 30 s), Secret AES-256-GCM-verschlüsselt in der DB, jeder Code nur einmal gültig
- Nach 5 Fehlversuchen (Passwort oder Code) ist das Konto 15 Minuten gesperrt
- Neue Session-ID nach Passwort und nach 2FA, Session-Cookie HttpOnly und SameSite=Lax
- Alle Anmeldeereignisse landen in `audit_log`

## Struktur

- `cmd/server/` Einstiegspunkt der App
- `internal/config/` Konfiguration aus Umgebungsvariablen
- `internal/db/` Verbindung zu MariaDB
- `internal/auth/` Passwort-Hashing, TOTP, Benutzer und Audit-Log
- `internal/httpserver/` Router, Middleware (Request-ID, Logging, Panic-Schutz, Security-Header, CSRF) und Handler
- `web/` Templates und statische Dateien (ins Binary eingebettet)
- `migrations/` SQL-Migrationen (ins Binary eingebettet)

## Migrationen

Die App spielt beim Start alle fehlenden Migrationen aus `migrations/` ein (goose). Neue Änderung = neue Datei mit der nächsten Nummer, z. B. `00004_roles.sql`, mit `-- +goose Up` und `-- +goose Down`. Bestehende Dateien nie nachträglich ändern.

## Stack

Go 1.25 (`net/http`), MariaDB 11.4, `go-sql-driver/mysql`, `goose` (Migrationen). `scs` (Sessions in MariaDB), Argon2id (`x/crypto`), TOTP (`pquerna/otp`).
