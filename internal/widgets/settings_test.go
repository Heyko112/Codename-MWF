package widgets

import (
	"encoding/json"
	"testing"
)

func TestCheckSettings(t *testing.T) {
	ok := []SettingDef{
		{Key: "titel", Name: "Titel", Type: "text", Size: 40, Default: json.RawMessage(`"x"`)},
		{Key: "an", Name: "An", Type: "bool", Default: json.RawMessage(`true`)},
		{Key: "anzahl", Name: "Anzahl", Type: "int"},
	}
	if err := checkSettings(ok); err != nil {
		t.Fatalf("gültige Einstellungen abgelehnt: %v", err)
	}
	bad := map[string][]SettingDef{
		"doppelter Schlüssel": {{Key: "a", Name: "A", Type: "int"}, {Key: "a", Name: "B", Type: "int"}},
		"doppelter Name":      {{Key: "a", Name: "A", Type: "int"}, {Key: "b", Name: "A", Type: "int"}},
		"Schlüssel id":        {{Key: "id", Name: "A", Type: "int"}},
		"Typ":                 {{Key: "a", Name: "A", Type: "farbe"}},
		"Vorgabe":             {{Key: "a", Name: "A", Type: "int", Default: json.RawMessage(`"zehn"`)}},
		"Länge":               {{Key: "a", Name: "A", Type: "text", Size: 5000}},
	}
	for name, defs := range bad {
		if err := checkSettings(defs); err == nil {
			t.Errorf("%s: nicht abgelehnt", name)
		}
	}
}
