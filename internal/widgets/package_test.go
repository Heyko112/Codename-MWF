package widgets

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

const okManifest = `{"id":"hallo","name":"Hallo","version":"1.0.0"}`

func TestParseOK(t *testing.T) {
	// Windows packt den Ordner mit: hallo/manifest.json
	p, err := Parse(zipOf(t, map[string]string{
		"hallo/manifest.json": okManifest, "hallo/index.html": "<p>hi", "hallo/js/app.js": "1", "__MACOSX/x": "", "hallo/Thumbs.db": "",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Manifest.Size != "small" || p.Manifest.Entry != "index.html" || len(p.Files) != 3 {
		t.Fatalf("%+v %d", p.Manifest, len(p.Files))
	}
	if p.Files["js/app.js"].ContentType != "text/javascript; charset=utf-8" {
		t.Fatal(p.Files["js/app.js"].ContentType)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]map[string]string{
		"fehlt die Datei manifest.json": {"index.html": "x"},
		"Dateityp nicht erlaubt":        {"manifest.json": okManifest, "index.html": "x", "run.exe": "x"},
		"Ungültiger Dateiname":          {"manifest.json": okManifest, "index.html": "x", "../evil.js": "x"},
		"„id“":                          {"manifest.json": `{"id":"Böse","name":"x","version":"1.0.0"}`, "index.html": "x"},
		"„version“":                     {"manifest.json": `{"id":"a","name":"x","version":"1"}`, "index.html": "x"},
		"Startdatei":                    {"manifest.json": okManifest},
		"kein gültiges JSON":            {"manifest.json": "{", "index.html": "x"},
	}
	for want, files := range cases {
		_, err := Parse(zipOf(t, files))
		var ie InputError
		if !errors.As(err, &ie) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	if _, err := Parse([]byte("kein zip")); err == nil {
		t.Error("kein ZIP akzeptiert")
	}
}

func TestParseSizeLimit(t *testing.T) {
	big := strings.Repeat("a", MaxFileSize+1)
	_, err := Parse(zipOf(t, map[string]string{"manifest.json": okManifest, "index.html": "x", "big.txt": big}))
	if err == nil || !strings.Contains(err.Error(), "zu groß") {
		t.Fatal(err)
	}
}

func TestParseBlueprint(t *testing.T) {
	ok := `{"id":"a","name":"A","version":"1.0.0","tables":[{"alias":"n","access":["read"],"name":"Notizen",
		"columns":[{"key":"text","name":"Text","type":"text","required":true},{"key":"erledigt","name":"Erledigt","type":"bool"}]}]}`
	p, err := Parse(zipOf(t, map[string]string{"manifest.json": ok, "index.html": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	tr := p.Manifest.Tables[0]
	if tr.SuggestedName("A") != "Notizen" || len(tr.Columns) != 2 || tr.Columns[0].Column().Size != 255 {
		t.Fatalf("%+v", tr)
	}
	if (TableRequest{Alias: "n"}).SuggestedName("A") != "A: n" {
		t.Fatal("Vorschlag ohne name")
	}
	bad := map[string]string{
		"Schlüssel": `[{"key":"Text","name":"Text","type":"text"}]`,
		"Typ":       `[{"key":"text","name":"Text","type":"xyz"}]`,
		"doppelt":   `[{"key":"a","name":"A","type":"bool"},{"key":"a","name":"B","type":"bool"}]`,
	}
	for want, cols := range bad {
		m := `{"id":"a","name":"A","version":"1.0.0","tables":[{"alias":"n","access":["read"],"columns":` + cols + `}]}`
		_, err := Parse(zipOf(t, map[string]string{"manifest.json": m, "index.html": "x"}))
		if err == nil || !strings.Contains(err.Error(), "Spalte") {
			t.Errorf("%s: %v", want, err)
		}
	}
}
