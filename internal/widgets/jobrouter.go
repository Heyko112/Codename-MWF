package widgets

import (
	"encoding/json"
	"html"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Umwandlung eines JobRouter-Dashboard-Widgets. Übernommen wird die Oberfläche (HTML, JS, CSS, Bilder),
// Titel und Größe kommen aus der PHP-Klasse. PHP läuft in MWF nicht: Was dort passiert, steht danach
// als offener Punkt im Manifest („import.hints“) und auf der Verwaltungsseite des Widgets.

var (
	jrClassRe   = regexp.MustCompile(`class\s+(\w+)\s+extends\s+\\?(?:[\w\\]*\\)?Widget\b`)
	jrTitleRe   = regexp.MustCompile(`function\s+getTitle\s*\([^)]*\)\s*(?::\s*\??\w+\s*)?\{\s*return\s*(['"])(.*?)['"]\s*;`)
	jrDimRe     = regexp.MustCompile(`['"](minWidth|minHeight)['"]\s*=>\s*(\d+)`)
	jrAppURLRe  = regexp.MustCompile(`['"]([^'"]*\.html?)(?:\?[^'"]*)?['"]`)
	jrHbsVarRe  = regexp.MustCompile(`\{\{\{?\s*([#/^]?)([\w.]+)\s*([\w.]*)[^}]*\}?\}\}`)
	jrPHPRefRe  = regexp.MustCompile(`[\w./-]+\.php\b`)
	jrRESTRe    = regexp.MustCompile(`api/rest/v\d`)
	jrAuthTrue  = regexp.MustCompile(`function\s+isAuthorized\s*\([^)]*\)\s*(?::\s*\w+\s*)?\{\s*return\s+true\s*;\s*\}`)
	jrDBRe      = regexp.MustCompile(`getJobDB|getDBConnection|->query\(|->exec\(|\bSELECT\b|\bINSERT\b|\bUPDATE\b|\bDELETE\b`)
	jrUserRe    = regexp.MustCompile(`getCurrentUsername|getCurrentJobFunctions|isCurrentUserInJobFunction`)
	jrSlugClean = regexp.MustCompile(`[^a-z0-9-]+`)
)

// jrSlug macht aus dem Klassennamen eine Widget-ID, z. B. „UmsatzHeute“ → „umsatz-heute“.
func jrSlug(class string) string {
	var b strings.Builder
	for i, r := range class {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	s := strings.Trim(jrSlugClean.ReplaceAllString(strings.ToLower(b.String()), "-"), "-")
	if s == "" {
		s = "widget"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// fromJobRouter erkennt ein JobRouter-Widget und liefert die Dateien eines MWF-Pakets samt manifest.json.
func fromJobRouter(files map[string][]byte) (map[string][]byte, bool) {
	var phpPath, class string
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		if strings.EqualFold(path.Ext(p), ".php") {
			if m := jrClassRe.FindSubmatch(files[p]); m != nil {
				phpPath, class = p, string(m[1])
				break
			}
		}
	}
	if phpPath == "" {
		return nil, false
	}
	php := string(files[phpPath])
	dir := path.Dir(phpPath)
	if dir == "." {
		dir = ""
	} else {
		dir += "/"
	}

	var hints []string
	out := map[string][]byte{}
	var skipped []string
	for _, p := range names {
		if !strings.HasPrefix(p, dir) {
			continue // liegt außerhalb des Widget-Ordners, z. B. README.md des ZIPs
		}
		rel := strings.TrimPrefix(p, dir)
		ext := strings.ToLower(path.Ext(rel))
		switch {
		case ext == ".php" || ext == ".hbs":
			if p != phpPath && ext == ".php" {
				skipped = append(skipped, rel)
			}
		case contentTypes[ext] == "":
			skipped = append(skipped, rel)
		default:
			out[rel] = files[p]
		}
	}

	name := class
	if m := jrTitleRe.FindStringSubmatch(php); m != nil && strings.TrimSpace(m[2]) != "" {
		name = strings.TrimSpace(m[2])
	} else {
		hints = append(hints, "Der Titel kommt nicht als fester Text aus getTitle(). Als Name gilt vorerst „"+class+"“.")
	}
	if len([]rune(name)) > 64 {
		name = string([]rune(name)[:64])
	}

	dims := map[string]int{}
	for _, m := range jrDimRe.FindAllStringSubmatch(php, -1) {
		dims[m[1]], _ = strconv.Atoi(m[2])
	}
	size := "small"
	if dims["minWidth"] >= 2 && dims["minHeight"] >= 2 {
		size = "large"
	} else if dims["minWidth"] >= 2 {
		size = "wide"
	}

	// Startseite: die App, die das iframe aus template.hbs lädt, sonst wird template.hbs selbst zur Seite
	entry := ""
	for _, m := range jrAppURLRe.FindAllStringSubmatch(php, -1) {
		u := m[1]
		if i := strings.Index(u, "/"+class+"/"); i >= 0 {
			u = u[i+len(class)+2:]
		}
		if _, ok := out[u]; ok {
			entry = u
			break
		}
	}
	if entry == "" {
		if _, ok := out["app/index.html"]; ok {
			entry = "app/index.html"
		}
	}
	if entry == "" {
		hbs := string(files[dir+"template.hbs"])
		if strings.TrimSpace(hbs) == "" {
			hints = append(hints, "Es gibt weder eine App (index.html) noch eine template.hbs. Die Startseite muss neu gebaut werden.")
			hbs = "<p>" + name + "</p>"
		}
		var vars []string
		seen := map[string]bool{}
		for _, m := range jrHbsVarRe.FindAllStringSubmatch(hbs, -1) {
			v := m[2]
			switch {
			case m[1] == "/":
				continue
			case m[1] != "" && (v == "each" || v == "if" || v == "unless" || v == "with"):
				v = m[3] // Block-Helfer: der Wert ist das Argument
			}
			if v != "" && !seen[v] && v != "else" && v != "this" && !strings.HasPrefix(v, "this.") && !strings.HasPrefix(v, "@") {
				seen[v] = true
				vars = append(vars, v)
			}
		}
		if len(vars) > 0 {
			hints = append(hints, "template.hbs nutzt Werte aus getData() (PHP): "+strings.Join(vars, ", ")+". Sie bleiben leer, bis das Widget sie über die Daten-Brücke holt.")
			hbs = jrHbsVarRe.ReplaceAllString(hbs, "")
		}
		css := ""
		if _, ok := out["style.css"]; ok {
			css = `<link rel="stylesheet" href="style.css">` + "\n"
		}
		entry = "index.html"
		if _, ok := out[entry]; ok {
			entry = "mwf-index.html"
		}
		out[entry] = []byte("<!doctype html>\n<html lang=\"de\">\n<head>\n<meta charset=\"utf-8\">\n<title>" + html.EscapeString(name) + "</title>\n" + css +
			"</head>\n<body>\n" + hbs + "\n</body>\n</html>\n")
	}

	if jrDBRe.MatchString(php) {
		hints = append(hints, "getData() liest Daten aus JobRouter (getJobDB/SQL). In MWF über eine Tabelle im Manifest („tables“ mit Bauplan) und mwf.list() lösen.")
	}
	if jrUserRe.MatchString(php) {
		hints = append(hints, "Benutzer oder JobFunctions kamen aus PHP. In MWF liefert mwf.context() den Benutzer, Rechte laufen über Rollen.")
	}
	if strings.Contains(php, "isAuthorized") && !jrAuthTrue.MatchString(php) {
		hints = append(hints, "isAuthorized() hat die Sichtbarkeit eingeschränkt. In MWF unter „Sichtbar für Rollen“ einstellen.")
	}
	if strings.Contains(php, "'administration'") || strings.Contains(php, `"administration"`) {
		hints = append(hints, "Das Widget war nur für Administratoren gedacht (Kategorie administration). In MWF über „Sichtbar für Rollen“ abbilden.")
	}
	for _, p := range skipped {
		hints = append(hints, "Nicht übernommen: „"+p+"“ (PHP oder nicht erlaubter Dateityp).")
	}

	// Aufrufe in der App, die in MWF ins Leere gehen
	var php2, rest, frame bool
	phpRefs := map[string]bool{}
	for p, b := range out {
		ext := path.Ext(p)
		if ext != ".html" && ext != ".js" && ext != ".mjs" {
			continue
		}
		for _, r := range jrPHPRefRe.FindAll(b, -1) {
			phpRefs[string(r)] = true
			php2 = true
		}
		rest = rest || jrRESTRe.Match(b)
		frame = frame || strings.Contains(string(b), "frameElement")
	}
	if php2 {
		refs := make([]string, 0, len(phpRefs))
		for r := range phpRefs {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		hints = append(hints, "Die App ruft PHP-Dateien auf ("+strings.Join(refs, ", ")+"). Diese Aufrufe auf die Daten-Brücke umstellen.")
	}
	if rest {
		hints = append(hints, "Die App nutzt die JobRouter-REST-API. Die gibt es in MWF nicht, Daten über die Daten-Brücke holen.")
	}
	if frame {
		hints = append(hints, "Die App greift über window.frameElement auf die Seite zu (Auto-Größe, Vollbild). In MWF ist das abgeschottet und unnötig: Die Kachel passt die Größe an, Vollbild hat einen eigenen Knopf.")
	}

	m := map[string]any{
		"id":          jrSlug(class),
		"name":        name,
		"version":     "1.0.0",
		"description": "Aus JobRouter übernommen (" + class + ").",
		"size":        size,
		"entry":       entry,
		"import":      ImportInfo{From: "jobrouter", Class: class, Hints: hints},
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	out["manifest.json"] = raw
	return out, true
}
