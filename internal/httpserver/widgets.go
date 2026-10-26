package httpserver

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Heyko112/codename-mwf/internal/authz"
	"github.com/Heyko112/codename-mwf/internal/tables"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

// Widgets laufen in einem iframe mit sandbox ohne allow-same-origin, also mit eigener, leerer Herkunft.
// Dort gibt es keine Cookies. Deshalb steckt in der Adresse jeder Widget-Datei eine signierte,
// kurzlebige Marke: /w/<widget>/<benutzer>.<ablauf>.<signatur>/<pfad>.
const widgetTokenTTL = 8 * time.Hour

// widgetCSP: nur Dateien aus dem eigenen Paket, kein Internet, keine Formulare nach außen.
const widgetCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
	"font-src 'self'; connect-src 'self'; media-src 'self' blob:; frame-src 'none'; object-src 'none'; worker-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'self'"

// widgetSandbox sind die Freiheiten des Rahmens. Bewusst ohne allow-same-origin und allow-top-navigation.
const widgetSandbox = "allow-scripts allow-forms allow-modals allow-downloads"

func widgetMsg(widgetID, userID, exp int64) string {
	return strconv.FormatInt(widgetID, 10) + "|" + strconv.FormatInt(userID, 10) + "|" + strconv.FormatInt(exp, 10)
}

func (s *Server) widgetToken(widgetID, userID int64) string {
	exp := time.Now().Add(widgetTokenTTL).Unix()
	sig := s.secrets.MAC("widget-file", widgetMsg(widgetID, userID, exp))
	return strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(exp, 10) + "." + base64.RawURLEncoding.EncodeToString(sig[:18])
}

// checkWidgetToken liefert den Benutzer zur Marke, wenn sie echt und nicht abgelaufen ist.
func (s *Server) checkWidgetToken(widgetID int64, tok string) (int64, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return 0, false
	}
	uid, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	sig, err3 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || time.Now().Unix() > exp {
		return 0, false
	}
	want := s.secrets.MAC("widget-file", widgetMsg(widgetID, uid, exp))
	return uid, hmac.Equal(sig, want[:18])
}

// canSee meldet, ob jemand mit diesem Zugriff das Widget sehen darf.
// Administratoren sehen alle, Verwalter auch inaktive (zum Testen), sonst additiv über Rollen.
func canSee(g *authz.Graph, acc *authz.Access, w *widgets.Widget) bool {
	if acc.IsAdmin(g) || acc.Has(authz.PermWidgetsManage) {
		return true
	}
	if !w.Active {
		return false
	}
	for _, id := range w.RoleIDs {
		if _, ok := acc.Roles[id]; ok {
			return true
		}
	}
	return false
}

func widgetURL(id int64) string { return "/widgets/" + strconv.FormatInt(id, 10) }

func (s *Server) widgetList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := s.widgets.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	g, acc := CurrentAccess(ctx)
	var list []*widgets.Widget
	for _, wd := range all {
		if canSee(g, acc, wd) {
			list = append(list, wd)
		}
	}
	favs, err := s.widgets.Favorites(ctx, CurrentUser(ctx).ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	fav := map[int64]bool{}
	for _, id := range favs {
		fav[id] = true
	}
	s.render(w, r, http.StatusOK, "widgets", map[string]any{
		"User":    CurrentUser(ctx),
		"Widgets": list,
		"Fav":     fav,
	})
}

// loadVisibleWidget liest das Widget aus dem Pfad. Unsichtbares gibt 404.
func (s *Server) loadVisibleWidget(w http.ResponseWriter, r *http.Request) (*widgets.Widget, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return nil, false
	}
	wd, err := s.widgets.ByID(r.Context(), id)
	if errors.Is(err, widgets.ErrNotFound) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, false
	}
	g, acc := CurrentAccess(r.Context())
	if !canSee(g, acc, wd) {
		s.notFound(w, r)
		return nil, false
	}
	return wd, true
}

func (s *Server) widgetView(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadVisibleWidget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	u := CurrentUser(ctx)
	favs, err := s.widgets.Favorites(ctx, u.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "widget_view", map[string]any{
		"User":     u,
		"Widget":   wd,
		"Src":      s.widgetSrc(wd, u, "vollbild"),
		"API":      widgetAPIURL(wd.ID),
		"Sandbox":  widgetSandbox,
		"Favorite": slices.Contains(favs, wd.ID),
	})
}

// widgetFile liefert eine Datei aus dem Paket an den abgeschotteten Rahmen.
// Rechte werden bei jeder Datei neu geprüft: Wer die Rolle verliert, sieht das Widget sofort nicht mehr.
func (s *Server) widgetFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	uid, ok := s.checkWidgetToken(id, r.PathValue("tok"))
	if !ok {
		http.Error(w, "Zugang abgelaufen. Bitte die Seite neu laden.", http.StatusForbidden)
		return
	}
	u, err := s.users.ByID(ctx, uid)
	if err != nil || !u.IsActive || u.IsService {
		http.Error(w, "Kein Zugriff.", http.StatusForbidden)
		return
	}
	wd, err := s.widgets.ByID(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, acc, err := s.roles.Access(ctx, uid)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !canSee(g, acc, wd) {
		http.NotFound(w, r)
		return
	}
	ct, data, err := s.widgets.File(ctx, id, r.PathValue("path"))
	if errors.Is(err, widgets.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", widgetCSP)
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Content-Type", ct)
	h.Set("Cache-Control", "private, max-age=60")
	// Module und Schriften lädt der Rahmen mit leerer Herkunft per CORS. Die Marke in der Adresse schützt die Datei.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Write(data)
}

// --- Verwaltung ---

func (s *Server) adminWidgets(w http.ResponseWriter, r *http.Request) {
	s.renderAdminWidgets(w, r, http.StatusOK, "")
}

func (s *Server) renderAdminWidgets(w http.ResponseWriter, r *http.Request, status int, problem string) {
	ctx := r.Context()
	list, err := s.widgets.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	g, _ := CurrentAccess(ctx)
	type row struct {
		*widgets.Widget
		Roles []string
	}
	rows := make([]row, len(list))
	for i, wd := range list {
		rows[i] = row{Widget: wd, Roles: g.PathNames(wd.RoleIDs)}
	}
	s.render(w, r, status, "admin_widgets", map[string]any{
		"User":    CurrentUser(ctx),
		"Widgets": rows,
		"Flash":   s.sessions.PopString(ctx, sessFlash),
		"Error":   problem,
		"MaxMB":   widgets.MaxUpload >> 20,
	})
}

func (s *Server) adminUploadWidget(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, widgets.MaxUpload+1<<20)
	file, _, err := r.FormFile("package")
	if err != nil {
		s.renderAdminWidgets(w, r, http.StatusUnprocessableEntity, "Bitte eine ZIP-Datei bis "+strconv.Itoa(widgets.MaxUpload>>20)+" MB auswählen.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, widgets.MaxUpload+1))
	if err != nil || len(data) > widgets.MaxUpload {
		s.renderAdminWidgets(w, r, http.StatusUnprocessableEntity, "Die ZIP-Datei ist größer als "+strconv.Itoa(widgets.MaxUpload>>20)+" MB.")
		return
	}
	pkg, err := widgets.Parse(data)
	var ie widgets.InputError
	if errors.As(err, &ie) {
		s.renderAdminWidgets(w, r, http.StatusUnprocessableEntity, ie.Error())
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	id, old, err := s.widgets.Install(ctx, pkg, CurrentUser(ctx).ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wd, err := s.widgets.ByID(ctx, id)
	if err == nil {
		_, err = s.widgets.SyncSettings(ctx, s.tables, wd, CurrentUser(ctx).ID)
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	m := pkg.Manifest
	msg := "Widget „" + m.Name + "“ " + m.Version + " installiert. Lege jetzt fest, welche Rollen es sehen."
	event := "widget_installed"
	if old == m.Version {
		msg = "Widget „" + m.Name + "“ " + m.Version + " neu eingespielt. Sichtbarkeit bleibt wie bisher."
		event = "widget_updated"
	} else if old != "" {
		msg = "Widget „" + m.Name + "“ von " + old + " auf " + m.Version + " aktualisiert. Sichtbarkeit bleibt wie bisher."
		event = "widget_updated"
	}
	s.auditDetail(r, 0, event, m.ID+" "+m.Version+" durch "+CurrentUser(ctx).Username)
	s.flashRedirect(w, r, msg, adminWidgetURL(id))
}

func adminWidgetURL(id int64) string { return "/admin/widgets/" + strconv.FormatInt(id, 10) }

func (s *Server) loadAdminWidget(w http.ResponseWriter, r *http.Request) (*widgets.Widget, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return nil, false
	}
	wd, err := s.widgets.ByID(r.Context(), id)
	if errors.Is(err, widgets.ErrNotFound) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, false
	}
	return wd, true
}

func (s *Server) adminWidgetDetail(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	selected := map[int64]bool{}
	for _, id := range wd.RoleIDs {
		selected[id] = true
	}
	grants, err := s.widgets.Grants(ctx, wd)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	tbls, err := s.tables.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	fields, err := s.widgetSettingsFields(r, wd)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "admin_widget", map[string]any{
		"Settings": fields,
		"User":     CurrentUser(ctx),
		"Widget":   wd,
		"Roles":    roles,
		"Selected": selected,
		"Grants":   grants,
		"Tables":   tbls,
		"Flash":    s.sessions.PopString(ctx, sessFlash),
	})
}

func (s *Server) adminSetWidgetRoles(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	ids := formIDs(r, "role")
	if err := s.widgets.SetRoles(r.Context(), wd.ID, ids); err != nil {
		s.serverError(w, r, err)
		return
	}
	g, _ := CurrentAccess(r.Context())
	s.auditDetail(r, 0, "widget_roles_changed", wd.Slug+" durch "+CurrentUser(r.Context()).Username+": "+strings.Join(g.PathNames(ids), ", "))
	s.flashRedirect(w, r, "Sichtbarkeit von „"+wd.Name+"“ gespeichert.", adminWidgetURL(wd.ID))
}

// adminSetWidgetTable ordnet einem angeforderten Alias eine Tabelle zu und gibt Rechte frei.
func (s *Server) adminSetWidgetTable(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	alias := r.PostFormValue("alias")
	tableID, _ := strconv.ParseInt(r.PostFormValue("table"), 10, 64)
	tableName := "keine"
	if tableID > 0 {
		t, err := s.tables.ByID(ctx, tableID)
		if err == nil && t.WidgetID.Valid {
			err = tables.ErrNotFound
		}
		if err != nil {
			s.flashRedirect(w, r, "Diese Tabelle gibt es nicht mehr.", adminWidgetURL(wd.ID))
			return
		}
		tableName = t.Name
	}
	p := tables.Perms{
		Read:   r.PostFormValue("read") == "on",
		Create: r.PostFormValue("create") == "on",
		Update: r.PostFormValue("update") == "on",
		Delete: r.PostFormValue("delete") == "on",
	}
	err := s.widgets.SetGrant(ctx, wd, alias, tableID, p)
	if errors.Is(err, widgets.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if tableID == 0 {
		p = tables.Perms{}
	}
	s.auditDetail(r, 0, "widget_table_changed", wd.Slug+" "+alias+" → "+tableName+" ("+permLetters(p)+") durch "+CurrentUser(ctx).Username)
	s.flashRedirect(w, r, "Tabelle für „"+alias+"“ gespeichert.", adminWidgetURL(wd.ID))
}

func (s *Server) adminActivateWidget(w http.ResponseWriter, r *http.Request) {
	s.adminSetWidgetActive(w, r, true)
}
func (s *Server) adminDeactivateWidget(w http.ResponseWriter, r *http.Request) {
	s.adminSetWidgetActive(w, r, false)
}

func (s *Server) adminSetWidgetActive(w http.ResponseWriter, r *http.Request, active bool) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	if err := s.widgets.SetActive(r.Context(), wd.ID, active); err != nil {
		s.serverError(w, r, err)
		return
	}
	event, msg := "widget_deactivated", "„"+wd.Name+"“ ist inaktiv. Nur Verwalter sehen es noch."
	if active {
		event, msg = "widget_activated", "„"+wd.Name+"“ ist aktiv."
	}
	s.auditDetail(r, 0, event, wd.Slug+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, msg, adminWidgetURL(wd.ID))
}

func (s *Server) adminDeleteWidget(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	if r.PostFormValue("confirm") != "on" {
		s.flashRedirect(w, r, "Zum Entfernen bitte das Häkchen „wirklich entfernen“ setzen.", adminWidgetURL(wd.ID))
		return
	}
	if err := s.widgets.DeleteSettings(r.Context(), s.tables, wd.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.widgets.Delete(r.Context(), wd.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "widget_deleted", wd.Slug+" "+wd.Version+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Widget „"+wd.Name+"“ entfernt.", "/admin/widgets")
}

// widgetSettingsFields liefert die Eingabefelder der Einstellungen, mit der Beschreibung aus dem Manifest.
func (s *Server) widgetSettingsFields(r *http.Request, wd *widgets.Widget) ([]field, error) {
	ctx := r.Context()
	t, err := s.widgets.SyncSettings(ctx, s.tables, wd, CurrentUser(ctx).ID)
	if err != nil {
		return nil, err
	}
	rec, err := s.widgets.SettingsRecord(ctx, s.tables, t)
	if err != nil {
		return nil, err
	}
	return settingsFields(t, rec, wd), nil
}

// settingsFields zeigt nur Einstellungen, die das aktuelle Manifest nennt, in dessen Reihenfolge.
func settingsFields(t *tables.Table, rec *tables.Record, wd *widgets.Widget) []field {
	byKey := map[string]field{}
	for _, f := range recordFields(t, rec) {
		byKey[f.Col.Key] = f
	}
	var out []field
	for _, d := range widgets.ParseManifest(wd.Manifest).Settings {
		if f, ok := byKey[d.Key]; ok {
			f.Hint = d.Description
			out = append(out, f)
		}
	}
	return out
}

func (s *Server) adminSaveWidgetSettings(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	t, err := s.widgets.SyncSettings(ctx, s.tables, wd, CurrentUser(ctx).ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	rec, err := s.widgets.SettingsRecord(ctx, s.tables, t)
	if err != nil || rec == nil {
		s.serverError(w, r, fmt.Errorf("Einstellungen von %s: %v", wd.Slug, err))
		return
	}
	// Nur Spalten aus dem aktuellen Manifest, alte Werte bleiben unberührt
	shown := settingsFields(t, rec, wd)
	sub := &tables.Table{ID: t.ID, Name: t.Name}
	for _, f := range shown {
		sub.Columns = append(sub.Columns, f.Col)
	}
	values, fields, valid := parseRecord(r, sub)
	if !valid {
		var msgs []string
		for _, f := range fields {
			if f.Error != "" {
				msgs = append(msgs, f.Col.Name+": "+f.Error)
			}
		}
		s.flashRedirect(w, r, "Einstellungen nicht gespeichert. "+strings.Join(msgs, "; "), adminWidgetURL(wd.ID))
		return
	}
	if err := s.tables.UpdateRecord(ctx, t, rec.ID, values, CurrentUser(ctx).ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "widget_settings_changed", wd.Slug+" durch "+CurrentUser(ctx).Username)
	s.flashRedirect(w, r, "Einstellungen von „"+wd.Name+"“ gespeichert.", adminWidgetURL(wd.ID))
}
