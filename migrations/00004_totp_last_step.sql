-- +goose Up
-- Letzter verwendeter TOTP-Zeitschritt, damit ein Code nicht zweimal benutzt werden kann
ALTER TABLE users ADD COLUMN totp_last_step BIGINT NOT NULL DEFAULT 0 AFTER totp_enabled;

-- +goose Down
ALTER TABLE users DROP COLUMN totp_last_step;
