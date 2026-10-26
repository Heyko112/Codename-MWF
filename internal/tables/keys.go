package tables

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// MakeKey bildet aus einem Spaltennamen einen technischen Schlüssel, z. B. „Größe (cm)“ → „groesse_cm“.
func MakeKey(name string) string {
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "Ä", "ae", "Ö", "oe", "Ü", "ue")
	s := strings.ToLower(r.Replace(name))
	var b strings.Builder
	under := false
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			under = false
		} else if !under && b.Len() > 0 {
			b.WriteByte('_')
			under = true
		}
	}
	k := strings.TrimRight(b.String(), "_")
	if k == "" || k[0] >= '0' && k[0] <= '9' {
		k = "f_" + k
	}
	if len(k) > 48 {
		k = strings.TrimRight(k[:48], "_")
	}
	return k
}

// CheckKey prüft einen selbst gewählten Schlüssel.
func CheckKey(key string) error {
	if !keyRe.MatchString(key) || key == "id" {
		return InputError("Der Schlüssel muss mit einem Kleinbuchstaben beginnen und darf nur a–z, 0–9 und _ enthalten (nicht „id“).")
	}
	return nil
}

// reserved sind Namen der Systemspalten, die als Schlüssel nicht taugen.
var reserved = map[string]bool{"id": true, "created_at": true, "created_by": true, "updated_at": true, "updated_by": true}

// uniqueKey hängt bei Bedarf _2, _3 … an, bis der Schlüssel in der Tabelle frei ist.
func uniqueKey(base string, taken map[string]bool) string {
	if reserved[base] {
		base += "_feld"
	}
	k := base
	for i := 2; taken[k]; i++ {
		k = base + "_" + strconv.Itoa(i)
	}
	return k
}

// BackfillKeys gibt Spalten ohne Schlüssel (angelegt vor Migration 00010) einen.
func (s *Store) BackfillKeys(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, table_id, name, COALESCE(col_key, '') FROM data_columns ORDER BY table_id, position, id`)
	if err != nil {
		return err
	}
	type col struct {
		id, table int64
		name, key string
	}
	var cols []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.id, &c.table, &c.name, &c.key); err != nil {
			rows.Close()
			return err
		}
		cols = append(cols, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	taken := map[int64]map[string]bool{}
	for _, c := range cols {
		if taken[c.table] == nil {
			taken[c.table] = map[string]bool{}
		}
		if c.key != "" {
			taken[c.table][c.key] = true
		}
	}
	for _, c := range cols {
		if c.key != "" {
			continue
		}
		k := uniqueKey(MakeKey(c.name), taken[c.table])
		taken[c.table][k] = true
		if _, err := s.db.ExecContext(ctx, `UPDATE data_columns SET col_key = ? WHERE id = ?`, k, c.id); err != nil {
			return err
		}
	}
	return nil
}
