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

const jrPHP = `<?php
namespace dashboard\MyWidgets\UmsatzHeute;
use JobRouter\Api\Dashboard\v1\Widget;
class UmsatzHeute extends Widget
{
    public function getTitle() { return 'Umsatz heute'; }
    public function isAuthorized() { return $this->isCurrentUserInJobFunction('Vertrieb'); }
    public function getDimensions() { return ['minWidth' => 2, 'minHeight' => 1]; }
    public function getData()
    {
        $db = $this->getJobDB();
        return ['appUrl' => 'dashboard/MyWidgets/UmsatzHeute/app/index.html?u=' . rawurlencode($this->getCurrentUsername()) . '&v=3'];
    }
}`

func TestParseJobRouterApp(t *testing.T) {
	p, err := Parse(zipOf(t, map[string]string{
		"README.md": "x",
		"dashboard/MyWidgets/UmsatzHeute/UmsatzHeute.php": jrPHP,
		"dashboard/MyWidgets/UmsatzHeute/template.hbs":    `<iframe src="{{appUrl}}"></iframe>`,
		"dashboard/MyWidgets/UmsatzHeute/style.css":       "x",
		"dashboard/MyWidgets/UmsatzHeute/app/index.html":  `<script>var fe=window.frameElement; fetch('daten.php')</script><button onclick="x()">`,
		"dashboard/MyWidgets/UmsatzHeute/app/app.js":      "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	m := p.Manifest
	if m.ID != "umsatz-heute" || m.Name != "Umsatz heute" || m.Size != "wide" || m.Entry != "app/index.html" || m.Import == nil {
		t.Fatalf("%+v", m)
	}
	// index.html, app.js, style.css, manifest.json und das ausgelagerte Skript
	if _, ok := p.Files["app/app.js"]; !ok || len(p.Files) != 5 {
		t.Fatalf("%v", len(p.Files))
	}
	if idx := string(p.Files["app/index.html"].Data); idx != `<script src="index-inline-1.js"></script><button onclick="x()">` {
		t.Fatal(idx)
	}
	all := strings.Join(m.Import.Hints, "\n")
	for _, want := range []string{"getJobDB", "isAuthorized", "daten.php", "frameElement", "mwf.context", "onclick"} {
		if !strings.Contains(all, want) {
			t.Errorf("Hinweis zu %s fehlt:\n%s", want, all)
		}
	}
}

func TestParseJobRouterTemplate(t *testing.T) {
	php := `<?php class Hallo extends \JobRouter\Api\Dashboard\v1\Widget { public function getTitle(): string { return "Hallo <Welt>"; } public function isAuthorized() { return true; } }`
	p, err := Parse(zipOf(t, map[string]string{
		"Hallo/Hallo.php": php, "Hallo/template.hbs": `<p>Hallo {{name}}</p>{{#each items}}<i>{{this}}</i>{{/each}}`, "Hallo/style.css": "p{}",
	}))
	if err != nil {
		t.Fatal(err)
	}
	idx := string(p.Files["index.html"].Data)
	if p.Manifest.Entry != "index.html" || strings.Contains(idx, "{{") || !strings.Contains(idx, "Hallo &lt;Welt&gt;") || !strings.Contains(idx, "style.css") {
		t.Fatal(idx)
	}
	if h := strings.Join(p.Manifest.Import.Hints, "\n"); !strings.Contains(h, "name, items") || strings.Contains(h, "isAuthorized") {
		t.Fatal(h)
	}
}
