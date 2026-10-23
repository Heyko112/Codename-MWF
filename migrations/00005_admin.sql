-- +goose Up
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE AFTER display_name;
-- Der zuerst angelegte Benutzer wird Admin, damit jemand die Verwaltung öffnen kann
UPDATE users SET is_admin = TRUE ORDER BY id LIMIT 1;
-- Zusatzinfo zu einem Ereignis, z. B. welcher Admin einen Benutzer gesperrt hat
ALTER TABLE audit_log ADD COLUMN detail VARCHAR(255) NULL AFTER event;

-- +goose Down
ALTER TABLE audit_log DROP COLUMN detail;
ALTER TABLE users DROP COLUMN is_admin;
