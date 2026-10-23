-- +goose Up
-- Schema vorgegeben von alexedwards/scs (mysqlstore)
CREATE TABLE sessions (
    token   CHAR(43)     NOT NULL,
    data    BLOB         NOT NULL,
    expiry  TIMESTAMP(6) NOT NULL,
    PRIMARY KEY (token),
    KEY sessions_expiry_idx (expiry)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE sessions;
