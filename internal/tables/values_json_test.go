package tables

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFromJSON(t *testing.T) {
	cases := []struct {
		typ, in string
		want    any
		bad     bool
	}{
		{"text", `"  Hallo "`, "Hallo", false},
		{"text", `5`, nil, true},
		{"int", `42`, int64(42), false},
		{"int", `4.5`, nil, true},
		{"decimal", `12.5`, "12.5", false},
		{"decimal", `"12.50"`, "12.50", false},
		{"date", `"2026-10-24"`, "2026-10-24", false},
		{"datetime", `"2026-10-24T13:05:00"`, "2026-10-24 13:05:00", false},
		{"bool", `true`, true, false},
		{"bool", `null`, false, false},
		{"bool", `"ja"`, nil, true},
		{"text", `null`, nil, false},
	}
	for _, c := range cases {
		col := &Column{Type: c.typ, Size: 10}
		got, err := FromJSON(col, json.RawMessage(c.in))
		if (err != nil) != c.bad || (!c.bad && got != c.want) {
			t.Errorf("%s %s: %v %v, erwartet %v", c.typ, c.in, got, err, c.want)
		}
	}
}

func TestToJSON(t *testing.T) {
	d := time.Date(2026, 10, 24, 13, 5, 0, 0, time.UTC)
	cases := []struct {
		typ  string
		in   any
		want any
	}{
		{"int", []byte("7"), int64(7)},
		{"bool", int64(1), true},
		{"bool", nil, false},
		{"date", d, "2026-10-24"},
		{"datetime", d, "2026-10-24T13:05:00"},
		{"decimal", []byte("3.50"), "3.50"},
		{"text", nil, nil},
	}
	for _, c := range cases {
		if got := ToJSON(&Column{Type: c.typ}, c.in); got != c.want {
			t.Errorf("%s %v: %v, erwartet %v", c.typ, c.in, got, c.want)
		}
	}
}
