// Package tables verwaltet selbst definierte Tabellen (wie JobData).
// Jede Tabelle ist eine echte MariaDB-Tabelle dt_<id> mit Spalten c_<id>.
// Namen im Frontend stehen nur im Verzeichnis, Umbenennen ändert die Struktur nie.
package tables

import "fmt"

// ColType beschreibt einen Feldtyp.
type ColType struct {
	Code  string
	Label string
	// Size: was die Zahl bei diesem Typ bedeutet ("" = keine Zahl)
	SizeLabel            string
	DefaultSize, MinSize int
	MaxSize              int
}

// Types in der Reihenfolge, in der sie angezeigt werden.
var Types = []ColType{
	{Code: "text", Label: "Text", SizeLabel: "Maximale Länge", DefaultSize: 255, MinSize: 1, MaxSize: 1000},
	{Code: "longtext", Label: "Langtext"},
	{Code: "int", Label: "Ganzzahl"},
	{Code: "decimal", Label: "Dezimalzahl", SizeLabel: "Nachkommastellen", DefaultSize: 2, MinSize: 0, MaxSize: 6},
	{Code: "date", Label: "Datum"},
	{Code: "datetime", Label: "Datum und Uhrzeit"},
	{Code: "bool", Label: "Ja/Nein"},
}

// TypeByCode liefert den Feldtyp oder nil.
func TypeByCode(code string) *ColType {
	for i := range Types {
		if Types[i].Code == code {
			return &Types[i]
		}
	}
	return nil
}

// sqlType liefert den MariaDB-Typ. Spalten sind immer NULL-fähig,
// Pflichtfelder prüft die App, damit Spalten auch an gefüllte Tabellen angehängt werden können.
func sqlType(code string, size int) string {
	switch code {
	case "text":
		return fmt.Sprintf("VARCHAR(%d)", size)
	case "longtext":
		return "MEDIUMTEXT"
	case "int":
		return "BIGINT"
	case "decimal":
		return fmt.Sprintf("DECIMAL(%d,%d)", 18+size, size)
	case "date":
		return "DATE"
	case "datetime":
		return "DATETIME"
	case "bool":
		return "BOOLEAN"
	}
	panic("unbekannter Feldtyp " + code)
}

func tableIdent(id int64) string  { return fmt.Sprintf("`dt_%d`", id) }
func columnIdent(id int64) string { return fmt.Sprintf("`c_%d`", id) }
