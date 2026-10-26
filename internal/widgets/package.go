// Package widgets verwaltet Widget-Pakete: kleine Webapps, die abgeschottet in MWF laufen.
package widgets

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Heyko112/codename-mwf/internal/tables"
)

// Grenzen für ein Paket, damit eine ZIP-Datei den Server nicht überlastet.
const (
	MaxUpload    = 10 << 20 // ZIP-Datei
	MaxFiles     = 500
	MaxFileSize  = 5 << 20  // eine entpackte Datei
	MaxTotalSize = 20 << 20 // alle entpackten Dateien
)

// InputError ist ein Fehler im Paket, den der Benutzer selbst beheben kann.
type InputError string

func (e InputError) Error() string { return string(e) }

// Manifest ist der Inhalt von manifest.json. Weitere Felder (z. B. tables, settings) bleiben im Rohtext erhalten.
type Manifest struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Size        string         `json:"size"`
	Entry       string         `json:"entry"`
	Tables      []TableRequest `json:"tables"`
	Settings    []SettingDef   `json:"settings"`
}

// SettingDef ist eine Einstellung des Widgets. Sie wird eine Spalte in der Einstellungs-Tabelle des Widgets.
type SettingDef struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Size        int             `json:"size"`
	Required    bool            `json:"required"`
	Default     json.RawMessage `json:"default"`
	Description string          `json:"description"`
}

// Column beschreibt die Einstellung als Spalte (Größe mit Vorgabe des Typs).
func (d SettingDef) Column() *tables.Column {
	c := &tables.Column{Key: d.Key, Name: d.Name, Type: d.Type, Size: d.Size, Required: d.Required}
	if t := tables.TypeByCode(d.Type); t != nil && c.Size == 0 {
		c.Size = t.DefaultSize
	}
	return c
}

// TableRequest ist eine Tabelle, die das Widget anfordert. Das Widget kennt nur den Alias,
// welche echte Tabelle dahinter steckt, legt der Admin bei der Installation fest.
type TableRequest struct {
	Alias       string   `json:"alias"`
	Access      []string `json:"access"`
	Description string   `json:"description"`
}

// Perms wandelt die angeforderten Rechte um.
func (t TableRequest) Perms() tables.Perms {
	var p tables.Perms
	for _, a := range t.Access {
		switch a {
		case "read":
			p.Read = true
		case "create":
			p.Create = true
		case "update":
			p.Update = true
		case "delete":
			p.Delete = true
		}
	}
	return p
}

// ParseManifest liest ein gespeichertes Manifest wieder ein.
func ParseManifest(raw string) Manifest {
	var m Manifest
	json.Unmarshal([]byte(raw), &m)
	return m
}

// Package ist ein geprüftes, entpacktes Paket.
type Package struct {
	Manifest Manifest
	Raw      string // manifest.json wie geliefert
	Files    map[string]File
}

type File struct {
	ContentType string
	Data        []byte
}

// Sizes sind die erlaubten Kachelgrößen.
var Sizes = map[string]string{"small": "klein", "wide": "breit", "large": "groß"}

// contentTypes sind die erlaubten Dateitypen. Alles andere wird abgelehnt.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".map":   "application/json",
	".txt":   "text/plain; charset=utf-8",
	".md":    "text/plain; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	versionRe = regexp.MustCompile(`^\d{1,6}\.\d{1,6}\.\d{1,6}$`)
	pathRe    = regexp.MustCompile(`^[A-Za-z0-9._\-/]{1,255}$`)
	aliasRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// ignored sind Dateien, die Packprogramme von selbst hinzufügen.
func ignored(name string) bool {
	base := path.Base(name)
	return strings.HasPrefix(name, "__MACOSX/") || base == ".DS_Store" || base == "Thumbs.db" || base == "desktop.ini"
}

// Parse entpackt und prüft ein Paket. Liegen alle Dateien in einem gemeinsamen Ordner
// (so packt Windows „Senden an ZIP“), wird dieser Ordner entfernt.
func Parse(data []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, InputError("Die Datei ist keine gültige ZIP-Datei.")
	}
	var names []string
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || ignored(f.Name) {
			continue
		}
		names = append(names, f.Name)
		entries[f.Name] = f
	}
	prefix := commonFolder(names)
	if len(names) > MaxFiles {
		return nil, InputError(fmt.Sprintf("Das Paket enthält mehr als %d Dateien.", MaxFiles))
	}
	pkg := &Package{Files: map[string]File{}}
	var total int64
	for _, name := range names {
		f := entries[name]
		p := strings.TrimPrefix(name, prefix)
		if !pathRe.MatchString(p) || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return nil, InputError("Ungültiger Dateiname im Paket: „" + name + "“. Erlaubt sind Buchstaben, Ziffern, Punkt, Minus, Unterstrich und Schrägstrich.")
		}
		ct, ok := contentTypes[strings.ToLower(path.Ext(p))]
		if !ok {
			return nil, InputError("Dateityp nicht erlaubt: „" + p + "“.")
		}
		if f.UncompressedSize64 > MaxFileSize {
			return nil, InputError("Datei zu groß (über 5 MB): „" + p + "“.")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, InputError("Datei im Paket ist beschädigt: „" + p + "“.")
		}
		// Der Größe im ZIP-Verzeichnis nicht blind trauen: höchstens MaxFileSize+1 lesen
		b, err := io.ReadAll(io.LimitReader(rc, MaxFileSize+1))
		rc.Close()
		if err != nil {
			return nil, InputError("Datei im Paket ist beschädigt: „" + p + "“.")
		}
		if len(b) > MaxFileSize {
			return nil, InputError("Datei zu groß (über 5 MB): „" + p + "“.")
		}
		if total += int64(len(b)); total > MaxTotalSize {
			return nil, InputError("Das entpackte Paket ist größer als 20 MB.")
		}
		pkg.Files[p] = File{ContentType: ct, Data: b}
	}
	mf, ok := pkg.Files["manifest.json"]
	if !ok {
		return nil, InputError("Im Paket fehlt die Datei manifest.json.")
	}
	if err := json.Unmarshal(mf.Data, &pkg.Manifest); err != nil {
		return nil, InputError("manifest.json ist kein gültiges JSON: " + err.Error())
	}
	pkg.Raw = string(mf.Data)
	if err := pkg.check(); err != nil {
		return nil, err
	}
	return pkg, nil
}

func commonFolder(names []string) string {
	if len(names) == 0 {
		return ""
	}
	for _, n := range names {
		if n == "manifest.json" {
			return ""
		}
	}
	first, _, ok := strings.Cut(names[0], "/")
	if !ok {
		return ""
	}
	prefix := first + "/"
	for _, n := range names {
		if !strings.HasPrefix(n, prefix) {
			return ""
		}
	}
	return prefix
}

func (p *Package) check() error {
	m := &p.Manifest
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.TrimSpace(m.Description)
	if m.Size == "" {
		m.Size = "small"
	}
	if m.Entry == "" {
		m.Entry = "index.html"
	}
	switch {
	case !idRe.MatchString(m.ID):
		return InputError("manifest.json: „id“ muss aus Kleinbuchstaben, Ziffern und Minus bestehen (höchstens 64 Zeichen), z. B. „umsatz-heute“.")
	case m.Name == "" || utf8.RuneCountInString(m.Name) > 64:
		return InputError("manifest.json: „name“ muss 1 bis 64 Zeichen lang sein.")
	case !versionRe.MatchString(m.Version):
		return InputError("manifest.json: „version“ muss wie 1.0.0 aussehen.")
	case utf8.RuneCountInString(m.Description) > 255:
		return InputError("manifest.json: „description“ darf höchstens 255 Zeichen lang sein.")
	case Sizes[m.Size] == "":
		return InputError("manifest.json: „size“ muss small, wide oder large sein.")
	}
	if len(m.Tables) > 20 {
		return InputError("manifest.json: höchstens 20 Einträge unter „tables“.")
	}
	seen := map[string]bool{}
	for _, t := range m.Tables {
		if !aliasRe.MatchString(t.Alias) || seen[t.Alias] {
			return InputError("manifest.json: „tables“: Jeder Alias muss eindeutig sein, mit einem Kleinbuchstaben beginnen und nur a–z, 0–9 und _ enthalten (höchstens 32 Zeichen).")
		}
		seen[t.Alias] = true
		if len(t.Access) == 0 {
			return InputError("manifest.json: „tables“: Bei „" + t.Alias + "“ fehlt „access“, z. B. [\"read\"].")
		}
		for _, a := range t.Access {
			if a != "read" && a != "create" && a != "update" && a != "delete" {
				return InputError("manifest.json: „tables“: Unbekanntes Recht „" + a + "“ bei „" + t.Alias + "“. Erlaubt sind read, create, update, delete.")
			}
		}
		if utf8.RuneCountInString(t.Description) > 255 {
			return InputError("manifest.json: „tables“: Beschreibung bei „" + t.Alias + "“ ist länger als 255 Zeichen.")
		}
	}
	if err := checkSettings(m.Settings); err != nil {
		return err
	}
	entry, ok := p.Files[m.Entry]
	if !ok || !strings.HasPrefix(entry.ContentType, "text/html") {
		return InputError("manifest.json: Die Startdatei „" + m.Entry + "“ fehlt im Paket oder ist keine HTML-Datei.")
	}
	return nil
}

func checkSettings(defs []SettingDef) error {
	if len(defs) > 30 {
		return InputError("manifest.json: höchstens 30 Einträge unter „settings“.")
	}
	keys, names := map[string]bool{}, map[string]bool{}
	for _, d := range defs {
		prefix := "manifest.json: „settings“ „" + d.Key + "“: "
		if err := tables.CheckKey(d.Key); err != nil || keys[d.Key] {
			return InputError("manifest.json: „settings“: Jeder „key“ muss eindeutig sein, mit einem Kleinbuchstaben beginnen und nur a–z, 0–9 und _ enthalten.")
		}
		keys[d.Key] = true
		name, err := tables.CheckName(d.Name)
		if err != nil || names[name] {
			return InputError(prefix + "„name“ muss eindeutig und 1 bis 64 Zeichen lang sein.")
		}
		names[name] = true
		t := tables.TypeByCode(d.Type)
		if t == nil {
			return InputError(prefix + "unbekannter „type“. Erlaubt sind text, longtext, int, decimal, date, datetime, bool.")
		}
		c := d.Column()
		if _, err := tables.CheckSize(t, c.Size); err != nil {
			return InputError(prefix + err.Error())
		}
		if len(d.Default) > 0 {
			if _, err := tables.FromJSON(c, d.Default); err != nil {
				return InputError(prefix + "„default“ passt nicht zum Typ: " + err.Error())
			}
		}
		if utf8.RuneCountInString(d.Description) > 255 {
			return InputError(prefix + "„description“ ist länger als 255 Zeichen.")
		}
	}
	return nil
}
