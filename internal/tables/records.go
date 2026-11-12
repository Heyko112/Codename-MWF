package tables

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Perms sind die Rechte an den Datensätzen einer Tabelle.
type Perms struct {
	Read, Create, Update, Delete bool
}

// Any meldet, ob überhaupt ein Recht besteht. Anlegen, Ändern und Löschen schließen Lesen ein.
func (p Perms) Any() bool { return p.Read || p.Create || p.Update || p.Delete }

func (p Perms) or(q Perms) Perms {
	return Perms{p.Read || q.Read, p.Create || q.Create, p.Update || q.Update, p.Delete || q.Delete}
}

// AllPerms sind die Rechte der Rolle „Administratoren“.
var AllPerms = Perms{true, true, true, true}

// RolePerms liefert die Rechte je Rolle für eine Tabelle.
func (s *Store) RolePerms(ctx context.Context, tableID int64) (map[int64]Perms, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT role_id, can_read, can_create, can_update, can_delete FROM table_permissions WHERE table_id = ?`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]Perms{}
	for rows.Next() {
		var id int64
		var p Perms
		if err := rows.Scan(&id, &p.Read, &p.Create, &p.Update, &p.Delete); err != nil {
			return nil, err
		}
		m[id] = p
	}
	return m, rows.Err()
}

// SetRolePerms ersetzt die Rechte aller Rollen an einer Tabelle.
func (s *Store) SetRolePerms(ctx context.Context, tableID int64, perms map[int64]Perms) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM table_permissions WHERE table_id = ?`, tableID); err != nil {
		return err
	}
	for roleID, p := range perms {
		if !p.Any() {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO table_permissions (table_id, role_id, can_read, can_create, can_update, can_delete)
			VALUES (?, ?, ?, ?, ?, ?)`, tableID, roleID, p.Read, p.Create, p.Update, p.Delete); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// userPerms fasst die Rechte ohne Filter der Rollen in „in“ je Tabelle zusammen (additiv).
func (s *Store) userPerms(ctx context.Context, in string, args []any) (map[int64]Perms, error) {
	m := map[int64]Perms{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT table_id, can_read, can_create, can_update, can_delete FROM table_permissions
		WHERE role_id IN `+in, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var p Perms
		if err := rows.Scan(&id, &p.Read, &p.Create, &p.Update, &p.Delete); err != nil {
			return nil, err
		}
		m[id] = m[id].or(p)
	}
	return m, rows.Err()
}

// Record ist ein Datensatz. Values ist nach Spalten-ID geordnet, roh wie vom Treiber.
type Record struct {
	ID        int64
	Values    map[int64]any
	CreatedAt time.Time
	CreatedBy sql.NullInt64
	UpdatedAt sql.NullTime
	UpdatedBy sql.NullInt64
}

func selectList(t *Table) string {
	cols := []string{"id", "created_at", "created_by", "updated_at", "updated_by"}
	for _, c := range t.Columns {
		cols = append(cols, columnIdent(c.ID))
	}
	return strings.Join(cols, ", ")
}

func scanRecord(t *Table, row interface{ Scan(...any) error }) (*Record, error) {
	r := &Record{Values: map[int64]any{}}
	vals := make([]any, len(t.Columns))
	dest := []any{&r.ID, &r.CreatedAt, &r.CreatedBy, &r.UpdatedAt, &r.UpdatedBy}
	for i := range vals {
		dest = append(dest, &vals[i])
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	for i, c := range t.Columns {
		r.Values[c.ID] = vals[i]
	}
	return r, nil
}

// searchable sind Spalten, in denen die Suche sucht.
func searchable(c *Column) bool { return c.Type == "text" || c.Type == "longtext" }

// Sort ist eine Sortierung nach Spalte (ColCreatedBy = nach ID).
type Sort struct {
	Col  int64
	Desc bool
}

// Query beschreibt eine Abfrage. Scope sind die erlaubten Zeilen, Where und Search schränken weiter ein.
type Query struct {
	Scope  Scope
	Search string
	Where  []Cond
	Vars   Vars
	Order  []Sort
	Offset int
	Limit  int
}

// Records liefert eine Seite Datensätze, standardmäßig neueste zuerst.
func (s *Store) Records(ctx context.Context, t *Table, q Query) ([]*Record, int64, error) {
	sw, args := q.Scope.where()
	conds := []string{"(" + sw + ")"}
	if len(q.Where) > 0 {
		w, wa := CondsSQL(t, q.Where, q.Vars)
		conds = append(conds, "("+w+")")
		args = append(args, wa...)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		var ors []string
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(search) + "%"
		for _, c := range t.Columns {
			if searchable(c) {
				ors = append(ors, columnIdent(c.ID)+" LIKE ?")
				args = append(args, like)
			}
		}
		if id, err := parseID(search); err == nil {
			ors = append(ors, "id = ?")
			args = append(args, id)
		}
		if len(ors) == 0 {
			return nil, 0, nil
		}
		conds = append(conds, "("+strings.Join(ors, " OR ")+")")
	}
	where := " WHERE " + strings.Join(conds, " AND ")
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableIdent(t.ID)+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	var order []string
	for _, o := range q.Order {
		ident := "id"
		if o.Col != ColCreatedBy {
			c := columnByID(t, o.Col)
			if c == nil {
				continue
			}
			ident = columnIdent(c.ID)
		}
		if o.Desc {
			ident += " DESC"
		}
		order = append(order, ident)
	}
	order = append(order, "id DESC")
	rows, err := s.db.QueryContext(ctx, `SELECT `+selectList(t)+` FROM `+tableIdent(t.ID)+where+` ORDER BY `+strings.Join(order, ", ")+` LIMIT ? OFFSET ?`,
		append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*Record
	for rows.Next() {
		r, err := scanRecord(t, rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	return list, total, rows.Err()
}

func parseID(s string) (int64, error) {
	var id int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("keine ID")
		}
		id = id*10 + int64(ch-'0')
		if id > 1<<53 {
			return 0, errors.New("zu groß")
		}
	}
	return id, nil
}

// RecordByID liefert einen Datensatz, wenn er im Scope liegt.
func (s *Store) RecordByID(ctx context.Context, t *Table, id int64, scope Scope) (*Record, error) {
	sw, args := scope.where()
	r, err := scanRecord(t, s.db.QueryRowContext(ctx, `SELECT `+selectList(t)+` FROM `+tableIdent(t.ID)+` WHERE id = ? AND (`+sw+`)`,
		append([]any{id}, args...)...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// InScope meldet, ob der Datensatz im Scope liegt.
func (s *Store) InScope(ctx context.Context, t *Table, id int64, scope Scope) (bool, error) {
	if scope.all {
		return true, nil
	}
	return inScope(ctx, s.db, t, id, scope)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func inScope(ctx context.Context, q queryer, t *Table, id int64, scope Scope) (bool, error) {
	sw, args := scope.where()
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableIdent(t.ID)+` WHERE id = ? AND (`+sw+`)`, append([]any{id}, args...)...).Scan(&n)
	return n > 0, err
}

// InsertRecord legt einen Datensatz an. values enthält bereits geprüfte Werte je Spalten-ID.
// Liegt der neue Datensatz nicht im Scope, wird nichts gespeichert (ErrOutOfScope).
func (s *Store) InsertRecord(ctx context.Context, t *Table, values map[int64]any, userID int64, scope Scope) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Zeiten immer in UTC speichern, angezeigt wird in der Zeitzone der App
	cols, marks, args := []string{"created_at", "created_by"}, []string{"UTC_TIMESTAMP(6)", "?"}, []any{userRef(userID)}
	for _, c := range t.Columns {
		cols = append(cols, columnIdent(c.ID))
		marks = append(marks, "?")
		args = append(args, values[c.ID])
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO `+tableIdent(t.ID)+` (`+strings.Join(cols, ", ")+`) VALUES (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if !scope.all {
		ok, err := inScope(ctx, tx, t, id, scope)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, ErrOutOfScope
		}
	}
	return id, tx.Commit()
}

// UpdateRecord speichert die Spalten, die in values vorkommen. Fehlende Spalten bleiben unverändert.
// Der Datensatz muss vorher und nachher im Scope liegen.
func (s *Store) UpdateRecord(ctx context.Context, t *Table, id int64, values map[int64]any, userID int64, scope Scope) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sets, args := []string{"updated_at = UTC_TIMESTAMP(6)", "updated_by = ?"}, []any{userRef(userID)}
	for _, c := range t.Columns {
		if v, ok := values[c.ID]; ok {
			sets = append(sets, columnIdent(c.ID)+" = ?")
			args = append(args, v)
		}
	}
	sw, sargs := scope.where()
	args = append(append(args, id), sargs...)
	res, err := tx.ExecContext(ctx, `UPDATE `+tableIdent(t.ID)+` SET `+strings.Join(sets, ", ")+` WHERE id = ? AND (`+sw+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if !scope.all {
		ok, err := inScope(ctx, tx, t, id, scope)
		if err != nil {
			return err
		}
		if !ok {
			return ErrOutOfScope
		}
	}
	return tx.Commit()
}

// DeleteRecord löscht einen Datensatz im Scope.
func (s *Store) DeleteRecord(ctx context.Context, t *Table, id int64, scope Scope) error {
	sw, args := scope.where()
	res, err := s.db.ExecContext(ctx, `DELETE FROM `+tableIdent(t.ID)+` WHERE id = ? AND (`+sw+`)`, append([]any{id}, args...)...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
