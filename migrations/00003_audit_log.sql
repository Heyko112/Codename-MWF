-- +goose Up
CREATE TABLE audit_log (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    -- NULL, wenn der Benutzer unbekannt ist (z. B. Login mit falschem Namen)
    user_id     BIGINT UNSIGNED NULL,
    event       VARCHAR(64)     NOT NULL,
    ip          VARCHAR(45)     NULL,
    user_agent  VARCHAR(512)    NULL,
    created_at  DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY audit_log_user_idx (user_id, created_at),
    KEY audit_log_event_idx (event, created_at),
    CONSTRAINT audit_log_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE audit_log;
