-- +goose Up
CREATE TABLE roles (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name        VARCHAR(64)     NOT NULL,
    description VARCHAR(255)    NOT NULL DEFAULT '',
    -- versteckt = taucht nicht in Auswahllisten auf (z. B. Service-Rollen)
    is_hidden   BOOLEAN         NOT NULL DEFAULT FALSE,
    -- Systemrolle (Administratoren): nicht löschbar, hat immer alle Rechte
    is_system   BOOLEAN         NOT NULL DEFAULT FALSE,
    created_at  DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY roles_name_uq (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Direkte Mitgliedschaft eines Benutzers in einer Rolle
CREATE TABLE role_members (
    role_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (role_id, user_id),
    KEY role_members_user_idx (user_id),
    CONSTRAINT role_members_role_fk FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE,
    CONSTRAINT role_members_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- Verschachtelung: child liegt in parent. Mitglieder von child sind damit auch Mitglieder von parent
-- und erben dessen Rechte (additiv).
CREATE TABLE role_includes (
    parent_id BIGINT UNSIGNED NOT NULL,
    child_id  BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (parent_id, child_id),
    KEY role_includes_child_idx (child_id),
    CONSTRAINT role_includes_parent_fk FOREIGN KEY (parent_id) REFERENCES roles (id) ON DELETE CASCADE,
    CONSTRAINT role_includes_child_fk FOREIGN KEY (child_id) REFERENCES roles (id) ON DELETE CASCADE,
    CONSTRAINT role_includes_self_ck CHECK (parent_id <> child_id)
) ENGINE=InnoDB;

-- Rechte hängen nur an Rollen. Die Liste der möglichen Rechte steht im Code (internal/authz).
CREATE TABLE role_permissions (
    role_id    BIGINT UNSIGNED NOT NULL,
    permission VARCHAR(64)     NOT NULL,
    PRIMARY KEY (role_id, permission),
    CONSTRAINT role_permissions_role_fk FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE
) ENGINE=InnoDB;

ALTER TABLE users
    ADD COLUMN is_hidden  BOOLEAN NOT NULL DEFAULT FALSE AFTER display_name,
    ADD COLUMN is_service BOOLEAN NOT NULL DEFAULT FALSE AFTER is_hidden;

-- Bisherige Admins werden Mitglieder der Systemrolle "Administratoren"
INSERT INTO roles (name, description, is_system) VALUES ('Administratoren', 'Hat immer alle Rechte', TRUE);
INSERT INTO role_members (role_id, user_id)
    SELECT r.id, u.id FROM roles r JOIN users u ON u.is_admin WHERE r.name = 'Administratoren';
ALTER TABLE users DROP COLUMN is_admin;

-- +goose Down
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE AFTER display_name;
UPDATE users u JOIN role_members m ON m.user_id = u.id JOIN roles r ON r.id = m.role_id AND r.is_system
    SET u.is_admin = TRUE;
ALTER TABLE users DROP COLUMN is_service, DROP COLUMN is_hidden;
DROP TABLE role_permissions;
DROP TABLE role_includes;
DROP TABLE role_members;
DROP TABLE roles;
