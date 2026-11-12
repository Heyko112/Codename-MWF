package tables

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func ruleTable() *Table {
	return &Table{ID: 7, Columns: []*Column{
		{ID: 1, Key: "person", Type: "text", Size: 50},
		{ID: 2, Key: "von", Type: "date"},
		{ID: 3, Key: "tage", Type: "int"},
		{ID: 4, Key: "ok", Type: "bool"},
		{ID: 5, Key: "zeit", Type: "datetime"},
	}}
}

var testVars = Vars{UserID: 42, Username: "anna", Today: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}

func TestCondSQL(t *testing.T) {
	tb := ruleTable()
	cases := []struct {
		c    Cond
		sql  string
		args []any
	}{
		{Cond{1, "eq", "{ich}"}, "`c_1` = ?", []any{"anna"}},
		{Cond{1, "ne", "bert"}, "(`c_1` IS NULL OR `c_1` <> ?)", []any{"bert"}},
		{Cond{1, "contains", "a%b"}, "`c_1` LIKE ?", []any{`%a\%b%`}},
		{Cond{1, "empty", ""}, "(`c_1` IS NULL OR `c_1` = '')", nil},
		{Cond{2, "ge", "{heute}"}, "`c_2` >= ?", []any{"2026-11-12"}},
		{Cond{2, "lt", "31.12.2026"}, "`c_2` < ?", []any{"2026-12-31"}},
		{Cond{3, "gt", "5"}, "`c_3` > ?", []any{int64(5)}},
		{Cond{4, "eq", "ja"}, "`c_4` = ?", []any{true}},
		{Cond{5, "eq", "{heute}"}, "DATE(`c_5`) = ?", []any{"2026-11-12"}},
		{Cond{ColCreatedBy, "eq", "{ich}"}, "created_by = ?", []any{int64(42)}},
		{Cond{ColCreatedBy, "eq", "bert"}, "created_by = (SELECT id FROM users WHERE username = ?)", []any{"bert"}},
	}
	for _, c := range cases {
		sql, args, err := condSQL(tb, c.c, testVars)
		if err != nil || sql != c.sql || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%+v: %q %v %v", c.c, sql, args, err)
		}
	}
}

func TestCheckCondRejects(t *testing.T) {
	tb := ruleTable()
	for _, c := range []Cond{
		{9, "eq", "x"},            // Spalte fehlt
		{1, "xx", "x"},            // Vergleich unbekannt
		{3, "eq", "{ich}"},        // {ich} nur bei Text
		{1, "eq", "{heute}"},      // {heute} nur bei Datum
		{3, "contains", "1"},      // enthält nur bei Text
		{4, "lt", "ja"},           // kleiner bei Ja/Nein
		{4, "eq", "vielleicht"},   // Ja/Nein
		{3, "eq", ""},             // Wert fehlt
		{ColCreatedBy, "lt", "x"}, // angelegt von: nur gleich/ungleich
	} {
		if err := CheckCond(tb, c); err == nil {
			t.Errorf("%+v akzeptiert", c)
		}
	}
	if err := CheckCond(tb, Cond{1, "eq", "{ich}"}); err != nil {
		t.Error(err)
	}
}

func TestScope(t *testing.T) {
	tb := ruleTable()
	own := []Cond{{1, "eq", "{ich}"}}
	acc := Access{
		{Perms: Perms{Read: true}, Conds: []Cond{{3, "gt", "0"}}},
		{Perms: Perms{Read: true, Update: true}, Conds: own},
	}
	if s := acc.Scope(tb, ActRead, testVars); s.All() || s.sql != "(`c_3` > ?) OR (`c_1` = ?)" || !reflect.DeepEqual(s.args, []any{int64(0), "anna"}) {
		t.Fatalf("read: %+v", s)
	}
	if s := acc.Scope(tb, ActUpdate, testVars); s.sql != "(`c_1` = ?)" {
		t.Fatalf("update: %+v", s)
	}
	if w, _ := acc.Scope(tb, ActDelete, testVars).where(); w != "FALSE" {
		t.Fatalf("delete: %s", w)
	}
	// Ein Recht ohne Filter erlaubt alle Zeilen
	acc = append(acc, Grant{Perms: Perms{Read: true}})
	if !acc.Scope(tb, ActRead, testVars).All() || acc.Filtered() {
		t.Fatal("alle Zeilen erwartet")
	}
	// Gelöschte Spalte: Bedingung sperrt statt freizugeben
	if s := (Access{{Perms: Perms{Read: true}, Conds: []Cond{{99, "eq", "x"}}}}).Scope(tb, ActRead, testVars); s.sql != "(FALSE)" {
		t.Fatalf("gelöschte Spalte: %+v", s)
	}
	if !strings.Contains(DescribeConds(tb, []Cond{{99, "eq", "x"}}), "gelöschte") {
		t.Fatal("Beschreibung")
	}
}

func TestLimit(t *testing.T) {
	acc := Access{{Perms: Perms{Read: true, Create: true, Update: true}, Conds: []Cond{{1, "eq", "{ich}"}}}, {Perms: Perms{Read: true}}}
	// Widget darf nur anlegen: liest nichts
	if l := acc.Limit(Perms{Create: true}); l.Perms() != (Perms{Create: true}) {
		t.Fatalf("%+v", l.Perms())
	}
	// Widget darf ändern: liest mit, Filter bleibt
	l := acc.Limit(Perms{Update: true})
	if l.Perms() != (Perms{Read: true, Update: true}) || l[0].Conds == nil {
		t.Fatalf("%+v", l)
	}
}
