package tables

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// FromJSON wandelt einen JSON-Wert (aus einem Widget) in einen Wert für die Datenbank.
// Text, Datum und Datum/Uhrzeit kommen als String, Ganzzahl als Zahl, Dezimal als String oder Zahl,
// Ja/Nein als true/false. null leert das Feld.
func FromJSON(c *Column, raw json.RawMessage) (any, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		if c.Type == "bool" {
			return false, nil
		}
		return nil, nil
	}
	switch c.Type {
	case "bool":
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return nil, InputError("bitte true oder false")
		}
		return b, nil
	case "int":
		var n json.Number
		if json.Unmarshal(raw, &n) != nil {
			return nil, InputError("bitte eine ganze Zahl")
		}
		i, err := strconv.ParseInt(n.String(), 10, 64)
		if err != nil {
			return nil, InputError("bitte eine ganze Zahl")
		}
		return i, nil
	case "decimal":
		var n json.Number
		if json.Unmarshal(raw, &n) == nil {
			raw, _ = json.Marshal(n.String())
		}
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil, InputError("bitte als Text übergeben")
	}
	return ParseValue(c, s)
}

// ToJSON formatiert einen gespeicherten Wert für ein Widget, passend zu FromJSON.
func ToJSON(c *Column, v any) any {
	s, t, ok := raw(v)
	if !ok {
		if c.Type == "bool" {
			return false
		}
		return nil
	}
	switch c.Type {
	case "bool":
		return s == "1"
	case "int":
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	case "date":
		return t.Format("2006-01-02")
	case "datetime":
		return t.Format("2006-01-02T15:04:05")
	}
	return s
}
