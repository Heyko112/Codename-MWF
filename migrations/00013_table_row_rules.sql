-- +goose Up
-- Zeilenfilter (Schritt E2): Rechte einer Rolle an den Datensätzen, die bestimmte Bedingungen erfüllen.
-- Ergänzt table_permissions (dort: alle Zeilen). Rein additiv: Ein Benutzer darf eine Zeile, wenn
-- irgendeine seiner Regeln sie erlaubt. Die Bedingungen einer Regel gelten zusammen (UND).
-- conditions ist JSON: [{"col": <Spalten-ID, 0 = angelegt von>, "op": "eq", "val": "{ich}"}]
CREATE TABLE table_row_rules (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    table_id   BIGINT UNSIGNED NOT NULL,
    role_id    BIGINT UNSIGNED NOT NULL,
    can_read   BOOLEAN         NOT NULL DEFAULT FALSE,
    can_create BOOLEAN         NOT NULL DEFAULT FALSE,
    can_update BOOLEAN         NOT NULL DEFAULT FALSE,
    can_delete BOOLEAN         NOT NULL DEFAULT FALSE,
    conditions TEXT            NOT NULL,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY table_row_rules_table_idx (table_id),
    KEY table_row_rules_role_idx (role_id),
    CONSTRAINT table_row_rules_table_fk FOREIGN KEY (table_id) REFERENCES data_tables (id) ON DELETE CASCADE,
    CONSTRAINT table_row_rules_role_fk FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE table_row_rules;
