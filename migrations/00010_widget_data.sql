-- +goose Up
-- Fester technischer Schlüssel je Spalte, z. B. „firma“. Bleibt beim Umbenennen gleich,
-- Widgets sprechen Spalten darüber an. Bestehende Spalten bekommen ihn beim Start der App.
ALTER TABLE data_columns
    ADD COLUMN col_key VARCHAR(64) NULL AFTER name,
    ADD UNIQUE KEY data_columns_key_uq (table_id, col_key);

-- Tabellen, die ein Widget im Manifest anfordert (per Alias), und was der Admin davon freigibt
CREATE TABLE widget_tables (
    widget_id  BIGINT UNSIGNED NOT NULL,
    alias      VARCHAR(32)     NOT NULL,
    table_id   BIGINT UNSIGNED NULL,
    can_read   BOOLEAN         NOT NULL DEFAULT FALSE,
    can_create BOOLEAN         NOT NULL DEFAULT FALSE,
    can_update BOOLEAN         NOT NULL DEFAULT FALSE,
    can_delete BOOLEAN         NOT NULL DEFAULT FALSE,
    PRIMARY KEY (widget_id, alias),
    CONSTRAINT widget_tables_widget_fk FOREIGN KEY (widget_id) REFERENCES widgets (id) ON DELETE CASCADE,
    CONSTRAINT widget_tables_table_fk FOREIGN KEY (table_id) REFERENCES data_tables (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE widget_tables;
ALTER TABLE data_columns DROP KEY data_columns_key_uq, DROP COLUMN col_key;
