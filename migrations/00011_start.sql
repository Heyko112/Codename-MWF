-- +goose Up
-- Design je Benutzer: '' = wie das Betriebssystem, 'light' oder 'dark'
ALTER TABLE users ADD COLUMN theme VARCHAR(8) NOT NULL DEFAULT '' AFTER last_login_at;

-- Bereiche der Startseite (Reiter). Ohne Rolle sieht jeder den Bereich, sonst additiv über Rollen.
CREATE TABLE start_sections (
    id       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name     VARCHAR(64)     NOT NULL,
    position INT             NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY start_sections_name_uq (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE start_section_roles (
    section_id BIGINT UNSIGNED NOT NULL,
    role_id    BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (section_id, role_id),
    KEY start_section_roles_role_idx (role_id),
    CONSTRAINT start_section_roles_section_fk FOREIGN KEY (section_id) REFERENCES start_sections (id) ON DELETE CASCADE,
    CONSTRAINT start_section_roles_role_fk FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Kacheln: ein Widget in einem Bereich, Reihenfolge legt der Verwalter fest
CREATE TABLE start_tiles (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    section_id BIGINT UNSIGNED NOT NULL,
    widget_id  BIGINT UNSIGNED NOT NULL,
    position   INT             NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    UNIQUE KEY start_tiles_uq (section_id, widget_id),
    CONSTRAINT start_tiles_section_fk FOREIGN KEY (section_id) REFERENCES start_sections (id) ON DELETE CASCADE,
    CONSTRAINT start_tiles_widget_fk FOREIGN KEY (widget_id) REFERENCES widgets (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Kacheln, die ein Benutzer für sich ausgeblendet hat
CREATE TABLE start_hidden (
    user_id BIGINT UNSIGNED NOT NULL,
    tile_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (user_id, tile_id),
    CONSTRAINT start_hidden_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT start_hidden_tile_fk FOREIGN KEY (tile_id) REFERENCES start_tiles (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Favoriten eines Benutzers, erscheinen in der Seitenleiste
CREATE TABLE widget_favorites (
    user_id    BIGINT UNSIGNED NOT NULL,
    widget_id  BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (user_id, widget_id),
    CONSTRAINT widget_favorites_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT widget_favorites_widget_fk FOREIGN KEY (widget_id) REFERENCES widgets (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE widget_favorites;
DROP TABLE start_hidden;
DROP TABLE start_tiles;
DROP TABLE start_section_roles;
DROP TABLE start_sections;
ALTER TABLE users DROP COLUMN theme;
