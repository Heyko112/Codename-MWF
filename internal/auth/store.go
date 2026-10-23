package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Nach so vielen Fehlversuchen in Folge wird das Konto gesperrt.
const (
	MaxFailedLogins = 5
	LockDuration    = 15 * time.Minute
)

var ErrNotFound = errors.New("Benutzer nicht gefunden")

type User struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash string
	TOTPSecret   []byte // verschlüsselt
	TOTPEnabled  bool
	TOTPLastStep int64
	IsActive     bool
	FailedLogins int
	LockedUntil  sql.NullTime
}

// Locked meldet, ob das Konto gerade gesperrt ist.
func (u *User) Locked(now time.Time) bool {
	return u.LockedUntil.Valid && u.LockedUntil.Time.After(now)
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const userColumns = `id, username, display_name, password_hash, totp_secret, totp_enabled,
	totp_last_step, is_active, failed_logins, locked_until`

func scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled,
		&u.TOTPLastStep, &u.IsActive, &u.FailedLogins, &u.LockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) ByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username))
}

func (s *Store) ByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// Create legt einen Benutzer an. 2FA wird beim ersten Login eingerichtet.
func (s *Store) Create(ctx context.Context, username, displayName, passwordHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, password_hash) VALUES (?, ?, ?)`,
		username, displayName, passwordHash)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RecordFailure zählt einen Fehlversuch und sperrt das Konto ab MaxFailedLogins.
// MariaDB wertet SET von links nach rechts aus, failed_logins ist im IF also schon erhöht.
// Liefert true, wenn das Konto jetzt gesperrt ist.
func (s *Store) RecordFailure(ctx context.Context, id int64) (bool, error) {
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET
			failed_logins = failed_logins + 1,
			locked_until = IF(failed_logins >= ?, UTC_TIMESTAMP(6) + INTERVAL ? SECOND, locked_until)
		WHERE id = ?`, MaxFailedLogins, int(LockDuration.Seconds()), id)
	if err != nil {
		return false, err
	}
	var failed int
	if err := s.db.QueryRowContext(ctx, `SELECT failed_logins FROM users WHERE id = ?`, id).Scan(&failed); err != nil {
		return false, err
	}
	return failed >= MaxFailedLogins, nil
}

// RecordSuccess setzt den Fehlerzähler zurück und merkt sich den TOTP-Schritt.
func (s *Store) RecordSuccess(ctx context.Context, id, totpStep int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET failed_logins = 0, locked_until = NULL, totp_last_step = ?, last_login_at = UTC_TIMESTAMP(6)
		WHERE id = ?`, totpStep, id)
	return err
}

// EnableTOTP speichert das verschlüsselte Secret und schaltet 2FA ein.
func (s *Store) EnableTOTP(ctx context.Context, id int64, sealedSecret []byte, step int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET totp_secret = ?, totp_enabled = TRUE, totp_last_step = ? WHERE id = ?`,
		sealedSecret, step, id)
	return err
}

// Audit schreibt ein Sicherheitsereignis. userID 0 heißt unbekannter Benutzer.
func (s *Store) Audit(ctx context.Context, userID int64, event, ip, userAgent string) error {
	var uid sql.NullInt64
	if userID > 0 {
		uid = sql.NullInt64{Int64: userID, Valid: true}
	}
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, event, ip, user_agent) VALUES (?, ?, ?, ?)`,
		uid, event, ip, userAgent)
	return err
}
