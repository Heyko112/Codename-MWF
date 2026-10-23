-- +goose Up
CREATE TABLE users (
    id              BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    username        VARCHAR(64)      NOT NULL,
    email           VARCHAR(255)     NULL,
    display_name    VARCHAR(128)     NOT NULL DEFAULT '',
    password_hash   VARCHAR(255)     NOT NULL,
    -- TOTP-Secret, von der App verschlüsselt abgelegt
    totp_secret     VARBINARY(255)   NULL,
    totp_enabled    BOOLEAN          NOT NULL DEFAULT FALSE,
    is_active       BOOLEAN          NOT NULL DEFAULT TRUE,
    failed_logins   INT UNSIGNED     NOT NULL DEFAULT 0,
    locked_until    DATETIME(6)      NULL,
    last_login_at   DATETIME(6)      NULL,
    created_at      DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at      DATETIME(6)      NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY users_username_uq (username),
    UNIQUE KEY users_email_uq (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE users;
