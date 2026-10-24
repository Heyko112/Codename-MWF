package tables

import (
	"testing"
	"time"
)

func TestParseValue(t *testing.T) {
	text := &Column{Type: "text", Size: 5}
	dec := &Column{Type: "decimal", Size: 2}
	cases := []struct {
		c    *Column
		in   string
		want any
		bad  bool
	}{
		{text, "  Hallo ", "Hallo", false},
		{text, "zu lang", nil, true},
		{text, "", nil, false},
		{&Column{Type: "int"}, "1.234", int64(1234), false},
		{&Column{Type: "int"}, "1,5", nil, true},
		{dec, "1.234,5", "1234.5", false},
		{dec, "-12.75", "-12.75", false},
		{dec, "1,234", nil, true},
		{dec, "abc", nil, true},
		{&Column{Type: "date"}, "31.12.2026", "2026-12-31", false},
		{&Column{Type: "date"}, "2026-02-30", nil, true},
		{&Column{Type: "datetime"}, "2026-12-31T14:30", "2026-12-31 14:30:00", false},
		{&Column{Type: "bool"}, "on", true, false},
		{&Column{Type: "bool"}, "", false, false},
	}
	for _, c := range cases {
		got, err := ParseValue(c.c, c.in)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("%s %q: %v, %v", c.c.Type, c.in, got, err)
		}
	}
}

func TestDisplay(t *testing.T) {
	d := time.Date(2026, 12, 31, 14, 30, 0, 0, time.UTC)
	if s := Display(&Column{Type: "datetime"}, d); s != "31.12.2026 14:30" {
		t.Error(s)
	}
	if s := Display(&Column{Type: "decimal"}, []byte("1234.50")); s != "1234,50" {
		t.Error(s)
	}
	if s := Display(&Column{Type: "bool"}, int64(1)); s != "Ja" {
		t.Error(s)
	}
	if s := Input(&Column{Type: "date"}, d); s != "2026-12-31" {
		t.Error(s)
	}
}
