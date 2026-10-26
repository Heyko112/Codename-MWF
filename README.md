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

## Tabellen

Mit dem Recht „Tabellen definieren“ erscheint oben **Tabellen** (`/admin/tabellen`). Dort legst du Tabellen an und gibst ihnen Spalten: Text (Länge 1–1000), Langtext, Ganzzahl, Dezimalzahl (0–6 Nachkommastellen), Datum, Datum und Uhrzeit, Ja/Nein.

- Jede Tabelle ist eine echte MariaDB-Tabelle `dt_<ID>`, jede Spalte heißt `c_<ID>`. Die Namen in der Oberfläche sind nur Beschriftung, Umbenennen ändert nichts an der Struktur.
- Jede Tabelle hat automatisch `id`, `created_at`, `created_by`, `updated_at`, `updated_by`.
- Bei Text lässt sich die Länge nur vergrößern, der Typ einer Spalte bleibt fest. Die Reihenfolge der Spalten ist nur Anzeige.
- Pflichtfelder prüft die App beim Speichern von Datensätzen.
- Löschen einer Spalte oder Tabelle entfernt auch die Daten und muss bestätigt werden.

## Daten

Unter **Daten** (`/daten`) sieht jeder angemeldete Benutzer die Tabellen, für die er Rechte hat, und kann dort Datensätze suchen, anlegen, ändern und löschen.

- Die Rechte vergibst du auf der Tabellenseite unter „Rechte an den Datensätzen“ je Rolle: Lesen, Anlegen, Ändern, Löschen. Sie gelten additiv auch über enthaltene Rollen, Anlegen, Ändern und Löschen schließen Lesen ein. „Administratoren“ dürfen immer alles.
- Wer keine Rechte an einer Tabelle hat, bekommt 404, die Tabelle ist für ihn unsichtbar.
- Zahlen in deutscher Schreibweise (1.234,56), Datum als 31.12.2026. Gesucht wird in Textfeldern und nach ID, 50 Datensätze je Seite.
- Gelöschte Datensätze landen im `audit_log`.

## Widgets

Widgets sind kleine eigene Webapps, die abgeschottet in MWF laufen (Konzept: Schritte D1 bis D5). Unter **Widgets** (`/widgets`) sieht jeder Benutzer die für ihn freigegebenen Widgets und öffnet sie im Vollbild.

- Mit dem Recht „Widgets verwalten“ lädst du unter `/admin/widgets` ein Paket als ZIP hoch (höchstens 10 MB, entpackt 20 MB, 500 Dateien). Gleiche ID im Manifest = Aktualisierung.
- Sichtbarkeit je Rolle, additiv auch über enthaltene Rollen. Inaktive Widgets sehen nur Verwalter.
- Ein Widget läuft in einem `iframe` mit `sandbox` ohne eigene Herkunft: keine Cookies, kein Zugriff auf die MWF-Seite, kein Internet (strenge CSP). Dateien werden über eine signierte, 8 Stunden gültige Adresse geladen, die Rechte werden bei jeder Datei neu geprüft.
- Erlaubte Dateitypen: HTML, JS, CSS, JSON, Text, Bilder (SVG, PNG, JPG, GIF, WebP, ICO), Schriften (WOFF, WOFF2, TTF).

Ein Beispiel liegt in `examples/widgets/hallo`. Den Ordner `hallo` als ZIP packen (Windows: Rechtsklick → Senden an → ZIP-komprimierter Ordner) und hochladen. Das Paket braucht eine `manifest.json`:

```json
{ "id": "hallo", "name": "Hallo", "version": "1.0.0", "description": "…", "size": "small", "entry": "index.html" }
```

`id`: Kleinbuchstaben, Ziffern, Minus. `size`: `small`, `wide` oder `large` (für die Startseite, kommt in D3).

### Daten aus dem Widget

Ein Widget fordert im Manifest Tabellen über einen Alias an. Unter `/admin/widgets/<id>` ordnet ein Verwalter jedem Alias eine echte Tabelle zu und gibt höchstens die angeforderten Rechte frei. Wirksam ist immer **Recht des Benutzers ∩ Freigabe des Widgets**: Über ein Widget kann niemand mehr als in der Tabelle selbst.

```json
"tables": [ {
  "alias": "notizen", "access": ["read", "create", "update", "delete"], "description": "Wofür das Widget die Tabelle braucht",
  "name": "Notizen",
  "columns": [
    { "key": "text", "name": "Text", "type": "text", "size": 200, "required": true },
    { "key": "erledigt", "name": "Erledigt", "type": "bool" }
  ]
} ]
```

`name` und `columns` sind der **Bauplan** (optional, Typen wie bei den Einstellungen unten). Mit Bauplan bietet die Widget-Seite je Alias „Neue Tabelle anlegen“: Name und Spaltennamen sind vorausgefüllt und änderbar, ein Klick legt die Tabelle mit allen Spalten an, ordnet sie zu, gibt die angeforderten Rechte frei und setzt auf Wunsch dieselben Rechte für gewählte Rollen. Alternativ „Vorhandene Tabelle nehmen“ (eine Tabelle mit dem vorgeschlagenen Namen ist vorausgewählt). Fehlen in der zugeordneten Tabelle Spalten, etwa nach einem Update mit neuen Spalten, zeigt die Seite sie mit „Fehlende Spalten ergänzen“. Ergänzt wird nur auf Klick, gelöscht wird nie etwas.

Spalten spricht das Widget über ihren festen **Schlüssel** an (unter Tabellen verwalten sichtbar, bei Anlage optional selbst wählbar, danach unveränderlich). So darf eine Spalte umbenannt werden, ohne dass ein Widget bricht.

Im Widget die Bibliothek einbinden, alle Aufrufe liefern ein Promise (Typen: `/static/mwf-widget.d.ts`):

```html
<script src="/static/mwf-widget.js"></script>
```

```js
const t = await mwf.tables();                        // Aliase, wirksame Rechte, Spalten
const { total, records } = await mwf.list("notizen", { limit: 50, search: "milch" });
const rec = await mwf.create("notizen", { text: "Brot", erledigt: false });
await mwf.update("notizen", rec.id, { erledigt: true }); // nur übergebene Spalten
await mwf.remove("notizen", rec.id);
```

Werte: Text als String, Ganzzahl als Zahl, Dezimal als String (`"12.50"`), Datum `"YYYY-MM-DD"`, Datum/Uhrzeit `"YYYY-MM-DDTHH:MM:SS"`, Ja/Nein als `true`/`false`, leer als `null`. Fehler kommen als `mwf.MwfError` mit `code` (z. B. `forbidden`, `invalid`, `not_mapped`) und bei `invalid` mit `fields` je Spalte. Löschen über ein Widget steht im Audit-Log.

Beispiel: `examples/widgets/notizen` (mit Bauplan für die Tabelle).

**Grenze:** Die Abschottung schützt MWF vor dem Widget. Ein Widget sieht aber die Daten, die es lesen darf, und könnte seinen eigenen Rahmen auf eine fremde Seite umleiten. Nur Widgets aus vertrauenswürdiger Quelle installieren.

### Einstellungen je Widget

Jedes Widget bekommt automatisch eine eigene Einstellungs-Tabelle (eine echte Tabelle mit genau einem Datensatz). Sie gehört dem Widget, taucht nicht unter Tabellen/Daten auf und wird mit dem Widget gelöscht. Welche Einstellungen es gibt, steht im Manifest:

```json
"settings": [
  { "key": "zeitzone", "name": "Zeitzone", "type": "text", "size": 64, "required": true, "default": "Europe/Berlin", "description": "z. B. Europe/Berlin" },
  { "key": "sekunden", "name": "Sekunden anzeigen", "type": "bool", "default": false }
]
```

Typen wie bei Tabellen: `text`, `longtext`, `int`, `decimal`, `date`, `datetime`, `bool`. Die Werte setzt ein Verwalter unter Widgets verwalten → Widget → Einstellungen, das Widget liest sie mit `await mwf.settings()` (Objekt nach `key`). Bei einem Update kommen neue Einstellungen mit ihrer Vorgabe dazu, vorhandene Werte bleiben erhalten. Beispiel: `examples/widgets/uhr`.

## Startseite und Design

Die Startseite besteht aus **Bereichen** (Reitern), zum Beispiel Übersicht, Vertrieb, Team. Mit dem Recht „Widgets verwalten“ legst du sie unter **Startseite** (`/admin/startseite`) an:

- Ein Bereich ohne Rolle ist für alle sichtbar, sonst für die gewählten Rollen (additiv, auch über enthaltene Rollen).
- In jeden Bereich legst du Widgets als **Kacheln** und ordnest sie mit den Pfeilen. Die Größe (klein, breit, groß) kommt aus dem Manifest des Widgets.
- Eine Kachel sieht nur, wer das Widget selbst sehen darf. Jeder Benutzer kann Kacheln für sich ausblenden und unten auf der Startseite wieder einblenden.
- Mit dem Stern in einem geöffneten Widget legt jeder Benutzer es als Favorit in seine Seitenleiste.

Unten in der Seitenleiste wählt jeder Benutzer sein Design: **Hell**, **Dunkel** oder **Auto** (wie das Betriebssystem). Widgets bekommen Ansicht und Design über die Adresse (`?ansicht=kachel|vollbild&design=light|dark`). `mwf-widget.js` setzt daraus `data-ansicht` und `data-theme` an `<html>` und bietet `mwf.view` und `mwf.theme`. Beispiele: `examples/widgets/uhr` und `examples/widgets/notizen`.

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
