-- +goose Up
-- Rechte einer Rolle an den Datensätzen einer Tabelle. Rein additiv: gilt für alle Mitglieder
-- der Rolle, auch über Verschachtelung. Die Rolle „Administratoren“ darf ohnehin alles.
CREATE TABLE table_permissions (
    table_id   BIGINT UNSIGNED NOT NULL,
    role_id    BIGINT UNSIGNED NOT NULL,
    can_read   BOOLEAN         NOT NULL DEFAULT FALSE,
    can_create BOOLEAN         NOT NULL DEFAULT FALSE,
    can_update BOOLEAN         NOT NULL DEFAULT FALSE,
    can_delete BOOLEAN         NOT NULL DEFAULT FALSE,
    PRIMARY KEY (table_id, role_id),
    KEY table_permissions_role_idx (role_id),
    CONSTRAINT table_permissions_table_fk FOREIGN KEY (table_id) REFERENCES data_tables (id) ON DELETE CASCADE,
    CONSTRAINT table_permissions_role_fk FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE table_permissions;
