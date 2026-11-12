package tables

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Zeilenfilter (Schritt E2). Eine Regel gibt einer Rolle Rechte an den Zeilen einer Tabelle,
// die alle Bedingungen der Regel erfüllen. Wie überall in MWF gilt additiv: Ein Benutzer darf
// eine Zeile, wenn irgendeine seiner Regeln (oder ein Recht ohne Filter) sie erlaubt.
// Geprüft wird immer in der Datenbank, beim Lesen und beim Schreiben, damit es nur eine Wahrheit gibt.

// ColCreatedBy steht in einer Bedingung für „angelegt von“.
const ColCreatedBy int64 = 0

// Platzhalter in Bedingungen. MWF setzt sie bei jeder Abfrage ein.
const (
	PlaceholderMe    = "{ich}"
	PlaceholderToday = "{heute}"
)

// Cond ist eine Bedingung: Spalte, Vergleich, Wert (wie eingegeben oder Platzhalter).
type Cond struct {
	Col int64  `json:"col"`
	Op  string `json:"op"`
	Val string `json:"val"`
}

// Op ist ein Vergleich für die Oberfläche.
type Op struct {
	Code    string
	Label   string
	NoValue bool
}

// Ops in der Reihenfolge der Auswahl.
var Ops = []Op{
	{"eq", "ist gleich", false},
	{"ne", "ist nicht", false},
	{"lt", "kleiner als", false},
	{"le", "höchstens", false},
	{"gt", "größer als", false},
	{"ge", "mindestens", false},
	{"contains", "enthält", false},
	{"empty", "ist leer", true},
	{"notempty", "ist nicht leer", true},
}

func opByCode(code string) *Op {
	for i := range Ops {
		if Ops[i].Code == code {
			return &Ops[i]
		}
	}
	return nil
}

// Rule ist eine gespeicherte Regel.
type Rule struct {
	ID      int64
	TableID int64
	RoleID  int64
	Perms   Perms
	Conds   []Cond
}

// Grant ist ein wirksames Recht an einer Tabelle. Conds == nil heißt: alle Zeilen.
// Read ist gesetzt, sobald irgendein Recht besteht (Anlegen, Ändern und Löschen schließen Lesen ein).
type Grant struct {
	Perms Perms
	Conds []Cond
}

// Access sind alle wirksamen Rechte eines Benutzers an einer Tabelle.
type Access []Grant

// FullAccess hat die Rolle „Administratoren“.
var FullAccess = Access{{Perms: AllPerms}}

// Perms fasst die Rechte zusammen, ohne Rücksicht auf Filter (für Knöpfe und Sichtbarkeit).
func (a Access) Perms() Perms {
	var p Perms
	for _, g := range a {
		p = p.or(g.Perms)
	}
	return p
}

// Filtered meldet, ob der Benutzer nicht alle Zeilen lesen darf.
func (a Access) Filtered() bool {
	for _, g := range a {
		if g.Perms.Read && g.Conds == nil {
			return false
		}
	}
	return true
}

// Limit schränkt alle Rechte auf die Freigabe eines Widgets ein. Beim Widget schließen nur Ändern
// und Löschen Lesen ein: Ein reines Erfassungs-Widget (nur Anlegen) liest nichts.
func (a Access) Limit(p Perms) Access {
	out := Access{}
	for _, g := range a {
		q := Perms{g.Perms.Read && (p.Read || p.Update || p.Delete), g.Perms.Create && p.Create, g.Perms.Update && p.Update, g.Perms.Delete && p.Delete}
		if q.Any() {
			out = append(out, Grant{Perms: q, Conds: g.Conds})
		}
	}
	return out
}

// Aktionen für Scope.
const (
	ActRead   = "read"
	ActCreate = "create"
	ActUpdate = "update"
	ActDelete = "delete"
)

func (p Perms) allows(action string) bool {
	switch action {
	case ActRead:
		return p.Read
	case ActCreate:
		return p.Create
	case ActUpdate:
		return p.Update
	case ActDelete:
		return p.Delete
	}
	return false
}

// Vars sind die Werte für die Platzhalter.
type Vars struct {
	UserID   int64
	Username string
	Today    time.Time
}

// Scope ist der Teil einer WHERE-Bedingung, der die erlaubten Zeilen beschreibt.
type Scope struct {
	all  bool
	sql  string
	args []any
}

// AllRows erlaubt alle Zeilen (für interne Zugriffe, z. B. Einstellungen eines Widgets).
var AllRows = Scope{all: true}

// All meldet, ob der Scope keine Zeile ausschließt.
func (s Scope) All() bool { return s.all }

// where liefert die Bedingung für eine WHERE-Klausel.
func (s Scope) where() (string, []any) {
	if s.all {
		return "TRUE", nil
	}
	if s.sql == "" {
		return "FALSE", nil
	}
	return s.sql, s.args
}

// Scope liefert die Zeilen, für die der Benutzer action darf.
func (a Access) Scope(t *Table, action string, v Vars) Scope {
	var ors []string
	var args []any
	for _, g := range a {
		if !g.Perms.allows(action) {
			continue
		}
		if g.Conds == nil {
			return AllRows
		}
		w, wa := CondsSQL(t, g.Conds, v)
		ors = append(ors, "("+w+")")
		args = append(args, wa...)
	}
	return Scope{sql: strings.Join(ors, " OR "), args: args}
}

// CondsSQL verbindet Bedingungen mit UND. Unbrauchbare Bedingungen (z. B. Spalte gelöscht) ergeben FALSE.
func CondsSQL(t *Table, conds []Cond, v Vars) (string, []any) {
	if len(conds) == 0 {
		return "TRUE", nil
	}
	parts, args := make([]string, 0, len(conds)), []any{}
	for _, c := range conds {
		s, a, err := condSQL(t, c, v)
		if err != nil {
			s, a = "FALSE", nil
		}
		parts = append(parts, s)
		args = append(args, a...)
	}
	return strings.Join(parts, " AND "), args
}

func columnByID(t *Table, id int64) *Column {
	for _, c := range t.Columns {
		if c.ID == id {
			return c
		}
	}
	return nil
}

var errBadCond = errors.New("ungültige Bedingung")

// condSQL übersetzt eine Bedingung. Werte gehen immer als Parameter, nie in den SQL-Text.
func condSQL(t *Table, c Cond, v Vars) (string, []any, error) {
	op := opByCode(c.Op)
	if op == nil {
		return "", nil, errBadCond
	}
	if c.Col == ColCreatedBy {
		switch {
		case c.Op == "empty":
			return "created_by IS NULL", nil, nil
		case c.Op == "notempty":
			return "created_by IS NOT NULL", nil, nil
		case c.Op != "eq" && c.Op != "ne":
			return "", nil, errBadCond
		}
		val := strings.TrimSpace(c.Val)
		var expr string
		var args []any
		if val == PlaceholderMe {
			expr, args = "?", []any{v.UserID}
		} else if val != "" {
			expr, args = "(SELECT id FROM users WHERE username = ?)", []any{val}
		} else {
			return "", nil, errBadCond
		}
		if c.Op == "eq" {
			return "created_by = " + expr, args, nil
		}
		return "(created_by IS NULL OR created_by <> " + expr + ")", args, nil
	}
	col := columnByID(t, c.Col)
	if col == nil {
		return "", nil, errBadCond
	}
	ident := columnIdent(col.ID)
	text := col.Type == "text" || col.Type == "longtext"
	switch c.Op {
	case "empty":
		if text {
			return "(" + ident + " IS NULL OR " + ident + " = '')", nil, nil
		}
		return ident + " IS NULL", nil, nil
	case "notempty":
		if text {
			return "(" + ident + " IS NOT NULL AND " + ident + " <> '')", nil, nil
		}
		return ident + " IS NOT NULL", nil, nil
	}
	val, err := condValue(col, c.Val, v)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(c.Val) == PlaceholderToday && col.Type == "datetime" {
		ident = "DATE(" + ident + ")"
	}
	switch c.Op {
	case "eq":
		return ident + " = ?", []any{val}, nil
	case "ne":
		return "(" + ident + " IS NULL OR " + ident + " <> ?)", []any{val}, nil
	case "lt", "le", "gt", "ge":
		if col.Type == "bool" {
			return "", nil, errBadCond
		}
		sym := map[string]string{"lt": "<", "le": "<=", "gt": ">", "ge": ">="}[c.Op]
		return ident + " " + sym + " ?", []any{val}, nil
	case "contains":
		if !text {
			return "", nil, errBadCond
		}
		s, _ := val.(string)
		return ident + " LIKE ?", []any{"%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s) + "%"}, nil
	}
	return "", nil, errBadCond
}

// condValue wandelt den Wert einer Bedingung für die Spalte um und setzt Platzhalter ein.
func condValue(col *Column, raw string, v Vars) (any, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case PlaceholderMe:
		if col.Type != "text" && col.Type != "longtext" {
			return nil, InputError("{ich} passt nur zu Text-Spalten (Login-Name)")
		}
		return v.Username, nil
	case PlaceholderToday:
		if col.Type != "date" && col.Type != "datetime" {
			return nil, InputError("{heute} passt nur zu Datums-Spalten")
		}
		return v.Today.Format("2006-01-02"), nil
	}
	if col.Type == "bool" {
		switch strings.ToLower(raw) {
		case "ja", "true", "on", "1":
			return true, nil
		case "nein", "false", "off", "0":
			return false, nil
		}
		return nil, InputError("bei Ja/Nein bitte „ja“ oder „nein“")
	}
	val, err := ParseValue(col, raw)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, InputError("Wert fehlt (für leere Felder „ist leer“ wählen)")
	}
	return val, nil
}

// CheckCond prüft eine Bedingung beim Speichern bzw. aus einer Widget-Anfrage.
func CheckCond(t *Table, c Cond) error {
	if opByCode(c.Op) == nil {
		return InputError("Unbekannter Vergleich.")
	}
	if c.Col != ColCreatedBy && columnByID(t, c.Col) == nil {
		return InputError("Unbekannte Spalte.")
	}
	_, _, err := condSQL(t, c, Vars{UserID: 1, Username: "x", Today: time.Now()})
	var ie InputError
	if errors.As(err, &ie) {
		return ie
	}
	if err != nil {
		if c.Col == ColCreatedBy {
			return InputError("„Angelegt von“ kennt nur „ist gleich“, „ist nicht“, „ist leer“ und „ist nicht leer“, Wert {ich} oder ein Login-Name.")
		}
		return InputError("Dieser Vergleich passt nicht zum Spaltentyp.")
	}
	return nil
}

// DescribeConds beschreibt Bedingungen für die Anzeige, z. B. „Person ist gleich {ich}“.
func DescribeConds(t *Table, conds []Cond) string {
	parts := make([]string, 0, len(conds))
	for _, c := range conds {
		name := "Angelegt von"
		if c.Col != ColCreatedBy {
			if col := columnByID(t, c.Col); col != nil {
				name = col.Name
			} else {
				name = "(gelöschte Spalte)"
			}
		}
		op := c.Op
		if o := opByCode(c.Op); o != nil {
			op = o.Label
			if o.NoValue {
				parts = append(parts, name+" "+op)
				continue
			}
		}
		parts = append(parts, name+" "+op+" „"+c.Val+"“")
	}
	return strings.Join(parts, " und ")
}

// MaxConds ist die Höchstzahl Bedingungen je Regel bzw. Widget-Abfrage.
const MaxConds = 10

// Rules liefert die Regeln einer Tabelle.
func (s *Store) Rules(ctx context.Context, tableID int64) ([]*Rule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, table_id, role_id, can_read, can_create, can_update, can_delete, conditions
		FROM table_row_rules WHERE table_id = ? ORDER BY id`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

func scanRule(row interface{ Scan(...any) error }) (*Rule, error) {
	var r Rule
	var conds string
	if err := row.Scan(&r.ID, &r.TableID, &r.RoleID, &r.Perms.Read, &r.Perms.Create, &r.Perms.Update, &r.Perms.Delete, &conds); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(conds), &r.Conds); err != nil {
		return nil, fmt.Errorf("Regel %d: %w", r.ID, err)
	}
	if r.Conds == nil {
		r.Conds = []Cond{} // nie „alle Zeilen“ durch kaputte Daten
	}
	return &r, nil
}

// AddRule speichert eine neue Regel. Die Bedingungen müssen geprüft sein (CheckCond).
func (s *Store) AddRule(ctx context.Context, t *Table, roleID int64, p Perms, conds []Cond) (int64, error) {
	if !p.Any() {
		return 0, InputError("Bitte mindestens ein Recht wählen.")
	}
	if len(conds) == 0 {
		return 0, InputError("Bitte mindestens eine Bedingung angeben. Für alle Zeilen die Rechte-Tabelle oben nutzen.")
	}
	if len(conds) > MaxConds {
		return 0, InputError(fmt.Sprintf("Höchstens %d Bedingungen.", MaxConds))
	}
	for _, c := range conds {
		if err := CheckCond(t, c); err != nil {
			return 0, err
		}
	}
	b, _ := json.Marshal(conds)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO table_row_rules (table_id, role_id, can_read, can_create, can_update, can_delete, conditions)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, t.ID, roleID, p.Read, p.Create, p.Update, p.Delete, string(b))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteRule entfernt eine Regel der Tabelle.
func (s *Store) DeleteRule(ctx context.Context, tableID, ruleID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM table_row_rules WHERE id = ? AND table_id = ?`, ruleID, tableID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserAccess liefert die wirksamen Rechte der Rollen eines Benutzers je Tabelle:
// Rechte ohne Filter aus table_permissions und Regeln mit Filter aus table_row_rules.
func (s *Store) UserAccess(ctx context.Context, roleIDs []int64) (map[int64]Access, error) {
	m := map[int64]Access{}
	if len(roleIDs) == 0 {
		return m, nil
	}
	in, args := "(?"+strings.Repeat(",?", len(roleIDs)-1)+")", make([]any, len(roleIDs))
	for i, id := range roleIDs {
		args[i] = id
	}
	all, err := s.userPerms(ctx, in, args)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		m[id] = append(m[id], Grant{Perms: withRead(all[id])})
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, table_id, role_id, can_read, can_create, can_update, can_delete, conditions
		FROM table_row_rules WHERE role_id IN `+in+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		m[r.TableID] = append(m[r.TableID], Grant{Perms: withRead(r.Perms), Conds: r.Conds})
	}
	return m, rows.Err()
}

func withRead(p Perms) Perms {
	p.Read = p.Any()
	return p
}

// ErrOutOfScope: Der Datensatz läge außerhalb der erlaubten Zeilen.
var ErrOutOfScope = InputError("Das erlauben deine Zeilenfilter nicht: Der Datensatz muss zu den Bedingungen deiner Rechte passen.")
