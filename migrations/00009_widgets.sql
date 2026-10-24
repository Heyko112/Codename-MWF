-- +goose Up
-- Installierte Widgets. slug ist die ID aus dem Manifest des Pakets, id der interne Schlüssel.
CREATE TABLE widgets (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    slug         VARCHAR(64)     NOT NULL,
    name         VARCHAR(64)     NOT NULL,
    version      VARCHAR(32)     NOT NULL,
    description  VARCHAR(255)    NOT NULL DEFAULT '',
    -- Kachelgröße: small, wide, large
    size         VARCHAR(8)      NOT NULL DEFAULT 'small',
    entry        VARCHAR(255)    NOT NULL DEFAULT 'index.html',
    manifest     TEXT            NOT NULL,
    is_active    BOOLEAN         NOT NULL DEFAULT TRUE,
    installed_by BIGINT UNSIGNED NULL,
    installed_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at   DATETIME(6)     NULL,
    PRIMARY KEY (id),
    UNIQUE KEY widgets_slug_uq (slug),
    CONSTRAINT widgets_user_fk FOREIGN KEY (installed_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Dateien eines Pakets, ausgeliefert unter /w/<id>/<marke>/<pfad>
CREATE TABLE widget_files (
    widget_id    BIGINT UNSIGNED NOT NULL,
    path         VARCHAR(255)    NOT NULL,
    content_type VARCHAR(64)     NOT NULL,
    data         MEDIUMBLOB      NOT NULL,
    PRIMARY KEY (widget_id, path),
    CONSTRAINT widget_files_widget_fk FOREIGN KEY (widget_id) REFERENCES widgets (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- Wer ein Widget sieht: additiv über Rollen, „Administratoren“ sehen alle
CREATE TABLE widget_roles (
    widget_id BIGINT UNSIGNED NOT NULL,
    role_id   BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (widget_id, role_id),
    KEY widget_roles_role_idx (role_id),
    CONSTRAINT widget_roles_widget_fk FOREIGN KEY (widget_id) REFERENCES widgets (id) ON DELETE CASCADE,
    CONSTRAINT widget_roles_role_fk FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE widget_roles;
DROP TABLE widget_files;
DROP TABLE widgets;
