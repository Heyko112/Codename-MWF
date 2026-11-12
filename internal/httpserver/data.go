package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Heyko112/codename-mwf/internal/tables"
)

const recordsPerPage = 50

// tableAccess liefert die Rechte des angemeldeten Benutzers je Tabelle.
// Administratoren dürfen alles, alle anderen erben additiv über ihre wirksamen Rollen.
func (s *Server) tableAccess(r *http.Request) (map[int64]tables.Access, bool, error) {
	g, acc := CurrentAccess(r.Context())
	if acc.IsAdmin(g) {
		return nil, true, nil
	}
	roleIDs := make([]int64, 0, len(acc.Roles))
	for id := range acc.Roles {
		roleIDs = append(roleIDs, id)
	}
	m, err := s.tables.UserAccess(r.Context(), roleIDs)
	return m, false, err
}

func accessFor(m map[int64]tables.Access, admin bool, tableID int64) tables.Access {
	if admin {
		return tables.FullAccess
	}
	return m[tableID]
}

// rowVars sind die Werte für {ich} und {heute}.
func rowVars(r *http.Request) tables.Vars {
	u := CurrentUser(r.Context())
	return tables.Vars{UserID: u.ID, Username: u.Username, Today: time.Now()}
}

// dataTable ist eine Tabelle mit den Rechten des Benutzers.
type dataTable struct {
	*tables.Table
	acc  tables.Access
	vars tables.Vars
}

func (d dataTable) scope(action string) tables.Scope { return d.acc.Scope(d.Table, action, d.vars) }

// loadDataTable liest die Tabelle aus dem Pfad und prüft, dass der Benutzer sie überhaupt sehen darf.
// Ohne Recht antwortet sie mit 404, damit niemand fremde Tabellen erraten kann.
func (s *Server) loadDataTable(w http.ResponseWriter, r *http.Request) (dataTable, tables.Perms, bool) {
	m, admin, err := s.tableAccess(r)
	if err != nil {
		s.serverError(w, r, err)
		return dataTable{}, tables.Perms{}, false
	}
	id, ok := pathID(r, "id")
	acc := accessFor(m, admin, id)
	p := acc.Perms()
	if !ok || !p.Any() {
		s.notFound(w, r)
		return dataTable{}, tables.Perms{}, false
	}
	t, ok := s.loadTable(w, r)
	return dataTable{Table: t, acc: acc, vars: rowVars(r)}, p, ok
}

func dataURL(id int64) string { return "/daten/" + strconv.FormatInt(id, 10) }

func recordURL(t, rec int64) string { return dataURL(t) + "/" + strconv.FormatInt(rec, 10) }

func (s *Server) dataTables(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	access, admin, err := s.tableAccess(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	all, err := s.tables.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var list []*tables.Table
	for _, t := range all {
		if accessFor(access, admin, t.ID).Perms().Any() {
			list = append(list, t)
		}
	}
	s.render(w, r, http.StatusOK, "data_tables", map[string]any{
		"User":   CurrentUser(ctx),
		"Tables": list,
	})
}

type cell struct {
	Text  string
	Title string
}

type recordRow struct {
	ID    int64
	Cells []cell
}

func (s *Server) dataRecords(w http.ResponseWriter, r *http.Request) {
	t, p, ok := s.loadDataTable(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := r.URL.Query().Get("q")
	page, _ := strconv.Atoi(r.URL.Query().Get("seite"))
	if page < 1 {
		page = 1
	}
	recs, total, err := s.tables.Records(ctx, t.Table, tables.Query{
		Scope: t.scope(tables.ActRead), Search: q, Offset: (page - 1) * recordsPerPage, Limit: recordsPerPage})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	rows := make([]recordRow, len(recs))
	for i, rec := range recs {
		row := recordRow{ID: rec.ID}
		for _, c := range t.Columns {
			text := tables.Display(c, rec.Values[c.ID])
			cl := cell{Text: text}
			if utf8.RuneCountInString(text) > 60 {
				cl.Text = string([]rune(text)[:60]) + " …"
				cl.Title = text
			}
			row.Cells = append(row.Cells, cl)
		}
		rows[i] = row
	}
	pages := int((total + recordsPerPage - 1) / recordsPerPage)
	link := func(n int) string {
		v := url.Values{"seite": {strconv.Itoa(n)}}
		if q != "" {
			v.Set("q", q)
		}
		return dataURL(t.ID) + "?" + v.Encode()
	}
	data := map[string]any{
		"User":     CurrentUser(ctx),
		"Table":    t.Table,
		"Perms":    p,
		"Filtered": t.acc.Filtered(),
		"Rows":     rows,
		"Total":    total,
		"Query":    q,
		"Page":     page,
		"Pages":    pages,
		"Flash":    s.sessions.PopString(ctx, sessFlash),
	}
	if page > 1 {
		data["Prev"] = link(page - 1)
	}
	if page < pages {
		data["Next"] = link(page + 1)
	}
	s.render(w, r, http.StatusOK, "data_records", data)
}

type field struct {
	Col     *tables.Column
	Value   string
	Checked bool
	Error   string
	Hint    string
}

// renderRecord zeigt das Formular für einen neuen oder bestehenden Datensatz.
func (s *Server) renderRecord(w http.ResponseWriter, r *http.Request, status int, t dataTable, p tables.Perms, rec *tables.Record, fields []field, problem string) {
	ctx := r.Context()
	editable := (rec == nil && p.Create) || (rec != nil && p.Update)
	data := map[string]any{
		"User":     CurrentUser(ctx),
		"Table":    t.Table,
		"Perms":    p,
		"Record":   rec,
		"Fields":   fields,
		"Editable": editable,
		"Error":    problem,
		"Flash":    s.sessions.PopString(ctx, sessFlash),
	}
	if rec != nil {
		names := map[int64]string{}
		for _, id := range []sql.NullInt64{rec.CreatedBy, rec.UpdatedBy} {
			if id.Valid {
				if u, err := s.users.ByID(ctx, id.Int64); err == nil {
					names[id.Int64] = u.Username
				}
			}
		}
		data["Names"] = names
	}
	s.render(w, r, status, "data_record", data)
}

func recordFields(t *tables.Table, rec *tables.Record) []field {
	fields := make([]field, len(t.Columns))
	for i, c := range t.Columns {
		f := field{Col: c}
		if rec != nil {
			f.Value = tables.Input(c, rec.Values[c.ID])
			f.Checked = tables.Checked(rec.Values[c.ID])
		}
		fields[i] = f
	}
	return fields
}

// parseRecord liest und prüft alle Felder. Bei Fehlern enthalten die Felder die Eingaben und Meldungen.
func parseRecord(r *http.Request, t *tables.Table) (map[int64]any, []field, bool) {
	values := map[int64]any{}
	fields := make([]field, len(t.Columns))
	ok := true
	for i, c := range t.Columns {
		in := r.PostFormValue("c" + strconv.FormatInt(c.ID, 10))
		f := field{Col: c, Value: in, Checked: in == "on"}
		v, err := tables.ParseValue(c, in)
		var ie tables.InputError
		switch {
		case errors.As(err, &ie):
			f.Error = ie.Error()
		case err != nil:
			f.Error = "ungültiger Wert"
		case c.Required && tables.IsEmpty(c, v):
			f.Error = "Pflichtfeld"
		}
		if f.Error != "" {
			ok = false
		}
		values[c.ID] = v
		fields[i] = f
	}
	return values, fields, ok
}

func (s *Server) dataNewForm(w http.ResponseWriter, r *http.Request) {
	t, p, ok := s.loadDataTable(w, r)
	if !ok {
		return
	}
	if !p.Create {
		s.forbidden(w, r, "Du darfst in „"+t.Name+"“ keine Datensätze anlegen.")
		return
	}
	s.renderRecord(w, r, http.StatusOK, t, p, nil, recordFields(t.Table, nil), "")
}

func (s *Server) dataCreate(w http.ResponseWriter, r *http.Request) {
	t, p, ok := s.loadDataTable(w, r)
	if !ok {
		return
	}
	if !p.Create {
		s.forbidden(w, r, "Du darfst in „"+t.Name+"“ keine Datensätze anlegen.")
		return
	}
	values, fields, valid := parseRecord(r, t.Table)
	if !valid {
		s.renderRecord(w, r, http.StatusUnprocessableEntity, t, p, nil, fields, "Bitte die markierten Felder prüfen.")
		return
	}
	id, err := s.tables.InsertRecord(r.Context(), t.Table, values, CurrentUser(r.Context()).ID, t.scope(tables.ActCreate))
	if errors.Is(err, tables.ErrOutOfScope) {
		s.renderRecord(w, r, http.StatusUnprocessableEntity, t, p, nil, fields, err.Error()+" "+t.allowedText(tables.ActCreate))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	to := recordURL(t.ID, id)
	if ok, _ := s.tables.InScope(r.Context(), t.Table, id, t.scope(tables.ActUpdate)); !p.Update || !ok {
		to = dataURL(t.ID)
	}
	s.flashRedirect(w, r, "Datensatz "+strconv.FormatInt(id, 10)+" angelegt.", to)
}

// loadRecord liest Tabelle und Datensatz aus dem Pfad.
// Ändern und Löschen sind in p nur gesetzt, wenn der Datensatz dafür in den Zeilenfiltern liegt.
func (s *Server) loadRecord(w http.ResponseWriter, r *http.Request) (dataTable, tables.Perms, *tables.Record, bool) {
	t, p, ok := s.loadDataTable(w, r)
	if !ok {
		return t, p, nil, false
	}
	id, ok := pathID(r, "rec")
	if !ok {
		s.notFound(w, r)
		return t, p, nil, false
	}
	rec, err := s.tables.RecordByID(r.Context(), t.Table, id, t.scope(tables.ActRead))
	if errors.Is(err, tables.ErrNotFound) {
		s.notFound(w, r)
		return t, p, nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return t, p, nil, false
	}
	for _, x := range []struct {
		on     *bool
		action string
	}{{&p.Update, tables.ActUpdate}, {&p.Delete, tables.ActDelete}} {
		if *x.on {
			if *x.on, err = s.tables.InScope(r.Context(), t.Table, rec.ID, t.scope(x.action)); err != nil {
				s.serverError(w, r, err)
				return t, p, nil, false
			}
		}
	}
	return t, p, rec, true
}

func (s *Server) dataRecord(w http.ResponseWriter, r *http.Request) {
	t, p, rec, ok := s.loadRecord(w, r)
	if !ok {
		return
	}
	s.renderRecord(w, r, http.StatusOK, t, p, rec, recordFields(t.Table, rec), "")
}

func (s *Server) dataUpdate(w http.ResponseWriter, r *http.Request) {
	t, p, rec, ok := s.loadRecord(w, r)
	if !ok {
		return
	}
	if !p.Update {
		s.forbidden(w, r, "Du darfst in „"+t.Name+"“ keine Datensätze ändern.")
		return
	}
	values, fields, valid := parseRecord(r, t.Table)
	if !valid {
		s.renderRecord(w, r, http.StatusUnprocessableEntity, t, p, rec, fields, "Bitte die markierten Felder prüfen.")
		return
	}
	err := s.tables.UpdateRecord(r.Context(), t.Table, rec.ID, values, CurrentUser(r.Context()).ID, t.scope(tables.ActUpdate))
	if errors.Is(err, tables.ErrOutOfScope) {
		s.renderRecord(w, r, http.StatusUnprocessableEntity, t, p, rec, fields, err.Error()+" "+t.allowedText(tables.ActUpdate))
		return
	}
	if errors.Is(err, tables.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.flashRedirect(w, r, "Datensatz "+strconv.FormatInt(rec.ID, 10)+" gespeichert.", recordURL(t.ID, rec.ID))
}

func (s *Server) dataDelete(w http.ResponseWriter, r *http.Request) {
	t, p, rec, ok := s.loadRecord(w, r)
	if !ok {
		return
	}
	if !p.Delete {
		s.forbidden(w, r, "Du darfst in „"+t.Name+"“ keine Datensätze löschen.")
		return
	}
	if r.PostFormValue("confirm") != "on" {
		s.flashRedirect(w, r, "Zum Löschen bitte das Häkchen „wirklich löschen“ setzen.", recordURL(t.ID, rec.ID))
		return
	}
	if err := s.tables.DeleteRecord(r.Context(), t.Table, rec.ID, t.scope(tables.ActDelete)); err != nil && !errors.Is(err, tables.ErrNotFound) {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "record_deleted", t.Name+" Datensatz "+strconv.FormatInt(rec.ID, 10)+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Datensatz "+strconv.FormatInt(rec.ID, 10)+" gelöscht.", dataURL(t.ID))
}

// allowedText beschreibt die Zeilenfilter für eine Aktion, z. B. „Erlaubt: Person ist gleich „{ich}““.
func (d dataTable) allowedText(action string) string {
	var parts []string
	for _, g := range d.acc {
		if g.Conds != nil && map[string]bool{tables.ActCreate: g.Perms.Create, tables.ActUpdate: g.Perms.Update, tables.ActDelete: g.Perms.Delete, tables.ActRead: g.Perms.Read}[action] {
			parts = append(parts, tables.DescribeConds(d.Table, g.Conds))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Erlaubt: " + strings.Join(parts, " oder ") + "."
}
