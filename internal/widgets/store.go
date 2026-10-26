package widgets

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Heyko112/codename-mwf/internal/tables"
)

var ErrNotFound = errors.New("Widget nicht gefunden")

// Widget ist ein installiertes Paket.
type Widget struct {
	ID          int64
	Slug        string
	Name        string
	Version     string
	Description string
	Size        string
	Entry       string
	Manifest    string
	Active      bool
	InstalledAt time.Time
	UpdatedAt   sql.NullTime
	FileCount   int
	TotalBytes  int64
	RoleIDs     []int64
}

// SizeLabel ist die Kachelgröße auf Deutsch.
func (w *Widget) SizeLabel() string { return Sizes[w.Size] }

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const widgetColumns = `w.id, w.slug, w.name, w.version, w.description, w.size, w.entry, w.manifest, w.is_active,
	w.installed_at, w.updated_at,
	(SELECT COUNT(*) FROM widget_files f WHERE f.widget_id = w.id),
	(SELECT COALESCE(SUM(LENGTH(f.data)), 0) FROM widget_files f WHERE f.widget_id = w.id),
	COALESCE((SELECT GROUP_CONCAT(r.role_id) FROM widget_roles r WHERE r.widget_id = w.id), '')`

func scanWidget(row interface{ Scan(...any) error }) (*Widget, error) {
	var w Widget
	var roles string
	err := row.Scan(&w.ID, &w.Slug, &w.Name, &w.Version, &w.Description, &w.Size, &w.Entry, &w.Manifest, &w.Active,
		&w.InstalledAt, &w.UpdatedAt, &w.FileCount, &w.TotalBytes, &roles)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, s := range strings.Split(roles, ",") {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			w.RoleIDs = append(w.RoleIDs, id)
		}
	}
	return &w, nil
}

// List liefert alle Widgets nach Name.
func (s *Store) List(ctx context.Context) ([]*Widget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+widgetColumns+` FROM widgets w ORDER BY w.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Widget
	for rows.Next() {
		w, err := scanWidget(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, w)
	}
	return list, rows.Err()
}

func (s *Store) ByID(ctx context.Context, id int64) (*Widget, error) {
	return scanWidget(s.db.QueryRowContext(ctx, `SELECT `+widgetColumns+` FROM widgets w WHERE w.id = ?`, id))
}

// Install legt ein Paket neu an oder ersetzt ein vorhandenes mit derselben ID aus dem Manifest.
// Beim Aktualisieren bleiben Sichtbarkeit und Aktiv-Schalter erhalten. Alles in einer Transaktion.
func (s *Store) Install(ctx context.Context, p *Package, userID int64) (id int64, oldVersion string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	m := p.Manifest
	err = tx.QueryRowContext(ctx, `SELECT id, version FROM widgets WHERE slug = ? FOR UPDATE`, m.ID).Scan(&id, &oldVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `
			INSERT INTO widgets (slug, name, version, description, size, entry, manifest, installed_by, installed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(6))`,
			m.ID, m.Name, m.Version, m.Description, m.Size, m.Entry, p.Raw, userID)
		if err != nil {
			return 0, "", err
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, "", err
		}
	case err != nil:
		return 0, "", err
	default:
		if _, err := tx.ExecContext(ctx, `
			UPDATE widgets SET name = ?, version = ?, description = ?, size = ?, entry = ?, manifest = ?, updated_at = UTC_TIMESTAMP(6)
			WHERE id = ?`, m.Name, m.Version, m.Description, m.Size, m.Entry, p.Raw, id); err != nil {
			return 0, "", err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM widget_files WHERE widget_id = ?`, id); err != nil {
			return 0, "", err
		}
	}
	if err := syncGrants(ctx, tx, id, m.Tables); err != nil {
		return 0, "", err
	}
	for path, f := range p.Files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO widget_files (widget_id, path, content_type, data) VALUES (?, ?, ?, ?)`,
			id, path, f.ContentType, f.Data); err != nil {
			return 0, "", err
		}
	}
	return id, oldVersion, tx.Commit()
}

// File liefert eine Datei eines Widgets.
func (s *Store) File(ctx context.Context, widgetID int64, path string) (string, []byte, error) {
	var ct string
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT content_type, data FROM widget_files WHERE widget_id = ? AND path = ?`, widgetID, path).
		Scan(&ct, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	return ct, data, err
}

// SetRoles ersetzt die Rollen, die das Widget sehen.
func (s *Store) SetRoles(ctx context.Context, widgetID int64, roleIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM widget_roles WHERE widget_id = ?`, widgetID); err != nil {
		return err
	}
	for _, id := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO widget_roles (widget_id, role_id) VALUES (?, ?)`, widgetID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetActive(ctx context.Context, widgetID int64, active bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE widgets SET is_active = ? WHERE id = ?`, active, widgetID)
	return err
}

func (s *Store) Delete(ctx context.Context, widgetID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM widgets WHERE id = ?`, widgetID)
	return err
}

// Grant ist eine angeforderte Tabelle mit dem, was der Admin zugeordnet und freigegeben hat.
type Grant struct {
	Alias       string
	Description string
	Requested   tables.Perms
	TableID     sql.NullInt64
	Granted     tables.Perms
}

// syncGrants gleicht die Freigaben mit dem Manifest ab. Bei einem Update bleibt eine Zuordnung erhalten,
// die Freigabe schrumpft aber auf das, was das neue Manifest noch anfordert. Neue Rechte gibt nur der Admin.
func syncGrants(ctx context.Context, tx *sql.Tx, widgetID int64, reqs []TableRequest) error {
	keep := []any{widgetID}
	for _, t := range reqs {
		r := t.Perms()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO widget_tables (widget_id, alias) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE can_read = can_read AND ?, can_create = can_create AND ?,
				can_update = can_update AND ?, can_delete = can_delete AND ?`,
			widgetID, t.Alias, r.Read, r.Create, r.Update, r.Delete); err != nil {
			return err
		}
		keep = append(keep, t.Alias)
	}
	q := `DELETE FROM widget_tables WHERE widget_id = ?`
	if len(keep) > 1 {
		q += ` AND alias NOT IN (?` + strings.Repeat(",?", len(keep)-2) + `)`
	}
	_, err := tx.ExecContext(ctx, q, keep...)
	return err
}

// Grants liefert die angeforderten Tabellen eines Widgets in der Reihenfolge des Manifests.
func (s *Store) Grants(ctx context.Context, w *Widget) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT alias, table_id, can_read, can_create, can_update, can_delete FROM widget_tables WHERE widget_id = ?`, w.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stored := map[string]Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.Alias, &g.TableID, &g.Granted.Read, &g.Granted.Create, &g.Granted.Update, &g.Granted.Delete); err != nil {
			return nil, err
		}
		stored[g.Alias] = g
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []Grant
	for _, t := range ParseManifest(w.Manifest).Tables {
		g := stored[t.Alias]
		g.Alias, g.Description, g.Requested = t.Alias, t.Description, t.Perms()
		out = append(out, g)
	}
	return out, nil
}

// SetGrant ordnet einem Alias eine Tabelle zu (0 = keine) und setzt die Freigabe,
// höchstens so viel, wie das Manifest anfordert.
func (s *Store) SetGrant(ctx context.Context, w *Widget, alias string, tableID int64, p tables.Perms) error {
	var req *TableRequest
	for _, t := range ParseManifest(w.Manifest).Tables {
		if t.Alias == alias {
			req = &t
			break
		}
	}
	if req == nil {
		return ErrNotFound
	}
	r := req.Perms()
	p = tables.Perms{Read: p.Read && r.Read, Create: p.Create && r.Create, Update: p.Update && r.Update, Delete: p.Delete && r.Delete}
	tid := sql.NullInt64{Int64: tableID, Valid: tableID > 0}
	if !tid.Valid {
		p = tables.Perms{}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO widget_tables (widget_id, alias, table_id, can_read, can_create, can_update, can_delete) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE table_id = VALUES(table_id), can_read = VALUES(can_read), can_create = VALUES(can_create),
			can_update = VALUES(can_update), can_delete = VALUES(can_delete)`,
		w.ID, alias, tid, p.Read, p.Create, p.Update, p.Delete)
	return err
}
