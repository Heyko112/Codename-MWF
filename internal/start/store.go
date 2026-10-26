// Package start verwaltet die Startseite: Bereiche (Reiter) je Rolle, Kacheln und was Benutzer ausblenden.
package start

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// InputError ist ein Fehler in der Eingabe, den der Benutzer selbst beheben kann.
type InputError string

func (e InputError) Error() string { return string(e) }

var (
	ErrNotFound  = errors.New("nicht gefunden")
	ErrNameTaken = InputError("Einen Bereich mit diesem Namen gibt es schon.")
	ErrBadName   = InputError("Der Name muss 1 bis 64 Zeichen lang sein.")
	ErrTileTaken = InputError("Das Widget liegt in diesem Bereich schon.")
)

// Section ist ein Bereich der Startseite. Ohne RoleIDs sehen ihn alle.
type Section struct {
	ID       int64
	Name     string
	Position int
	RoleIDs  []int64
	Tiles    []Tile
}

// Tile ist ein Widget in einem Bereich.
type Tile struct {
	ID        int64
	SectionID int64
	WidgetID  int64
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Sections liefert alle Bereiche in ihrer Reihenfolge, mit Rollen und Kacheln.
func (s *Store) Sections(ctx context.Context) ([]*Section, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.position,
			COALESCE((SELECT GROUP_CONCAT(r.role_id) FROM start_section_roles r WHERE r.section_id = s.id), '')
		FROM start_sections s ORDER BY s.position, s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Section
	byID := map[int64]*Section{}
	for rows.Next() {
		var sec Section
		var roles string
		if err := rows.Scan(&sec.ID, &sec.Name, &sec.Position, &roles); err != nil {
			return nil, err
		}
		for _, x := range strings.Split(roles, ",") {
			if id, err := strconv.ParseInt(x, 10, 64); err == nil {
				sec.RoleIDs = append(sec.RoleIDs, id)
			}
		}
		list = append(list, &sec)
		byID[sec.ID] = &sec
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	trows, err := s.db.QueryContext(ctx, `SELECT id, section_id, widget_id FROM start_tiles ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var t Tile
		if err := trows.Scan(&t.ID, &t.SectionID, &t.WidgetID); err != nil {
			return nil, err
		}
		if sec := byID[t.SectionID]; sec != nil {
			sec.Tiles = append(sec.Tiles, t)
		}
	}
	return list, trows.Err()
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", ErrBadName
	}
	return name, nil
}

func isDuplicate(err error) bool { return err != nil && strings.Contains(err.Error(), "Error 1062") }

// CreateSection legt einen Bereich am Ende an.
func (s *Store) CreateSection(ctx context.Context, name string) (int64, error) {
	name, err := checkName(name)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO start_sections (name, position) SELECT ?, COALESCE(MAX(position), 0) + 1 FROM start_sections`, name)
	if isDuplicate(err) {
		return 0, ErrNameTaken
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RenameSection benennt einen Bereich um.
func (s *Store) RenameSection(ctx context.Context, id int64, name string) error {
	name, err := checkName(name)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE start_sections SET name = ? WHERE id = ?`, name, id)
	if isDuplicate(err) {
		return ErrNameTaken
	}
	return err
}

// DeleteSection löscht einen Bereich mit seinen Kacheln.
func (s *Store) DeleteSection(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM start_sections WHERE id = ?`, id)
	return err
}

// SetSectionRoles ersetzt die Rollen eines Bereichs.
func (s *Store) SetSectionRoles(ctx context.Context, id int64, roleIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM start_section_roles WHERE section_id = ?`, id); err != nil {
		return err
	}
	for _, r := range roleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO start_section_roles (section_id, role_id) VALUES (?, ?)`, id, r); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MoveSection verschiebt einen Bereich um eine Stelle (-1 = nach vorn, +1 = nach hinten).
func (s *Store) MoveSection(ctx context.Context, id int64, dir int) error {
	return s.move(ctx, `SELECT id FROM start_sections ORDER BY position, id`, `UPDATE start_sections SET position = ? WHERE id = ?`, id, dir)
}

// AddTile hängt ein Widget an einen Bereich an.
func (s *Store) AddTile(ctx context.Context, sectionID, widgetID int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO start_tiles (section_id, widget_id, position)
		SELECT ?, ?, COALESCE(MAX(position), 0) + 1 FROM start_tiles WHERE section_id = ?`, sectionID, widgetID, sectionID)
	if isDuplicate(err) {
		return ErrTileTaken
	}
	return err
}

// TileByID liefert eine Kachel.
func (s *Store) TileByID(ctx context.Context, id int64) (*Tile, error) {
	var t Tile
	err := s.db.QueryRowContext(ctx, `SELECT id, section_id, widget_id FROM start_tiles WHERE id = ?`, id).Scan(&t.ID, &t.SectionID, &t.WidgetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// RemoveTile entfernt eine Kachel.
func (s *Store) RemoveTile(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM start_tiles WHERE id = ?`, id)
	return err
}

// MoveTile verschiebt eine Kachel innerhalb ihres Bereichs.
func (s *Store) MoveTile(ctx context.Context, t *Tile, dir int) error {
	return s.move(ctx, `SELECT id FROM start_tiles WHERE section_id = `+strconv.FormatInt(t.SectionID, 10)+` ORDER BY position, id`,
		`UPDATE start_tiles SET position = ? WHERE id = ?`, t.ID, dir)
}

// move tauscht ein Element mit seinem Nachbarn und nummeriert alle neu.
func (s *Store) move(ctx context.Context, list, update string, id int64, dir int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, list+` FOR UPDATE`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, x := range ids {
		if x == id && i+dir >= 0 && i+dir < len(ids) {
			ids[i], ids[i+dir] = ids[i+dir], ids[i]
			break
		}
	}
	for i, x := range ids {
		if _, err := tx.ExecContext(ctx, update, i+1, x); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Hidden liefert die Kacheln, die ein Benutzer ausgeblendet hat.
func (s *Store) Hidden(ctx context.Context, userID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tile_id FROM start_hidden WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		m[id] = true
	}
	return m, rows.Err()
}

// SetHidden blendet eine Kachel für einen Benutzer aus oder wieder ein.
func (s *Store) SetHidden(ctx context.Context, userID, tileID int64, hidden bool) error {
	q := `DELETE FROM start_hidden WHERE user_id = ? AND tile_id = ?`
	if hidden {
		q = `INSERT IGNORE INTO start_hidden (user_id, tile_id) VALUES (?, ?)`
	}
	_, err := s.db.ExecContext(ctx, q, userID, tileID)
	return err
}
