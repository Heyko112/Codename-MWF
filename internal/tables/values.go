package tables

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxLongText begrenzt Langtext, damit ein Formular die Datenbank nicht flutet.
const MaxLongText = 100000

var decimalRe = regexp.MustCompile(`^-?\d{1,18}(\.\d+)?$`)

// ParseValue wandelt eine Formulareingabe in einen Wert für die Datenbank.
// Leere Eingabe ergibt nil (außer bei Ja/Nein). Pflichtfelder prüft der Aufrufer über IsEmpty.
func ParseValue(c *Column, raw string) (any, error) {
	if c.Type != "longtext" {
		raw = strings.TrimSpace(raw)
	}
	if c.Type == "bool" {
		return raw == "on", nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	switch c.Type {
	case "text":
		if utf8.RuneCountInString(raw) > c.Size {
			return nil, InputError(fmt.Sprintf("höchstens %d Zeichen", c.Size))
		}
		return raw, nil
	case "longtext":
		if utf8.RuneCountInString(raw) > MaxLongText {
			return nil, InputError(fmt.Sprintf("höchstens %d Zeichen", MaxLongText))
		}
		return raw, nil
	case "int":
		n, err := strconv.ParseInt(strings.ReplaceAll(raw, ".", ""), 10, 64)
		if err != nil {
			return nil, InputError("bitte eine ganze Zahl eingeben")
		}
		return n, nil
	case "decimal":
		s := strings.ReplaceAll(raw, " ", "")
		if strings.Contains(s, ",") {
			// deutsche Schreibweise: Punkt = Tausender, Komma = Dezimaltrenner
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		}
		if !decimalRe.MatchString(s) {
			return nil, InputError("bitte eine Zahl eingeben, z. B. 1234,50")
		}
		if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > c.Size {
			return nil, InputError(fmt.Sprintf("höchstens %d Nachkommastellen", c.Size))
		}
		return s, nil
	case "date":
		for _, layout := range []string{"2006-01-02", "02.01.2006", "2.1.2006"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.Format("2006-01-02"), nil
			}
		}
		return nil, InputError("bitte ein Datum eingeben, z. B. 31.12.2026")
	case "datetime":
		for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "02.01.2006 15:04", "2006-01-02 15:04"} {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.Format("2006-01-02 15:04:05"), nil
			}
		}
		return nil, InputError("bitte Datum und Uhrzeit eingeben, z. B. 31.12.2026 14:30")
	}
	return nil, ErrBadType
}

// IsEmpty meldet, ob ein Pflichtfeld nicht ausgefüllt ist. Ja/Nein ist nie leer.
func IsEmpty(c *Column, v any) bool {
	return v == nil && c.Type != "bool"
}

// raw macht aus dem, was der Treiber liefert, einen String bzw. eine Zeit.
func raw(v any) (string, time.Time, bool) {
	switch x := v.(type) {
	case nil:
		return "", time.Time{}, false
	case []byte:
		return string(x), time.Time{}, true
	case string:
		return x, time.Time{}, true
	case int64:
		return strconv.FormatInt(x, 10), time.Time{}, true
	case bool:
		if x {
			return "1", time.Time{}, true
		}
		return "0", time.Time{}, true
	case time.Time:
		return "", x, true
	}
	return fmt.Sprint(v), time.Time{}, true
}

// Display formatiert einen Wert für Listen und Ansichten.
func Display(c *Column, v any) string {
	s, t, ok := raw(v)
	if !ok {
		if c.Type == "bool" {
			return "Nein"
		}
		return ""
	}
	switch c.Type {
	case "bool":
		if s == "1" {
			return "Ja"
		}
		return "Nein"
	case "decimal":
		return strings.Replace(s, ".", ",", 1)
	case "date":
		return t.Format("02.01.2006")
	case "datetime":
		return t.Format("02.01.2006 15:04")
	}
	return s
}

// Input formatiert einen Wert als Vorbelegung für ein Formularfeld.
func Input(c *Column, v any) string {
	s, t, ok := raw(v)
	if !ok {
		return ""
	}
	switch c.Type {
	case "decimal":
		return strings.Replace(s, ".", ",", 1)
	case "date":
		return t.Format("2006-01-02")
	case "datetime":
		return t.Format("2006-01-02T15:04")
	}
	return s
}

// Checked meldet für Ja/Nein, ob der Haken gesetzt ist.
func Checked(v any) bool {
	s, _, _ := raw(v)
	return s == "1"
}
