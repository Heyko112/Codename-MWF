package tables

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

var ErrNotFound = errors.New("Tabelle oder Spalte nicht gefunden")

// InputError ist ein Fehler in der Eingabe, den der Benutzer selbst beheben kann.
type InputError string

func (e InputError) Error() string { return string(e) }

const (
	ErrNameTaken = InputError("Den Namen gibt es hier schon.")
	ErrBadName   = InputError("Der Name muss 1 bis 64 Zeichen lang sein.")
	ErrBadDesc   = InputError("Die Beschreibung darf höchstens 255 Zeichen lang sein.")
	ErrBadType   = InputError("Unbekannter Feldtyp.")
	ErrShrink    = InputError("Die Länge kann nur vergrößert werden, sonst gehen Daten verloren.")
)

// Table ist ein Eintrag im Tabellenverzeichnis.
type Table struct {
	ID          int64
	Name        string
	Description string
	ColumnCount int
	RowCount    int64
	Columns     []*Column
}

// Column ist eine Spalte einer Tabelle.
type Column struct {
	ID       int64
	TableID  int64
	Name     string
	Type     string
	Size     int
	Required bool
	Position int
}

// TypeLabel beschreibt den Typ für die Anzeige, z. B. „Text (255)“.
func (c *Column) TypeLabel() string {
	t := TypeByCode(c.Type)
	if t == nil {
		return c.Type
	}
	if t.SizeLabel != "" {
		return t.Label + " (" + strconv.Itoa(c.Size) + ")"
	}
	return t.Label
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// CheckName prüft einen Tabellen- oder Spaltennamen und gibt ihn bereinigt zurück.
func CheckName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return name, ErrBadName
	}
	return name, nil
}

// List liefert alle Tabellen nach Name, mit Anzahl Spalten und Datensätze.
func (s *Store) List(ctx context.Context) ([]*Table, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.description, (SELECT COUNT(*) FROM data_columns c WHERE c.table_id = t.id)
		FROM data_tables t ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.ColumnCount); err != nil {
			return nil, err
		}
		list = append(list, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, t := range list {
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableIdent(t.ID)).Scan(&t.RowCount); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// ByID liefert eine Tabelle mit ihren Spalten in Anzeigereihenfolge.
func (s *Store) ByID(ctx context.Context, id int64) (*Table, error) {
	var t Table
	err := s.db.QueryRowContext(ctx, `SELECT id, name, description FROM data_tables WHERE id = ?`, id).
		Scan(&t.ID, &t.Name, &t.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, table_id, name, type, size, required, position
		FROM data_columns WHERE table_id = ? ORDER BY position, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.ID, &c.TableID, &c.Name, &c.Type, &c.Size, &c.Required, &c.Position); err != nil {
			return nil, err
		}
		t.Columns = append(t.Columns, &c)
	}
	t.ColumnCount = len(t.Columns)
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableIdent(id)).Scan(&t.RowCount)
	return &t, err
}

// Create trägt die Tabelle ins Verzeichnis ein und legt dt_<id> an.
// DDL lässt sich in MariaDB nicht zurückrollen, deshalb wird bei einem Fehler der Eintrag wieder entfernt.
func (s *Store) Create(ctx context.Context, name, description string, userID int64) (int64, error) {
	if err := checkTable(&name, description); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO data_tables (name, description, created_by) VALUES (?, ?, ?)`,
		name, description, userID)
	if isDuplicate(err) {
		return 0, ErrNameTaken
	}
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx, `CREATE TABLE `+tableIdent(id)+` (
		id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		created_at DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		created_by BIGINT UNSIGNED NULL,
		updated_at DATETIME(6)     NULL,
		updated_by BIGINT UNSIGNED NULL,
		PRIMARY KEY (id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		s.db.ExecContext(ctx, `DELETE FROM data_tables WHERE id = ?`, id)
		return 0, fmt.Errorf("Tabelle anlegen: %w", err)
	}
	return id, nil
}

func checkTable(name *string, description string) error {
	n, err := CheckName(*name)
	*name = n
	if err != nil {
		return err
	}
	if utf8.RuneCountInString(description) > 255 {
		return ErrBadDesc
	}
	return nil
}

// Update ändert Name und Beschreibung. Die echte Tabelle bleibt unverändert.
func (s *Store) Update(ctx context.Context, id int64, name, description string) error {
	if err := checkTable(&name, description); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE data_tables SET name = ?, description = ? WHERE id = ?`, name, description, id)
	if isDuplicate(err) {
		return ErrNameTaken
	}
	return err
}

// Delete löscht die Tabelle samt allen Datensätzen.
func (s *Store) Delete(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+tableIdent(id)); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM data_tables WHERE id = ?`, id)
	return err
}

// Column liefert eine Spalte einer Tabelle.
func (s *Store) Column(ctx context.Context, tableID, colID int64) (*Column, error) {
	var c Column
	err := s.db.QueryRowContext(ctx, `
		SELECT id, table_id, name, type, size, required, position
		FROM data_columns WHERE id = ? AND table_id = ?`, colID, tableID).
		Scan(&c.ID, &c.TableID, &c.Name, &c.Type, &c.Size, &c.Required, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// CheckSize prüft die Zahl zum Feldtyp und setzt bei Typen ohne Zahl 0.
func CheckSize(t *ColType, size int) (int, error) {
	if t.SizeLabel == "" {
		return 0, nil
	}
	if size < t.MinSize || size > t.MaxSize {
		return size, InputError(fmt.Sprintf("%s muss zwischen %d und %d liegen.", t.SizeLabel, t.MinSize, t.MaxSize))
	}
	return size, nil
}

// AddColumn hängt eine Spalte hinten an.
func (s *Store) AddColumn(ctx context.Context, tableID int64, name, typ string, size int, required bool) (int64, error) {
	name, err := CheckName(name)
	if err != nil {
		return 0, err
	}
	t := TypeByCode(typ)
	if t == nil {
		return 0, ErrBadType
	}
	if size, err = CheckSize(t, size); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO data_columns (table_id, name, type, size, required, position)
		SELECT ?, ?, ?, ?, ?, COALESCE(MAX(position), 0) + 1 FROM data_columns WHERE table_id = ?`,
		tableID, name, typ, size, required, tableID)
	if isDuplicate(err) {
		return 0, ErrNameTaken
	}
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+tableIdent(tableID)+` ADD COLUMN `+columnIdent(id)+` `+sqlType(typ, size)+` NULL`)
	if err != nil {
		s.db.ExecContext(ctx, `DELETE FROM data_columns WHERE id = ?`, id)
		return 0, fmt.Errorf("Spalte anlegen: %w", err)
	}
	return id, nil
}

// UpdateColumn ändert Name und Pflichtfeld, bei Text auch die Länge (nur größer).
func (s *Store) UpdateColumn(ctx context.Context, c *Column, name string, size int, required bool) error {
	name, err := CheckName(name)
	if err != nil {
		return err
	}
	t := TypeByCode(c.Type)
	if t == nil {
		return ErrBadType
	}
	if c.Type != "text" {
		size = c.Size
	}
	if size, err = CheckSize(t, size); err != nil {
		return err
	}
	if size < c.Size {
		return ErrShrink
	}
	_, err = s.db.ExecContext(ctx, `UPDATE data_columns SET name = ?, required = ? WHERE id = ?`, name, required, c.ID)
	if isDuplicate(err) {
		return ErrNameTaken
	}
	if err != nil {
		return err
	}
	if size == c.Size {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+tableIdent(c.TableID)+` MODIFY COLUMN `+columnIdent(c.ID)+` `+sqlType(c.Type, size)+` NULL`)
	if err != nil {
		return fmt.Errorf("Länge ändern: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE data_columns SET size = ? WHERE id = ?`, size, c.ID)
	return err
}

// MoveColumn tauscht die Spalte mit ihrem Nachbarn (dir -1 = nach oben, +1 = nach unten).
// Die Reihenfolge ist nur Anzeige, die echte Tabelle bleibt unverändert.
func (s *Store) MoveColumn(ctx context.Context, c *Column, dir int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM data_columns WHERE table_id = ? ORDER BY position, id FOR UPDATE`, c.TableID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, id := range ids {
		if id == c.ID {
			j := i + dir
			if j >= 0 && j < len(ids) {
				ids[i], ids[j] = ids[j], ids[i]
			}
			break
		}
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE data_columns SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteColumn entfernt die Spalte samt ihren Werten.
func (s *Store) DeleteColumn(ctx context.Context, c *Column) error {
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+tableIdent(c.TableID)+` DROP COLUMN IF EXISTS `+columnIdent(c.ID)); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM data_columns WHERE id = ?`, c.ID)
	return err
}
