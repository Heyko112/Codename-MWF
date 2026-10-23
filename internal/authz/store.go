package authz

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
)

var (
	ErrNotFound    = errors.New("Rolle nicht gefunden")
	ErrNameTaken   = errors.New("Den Rollennamen gibt es schon.")
	ErrSystemRole  = errors.New("Die Rolle „Administratoren“ kann nicht gelöscht werden.")
	ErrLastAdmin   = errors.New("Die Rolle „Administratoren“ braucht mindestens ein Mitglied.")
	ErrUnknownPerm = errors.New("unbekanntes Recht")
)

// Role ist eine Rolle mit allem, was die Verwaltung anzeigt.
type Role struct {
	ID          int64
	Name        string
	Description string
	Hidden      bool
	System      bool
	Permissions []string
	MemberCount int
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Graph lädt alle Rollen, Rechte und Verschachtelungen.
func (s *Store) Graph(ctx context.Context) (*Graph, error) {
	roles, err := s.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	infos := make([]*RoleInfo, len(roles))
	for i, r := range roles {
		infos[i] = &RoleInfo{ID: r.ID, Name: r.Name, System: r.System, Permissions: r.Permissions}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT parent_id, child_id FROM role_includes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var includes [][2]int64
	for rows.Next() {
		var e [2]int64
		if err := rows.Scan(&e[0], &e[1]); err != nil {
			return nil, err
		}
		includes = append(includes, e)
	}
	return NewGraph(infos, includes), rows.Err()
}

// ListRoles liefert alle Rollen, Systemrolle zuerst, dann nach Name.
func (s *Store) ListRoles(ctx context.Context) ([]*Role, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.name, r.description, r.is_hidden, r.is_system,
		       (SELECT COUNT(*) FROM role_members m WHERE m.role_id = r.id),
		       COALESCE((SELECT GROUP_CONCAT(p.permission ORDER BY p.permission) FROM role_permissions p WHERE p.role_id = r.id), '')
		FROM roles r ORDER BY r.is_system DESC, r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []*Role
	for rows.Next() {
		var r Role
		var perms string
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Hidden, &r.System, &r.MemberCount, &perms); err != nil {
			return nil, err
		}
		if perms != "" {
			r.Permissions = strings.Split(perms, ",")
		}
		roles = append(roles, &r)
	}
	return roles, rows.Err()
}

func (s *Store) RoleByID(ctx context.Context, id int64) (*Role, error) {
	roles, err := s.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, ErrNotFound
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func (s *Store) CreateRole(ctx context.Context, name, description string, hidden bool) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO roles (name, description, is_hidden) VALUES (?, ?, ?)`, name, description, hidden)
	if isDuplicate(err) {
		return 0, ErrNameTaken
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateRole ändert Name, Beschreibung und „versteckt“. Die Systemrolle behält ihren Namen.
func (s *Store) UpdateRole(ctx context.Context, id int64, name, description string, hidden bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE roles SET name = IF(is_system, name, ?), description = ?, is_hidden = ? WHERE id = ?`,
		name, description, hidden, id)
	if isDuplicate(err) {
		return ErrNameTaken
	}
	return err
}

func (s *Store) DeleteRole(ctx context.Context, id int64) error {
	r, err := s.RoleByID(ctx, id)
	if err != nil {
		return err
	}
	if r.System {
		return ErrSystemRole
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, id)
	return err
}

// SetPermissions ersetzt die Rechte einer Rolle. Die Systemrolle hat ohnehin alle.
func (s *Store) SetPermissions(ctx context.Context, roleID int64, perms []string) error {
	for _, p := range perms {
		if PermissionLabel(p) == p {
			return ErrUnknownPerm
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, p := range perms {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)`, roleID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddInclude legt child in parent, nach Prüfung auf Kreise und Tiefe.
func (s *Store) AddInclude(ctx context.Context, parent, child int64) error {
	g, err := s.Graph(ctx)
	if err != nil {
		return err
	}
	if g.Roles[parent] == nil || g.Roles[child] == nil {
		return ErrNotFound
	}
	if err := g.CheckInclude(parent, child); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO role_includes (parent_id, child_id) VALUES (?, ?)`, parent, child)
	return err
}

func (s *Store) RemoveInclude(ctx context.Context, parent, child int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM role_includes WHERE parent_id = ? AND child_id = ?`, parent, child)
	return err
}

// UserRoleIDs liefert die direkten Rollen eines Benutzers.
func (s *Store) UserRoleIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT role_id FROM role_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AllUserRoleIDs liefert für alle Benutzer die direkten Rollen (für Listen).
func (s *Store) AllUserRoleIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, role_id FROM role_members ORDER BY role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64][]int64{}
	for rows.Next() {
		var uid, rid int64
		if err := rows.Scan(&uid, &rid); err != nil {
			return nil, err
		}
		m[uid] = append(m[uid], rid)
	}
	return m, rows.Err()
}

// SetUserRoles ersetzt die direkten Rollen eines Benutzers.
// Verhindert, dass die Systemrolle ihr letztes Mitglied verliert.
func (s *Store) SetUserRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var systemID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE is_system LIMIT 1 FOR UPDATE`).Scan(&systemID); err != nil {
		return err
	}
	keepsSystem := false
	for _, id := range roleIDs {
		if id == systemID {
			keepsSystem = true
		}
	}
	if !keepsSystem {
		var others int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM role_members WHERE role_id = ? AND user_id <> ?`, systemID, userID).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return ErrLastAdmin
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM role_members WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, id := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_members (role_id, user_id) VALUES (?, ?)`, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddMember fügt einen Benutzer einer Rolle hinzu (z. B. beim Einrichten per Kommandozeile).
func (s *Store) AddMember(ctx context.Context, roleID, userID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO role_members (role_id, user_id) VALUES (?, ?)`, roleID, userID)
	return err
}

// SystemRoleID liefert die ID der Rolle „Administratoren“.
func (s *Store) SystemRoleID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM roles WHERE is_system LIMIT 1`).Scan(&id)
	return id, err
}

// Access löst die Rechte eines Benutzers auf.
func (s *Store) Access(ctx context.Context, userID int64) (*Graph, *Access, error) {
	g, err := s.Graph(ctx)
	if err != nil {
		return nil, nil, err
	}
	direct, err := s.UserRoleIDs(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return g, g.Resolve(direct), nil
}
