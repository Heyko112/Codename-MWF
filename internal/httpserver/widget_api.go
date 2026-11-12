package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/Heyko112/codename-mwf/internal/tables"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

// Die Daten-Brücke: Das Widget schickt per postMessage eine Anfrage an die MWF-Seite (widget-host.js),
// die sie mit der Sitzung des Benutzers hierher weiterreicht. Wirksam ist immer
// Recht des Benutzers ∩ Freigabe des Widgets.

const (
	apiMaxBody   = 1 << 20
	apiMaxLimit  = 200
	apiDefLimit  = 50
	apiTimestamp = "2006-01-02T15:04:05Z"
)

type apiError struct {
	status  int
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *apiError) Error() string { return e.Message }

func apiErr(status int, code, msg string) *apiError {
	return &apiError{status: status, Code: code, Message: msg}
}

var (
	errAPINotFound  = apiErr(http.StatusNotFound, "not_found", "Datensatz nicht gefunden.")
	errAPIBadParams = apiErr(http.StatusBadRequest, "bad_request", "Ungültige Parameter.")
)

type apiCall struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// apiTable ist eine Tabelle, wie das Widget sie über ihren Alias sieht.
type apiTable struct {
	grant widgets.Grant
	table *tables.Table // nil, wenn keine Tabelle zugeordnet ist
	acc   tables.Access // wirksam: Rechte des Benutzers ∩ Freigabe des Widgets
	perms tables.Perms  // zusammengefasst
}

func (s *Server) widgetAPI(w http.ResponseWriter, r *http.Request) {
	result, err := s.widgetCall(w, r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		var ae *apiError
		if !errors.As(err, &ae) {
			slog.Error("Widget-API", "id", RequestIDFrom(r.Context()), "fehler", err)
			ae = apiErr(http.StatusInternalServerError, "server", "Interner Fehler.")
		}
		w.WriteHeader(ae.status)
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": ae})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (s *Server) widgetCall(w http.ResponseWriter, r *http.Request) (any, error) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		return nil, apiErr(http.StatusNotFound, "not_found", "Widget nicht gefunden.")
	}
	wd, err := s.widgets.ByID(ctx, id)
	g, acc := CurrentAccess(ctx)
	if errors.Is(err, widgets.ErrNotFound) || (err == nil && !canSee(g, acc, wd)) {
		return nil, apiErr(http.StatusNotFound, "not_found", "Widget nicht gefunden.")
	}
	if err != nil {
		return nil, err
	}
	var call apiCall
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, apiMaxBody))
	if err := dec.Decode(&call); err != nil {
		return nil, apiErr(http.StatusBadRequest, "bad_request", "Anfrage ist kein gültiges JSON (höchstens 1 MB).")
	}
	if len(call.Params) == 0 || string(call.Params) == "null" {
		call.Params = json.RawMessage("{}")
	}
	return s.widgetMethod(r, wd, call)
}

// widgetMethod führt eine Methode der Daten-Brücke aus (aus dem Browser oder aus einem Server-Skript).
func (s *Server) widgetMethod(r *http.Request, wd *widgets.Widget, call apiCall) (any, error) {
	ctx := r.Context()
	g, acc := CurrentAccess(ctx)
	switch call.Method {
	case "context":
		u := CurrentUser(ctx)
		return map[string]any{
			"user":   map[string]any{"username": u.Username, "displayName": u.DisplayName, "roles": visibleRoleNames(g, acc)},
			"widget": map[string]any{"id": wd.Slug, "name": wd.Name, "version": wd.Version},
		}, nil
	case "tables":
		list, err := s.apiTables(r, wd)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, t := range list {
			out = append(out, describeAPITable(t))
		}
		return out, nil
	case "settings":
		// Die Tabelle legt Installation bzw. Serverstart an, hier wird nur gelesen
		out := map[string]any{}
		tid, err := s.widgets.SettingsTableID(ctx, wd.ID)
		if err != nil || tid == 0 {
			return out, err
		}
		t, err := s.tables.ByID(ctx, tid)
		if err != nil {
			return nil, err
		}
		rec, err := s.widgets.SettingsRecord(ctx, s.tables, t)
		if err != nil {
			return nil, err
		}
		for _, f := range settingsFields(t, rec, wd) {
			var v any
			if rec != nil {
				v = rec.Values[f.Col.ID]
			}
			out[f.Col.Key] = tables.ToJSON(f.Col, v)
		}
		return out, nil
	case "users":
		return s.apiUsers(r, wd, call.Params)
	case "roles":
		return s.apiRoles(r, wd)
	case "list", "get", "create", "update", "delete":
		return s.apiRecords(r, wd, call)
	case "run":
		return s.apiRun(r, wd, call.Params)
	}
	return nil, apiErr(http.StatusBadRequest, "bad_request", "Unbekannte Methode „"+call.Method+"“.")
}

// apiTables liefert alle angeforderten Tabellen mit den wirksamen Rechten des Benutzers.
func (s *Server) apiTables(r *http.Request, wd *widgets.Widget) ([]apiTable, error) {
	access, admin, err := s.tableAccess(r)
	if err != nil {
		return nil, err
	}
	return s.apiTablesFor(r.Context(), wd, access, admin)
}

// apiTablesFor liefert die angeforderten Tabellen mit den gegebenen Rechten ∩ Freigabe des Widgets.
func (s *Server) apiTablesFor(ctx context.Context, wd *widgets.Widget, access map[int64]tables.Access, admin bool) ([]apiTable, error) {
	grants, err := s.widgets.Grants(ctx, wd)
	if err != nil {
		return nil, err
	}
	out := make([]apiTable, len(grants))
	for i, gr := range grants {
		at := apiTable{grant: gr}
		if gr.TableID.Valid {
			t, err := s.tables.ByID(ctx, gr.TableID.Int64)
			if err != nil && !errors.Is(err, tables.ErrNotFound) {
				return nil, err
			}
			if t != nil {
				at.table = t
				at.acc = accessFor(access, admin, t.ID).Limit(gr.Granted)
				at.perms = at.acc.Perms()
			}
		}
		out[i] = at
	}
	return out, nil
}

func describeAPITable(t apiTable) map[string]any {
	d := map[string]any{
		"alias":       t.grant.Alias,
		"description": t.grant.Description,
		"mapped":      t.table != nil,
		"perms":       map[string]bool{"read": t.perms.Read, "create": t.perms.Create, "update": t.perms.Update, "delete": t.perms.Delete},
		"filtered":    t.table != nil && t.perms.Read && t.acc.Filtered(),
	}
	if t.table != nil && t.perms.Any() {
		cols := []any{}
		for _, c := range t.table.Columns {
			cols = append(cols, map[string]any{"key": c.Key, "name": c.Name, "type": c.Type, "size": c.Size, "required": c.Required})
		}
		d["columns"] = cols
	}
	return d
}

func recordJSON(t *tables.Table, rec *tables.Record) map[string]any {
	vals := map[string]any{}
	for _, c := range t.Columns {
		vals[c.Key] = tables.ToJSON(c, rec.Values[c.ID])
	}
	out := map[string]any{"id": rec.ID, "createdAt": rec.CreatedAt.UTC().Format(apiTimestamp), "updatedAt": nil, "values": vals}
	if rec.UpdatedAt.Valid {
		out["updatedAt"] = rec.UpdatedAt.Time.UTC().Format(apiTimestamp)
	}
	return out
}

type recordParams struct {
	Table  string                     `json:"table"`
	ID     int64                      `json:"id"`
	Values map[string]json.RawMessage `json:"values"`
	Limit  int                        `json:"limit"`
	Offset int                        `json:"offset"`
	Search string                     `json:"search"`
	Where  []apiCond                  `json:"where"`
	Order  string                     `json:"order"`
}

// apiCond ist ein Such-Filter aus dem Widget. Er schränkt nur weiter ein, die Zeilenfilter gelten immer.
type apiCond struct {
	Col   string          `json:"col"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

const apiKeyCreatedBy = "createdBy"

func apiColumn(t *tables.Table, key string) (int64, bool) {
	if key == apiKeyCreatedBy {
		return tables.ColCreatedBy, true
	}
	for _, c := range t.Columns {
		if c.Key == key {
			return c.ID, true
		}
	}
	return 0, false
}

// apiWhere wandelt die Filter des Widgets in Bedingungen um. Werte wie bei create, dazu {ich} und {heute}.
func apiWhere(t *tables.Table, in []apiCond) ([]tables.Cond, error) {
	if len(in) > tables.MaxConds {
		return nil, apiErr(http.StatusBadRequest, "bad_request", "where: höchstens "+strconv.Itoa(tables.MaxConds)+" Bedingungen.")
	}
	out := make([]tables.Cond, 0, len(in))
	for _, w := range in {
		id, ok := apiColumn(t, w.Col)
		if !ok {
			return nil, apiErr(http.StatusBadRequest, "bad_request", "where: Unbekannte Spalte „"+w.Col+"“.")
		}
		val := ""
		if len(w.Value) > 0 && string(w.Value) != "null" {
			var v any
			if err := json.Unmarshal(w.Value, &v); err != nil {
				return nil, errAPIBadParams
			}
			switch x := v.(type) {
			case string:
				val = x
			case bool:
				val = "nein"
				if x {
					val = "ja"
				}
			case float64:
				val = strconv.FormatFloat(x, 'f', -1, 64)
			default:
				return nil, apiErr(http.StatusBadRequest, "bad_request", "where: Wert bei „"+w.Col+"“ muss Text, Zahl oder Ja/Nein sein.")
			}
		}
		c := tables.Cond{Col: id, Op: w.Op, Val: val}
		if err := tables.CheckCond(t, c); err != nil {
			return nil, apiErr(http.StatusBadRequest, "bad_request", "where „"+w.Col+"“: "+err.Error())
		}
		out = append(out, c)
	}
	return out, nil
}

// apiOrder liest z. B. "datum" oder "-datum" (absteigend). "id" sortiert nach Anlage.
func apiOrder(t *tables.Table, order string) ([]tables.Sort, error) {
	if order == "" {
		return nil, nil
	}
	desc := strings.HasPrefix(order, "-")
	key := strings.TrimPrefix(order, "-")
	if key == "id" {
		return []tables.Sort{{Col: tables.ColCreatedBy, Desc: desc}}, nil
	}
	id, ok := apiColumn(t, key)
	if !ok || id == tables.ColCreatedBy {
		return nil, apiErr(http.StatusBadRequest, "bad_request", "order: Unbekannte Spalte „"+key+"“.")
	}
	return []tables.Sort{{Col: id, Desc: desc}}, nil
}

func (s *Server) apiRecords(r *http.Request, wd *widgets.Widget, call apiCall) (any, error) {
	list, err := s.apiTables(r, wd)
	if err != nil {
		return nil, err
	}
	return s.recordCall(r, wd, call, list, "")
}

// recordCall führt list/get/create/update/delete auf den Tabellen in list aus.
// via steht im Protokoll, z. B. " (Service-Rolle)".
func (s *Server) recordCall(r *http.Request, wd *widgets.Widget, call apiCall, list []apiTable, via string) (any, error) {
	ctx := r.Context()
	var p recordParams
	if err := json.Unmarshal(call.Params, &p); err != nil {
		return nil, errAPIBadParams
	}
	var at *apiTable
	for i := range list {
		if list[i].grant.Alias == p.Table {
			at = &list[i]
		}
	}
	switch {
	case at == nil:
		return nil, apiErr(http.StatusNotFound, "not_found", "Das Widget hat keine Tabelle „"+p.Table+"“ angefordert.")
	case at.table == nil:
		return nil, apiErr(http.StatusConflict, "not_mapped", "Für „"+p.Table+"“ ist noch keine Tabelle zugeordnet. Das macht ein Verwalter unter Widgets verwalten.")
	}
	t, perm := at.table, at.perms
	need := map[string]bool{"list": perm.Read, "get": perm.Read, "create": perm.Create, "update": perm.Update, "delete": perm.Delete}
	if !need[call.Method] {
		return nil, apiErr(http.StatusForbidden, "forbidden", "Keine Berechtigung für „"+call.Method+"“ in „"+p.Table+"“.")
	}
	uid := CurrentUser(ctx).ID
	vars := rowVars(r)
	scope := func(action string) tables.Scope { return at.acc.Scope(t, action, vars) }
	outOfScope := apiErr(http.StatusForbidden, "forbidden", tables.ErrOutOfScope.Error())

	switch call.Method {
	case "list":
		if p.Limit <= 0 {
			p.Limit = apiDefLimit
		}
		if p.Limit > apiMaxLimit || p.Offset < 0 {
			return nil, apiErr(http.StatusBadRequest, "bad_request", "limit höchstens "+strconv.Itoa(apiMaxLimit)+", offset nicht negativ.")
		}
		where, err := apiWhere(t, p.Where)
		if err != nil {
			return nil, err
		}
		order, err := apiOrder(t, p.Order)
		if err != nil {
			return nil, err
		}
		recs, total, err := s.tables.Records(ctx, t, tables.Query{
			Scope: scope(tables.ActRead), Search: p.Search, Where: where, Vars: vars, Order: order, Offset: p.Offset, Limit: p.Limit})
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, rec := range recs {
			out = append(out, recordJSON(t, rec))
		}
		return map[string]any{"total": total, "records": out}, nil
	case "get":
		return s.apiGet(r, t, p.ID, scope(tables.ActRead))
	case "create":
		values, err := apiValues(t, p.Values, true)
		if err != nil {
			return nil, err
		}
		id, err := s.tables.InsertRecord(ctx, t, values, uid, scope(tables.ActCreate))
		if errors.Is(err, tables.ErrOutOfScope) {
			return nil, outOfScope
		} else if err != nil {
			return nil, err
		}
		// Der eben angelegte Datensatz geht zurück, auch wenn das Widget nur anlegen darf
		return s.apiGet(r, t, id, tables.AllRows)
	case "update":
		if p.ID <= 0 {
			return nil, errAPIBadParams
		}
		values, err := apiValues(t, p.Values, false)
		if err != nil {
			return nil, err
		}
		if err := s.tables.UpdateRecord(ctx, t, p.ID, values, uid, scope(tables.ActUpdate)); errors.Is(err, tables.ErrNotFound) {
			return nil, errAPINotFound
		} else if errors.Is(err, tables.ErrOutOfScope) {
			return nil, outOfScope
		} else if err != nil {
			return nil, err
		}
		return s.apiGet(r, t, p.ID, tables.AllRows)
	default: // delete
		if p.ID <= 0 {
			return nil, errAPIBadParams
		}
		if err := s.tables.DeleteRecord(ctx, t, p.ID, scope(tables.ActDelete)); errors.Is(err, tables.ErrNotFound) {
			return nil, errAPINotFound
		} else if err != nil {
			return nil, err
		}
		s.auditDetail(r, 0, "record_deleted", t.Name+" Datensatz "+strconv.FormatInt(p.ID, 10)+" durch "+
			CurrentUser(ctx).Username+" über Widget "+wd.Slug+via)
		return map[string]any{"deleted": p.ID}, nil
	}
}

func (s *Server) apiGet(r *http.Request, t *tables.Table, id int64, scope tables.Scope) (any, error) {
	rec, err := s.tables.RecordByID(r.Context(), t, id, scope)
	if errors.Is(err, tables.ErrNotFound) {
		return nil, errAPINotFound
	}
	if err != nil {
		return nil, err
	}
	return recordJSON(t, rec), nil
}

// apiValues prüft die Werte nach Spaltenschlüssel. Beim Anlegen zählen alle Spalten (Pflichtfelder),
// beim Ändern nur die übergebenen.
func apiValues(t *tables.Table, in map[string]json.RawMessage, all bool) (map[int64]any, error) {
	out := map[int64]any{}
	fields := map[string]string{}
	known := map[string]bool{}
	for _, c := range t.Columns {
		known[c.Key] = true
		raw, given := in[c.Key]
		if !given && !all {
			continue
		}
		v, err := tables.FromJSON(c, raw)
		var ie tables.InputError
		switch {
		case errors.As(err, &ie):
			fields[c.Key] = ie.Error()
		case err != nil:
			fields[c.Key] = "ungültiger Wert"
		case c.Required && tables.IsEmpty(c, v):
			fields[c.Key] = "Pflichtfeld"
		}
		out[c.ID] = v
	}
	for k := range in {
		if !known[k] {
			fields[k] = "unbekanntes Feld"
		}
	}
	if len(fields) > 0 {
		e := apiErr(http.StatusUnprocessableEntity, "invalid", "Bitte die Eingaben prüfen.")
		e.Fields = fields
		return nil, e
	}
	return out, nil
}
