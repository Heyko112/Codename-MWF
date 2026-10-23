package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
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
	IsAdmin      bool
	PasswordHash string
	TOTPSecret   []byte // verschlüsselt
	TOTPEnabled  bool
	TOTPLastStep int64
	IsActive     bool
	FailedLogins int
	LockedUntil  sql.NullTime
	LastLoginAt  sql.NullTime
}

// Locked meldet, ob das Konto gerade gesperrt ist.
func (u *User) Locked(now time.Time) bool {
	return u.LockedUntil.Valid && u.LockedUntil.Time.After(now)
}

// LockedNow ist Locked zum aktuellen Zeitpunkt (für Templates).
func (u *User) LockedNow() bool { return u.Locked(time.Now()) }

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const userColumns = `id, username, display_name, is_admin, password_hash, totp_secret, totp_enabled,
	totp_last_step, is_active, failed_logins, locked_until, last_login_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.PasswordHash, &u.TOTPSecret, &u.TOTPEnabled,
		&u.TOTPLastStep, &u.IsActive, &u.FailedLogins, &u.LockedUntil, &u.LastLoginAt)
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

// ErrUsernameTaken meldet einen bereits vergebenen Benutzernamen.
var ErrUsernameTaken = errors.New("Benutzername ist bereits vergeben")

// Create legt einen Benutzer an. 2FA wird beim ersten Login eingerichtet.
func (s *Store) Create(ctx context.Context, username, displayName, passwordHash string, isAdmin bool) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, password_hash, is_admin) VALUES (?, ?, ?, ?)`,
		username, displayName, passwordHash, isAdmin)
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1062 {
		return 0, ErrUsernameTaken
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// List liefert alle Benutzer, sortiert nach Benutzername.
func (s *Store) List(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// SetActive sperrt oder entsperrt ein Konto. Beim Entsperren wird auch die Fehlversuch-Sperre aufgehoben.
func (s *Store) SetActive(ctx context.Context, id int64, active bool) error {
	q := `UPDATE users SET is_active = FALSE WHERE id = ?`
	if active {
		q = `UPDATE users SET is_active = TRUE, failed_logins = 0, locked_until = NULL WHERE id = ?`
	}
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.ByID(ctx, id); err != nil {
			return err
		}
	}
	return nil
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

// Audit schreibt ein Sicherheitsereignis. userID 0 heißt unbekannter Benutzer, detail darf leer sein.
func (s *Store) Audit(ctx context.Context, userID int64, event, detail, ip, userAgent string) error {
	var uid sql.NullInt64
	if userID > 0 {
		uid = sql.NullInt64{Int64: userID, Valid: true}
	}
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	var det sql.NullString
	if detail != "" {
		det = sql.NullString{String: detail, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, event, detail, ip, user_agent) VALUES (?, ?, ?, ?, ?)`,
		uid, event, det, ip, userAgent)
	return err
}
