-- +goose Up
-- Verzeichnis der selbst definierten Tabellen (wie JobData).
-- Die echte Tabelle heißt dt_<id>, ihre Spalten c_<id>. Namen im Frontend sind nur Beschriftung,
-- Umbenennen ändert also nie die Struktur in MariaDB.
CREATE TABLE data_tables (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name        VARCHAR(64)     NOT NULL,
    description VARCHAR(255)    NOT NULL DEFAULT '',
    created_by  BIGINT UNSIGNED NULL,
    created_at  DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY data_tables_name_uq (name),
    CONSTRAINT data_tables_user_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE data_columns (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    table_id    BIGINT UNSIGNED NOT NULL,
    name        VARCHAR(64)     NOT NULL,
    -- text, longtext, int, decimal, date, datetime, bool
    type        VARCHAR(16)     NOT NULL,
    -- Länge bei text, Nachkommastellen bei decimal
    size        INT UNSIGNED    NOT NULL DEFAULT 0,
    -- Pflichtfeld prüft die App beim Speichern von Datensätzen (Schritt C)
    required    BOOLEAN         NOT NULL DEFAULT FALSE,
    position    INT UNSIGNED    NOT NULL DEFAULT 0,
    created_at  DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY data_columns_name_uq (table_id, name),
    CONSTRAINT data_columns_table_fk FOREIGN KEY (table_id) REFERENCES data_tables (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE data_columns;
DROP TABLE data_tables;
