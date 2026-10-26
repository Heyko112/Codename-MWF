package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Heyko112/codename-mwf/internal/tables"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

// tableSetup ist der Stand einer angeforderten Tabelle auf der Verwaltungsseite des Widgets.
type tableSetup struct {
	widgets.Grant
	Table        *tables.Table       // zugeordnete Tabelle
	Missing      []widgets.ColumnDef // Spalten aus dem Bauplan, die in der Tabelle fehlen
	Mismatch     []string            // Spalten mit anderem Typ als im Bauplan
	Match        int64               // vorhandene Tabelle mit dem vorgeschlagenen Namen
	Rights       string              // angeforderte Rechte in Worten
	GrantedWords string              // Freigabe fürs Widget in Worten
}

func permWords(p tables.Perms) string {
	var out []string
	for _, x := range []struct {
		on bool
		w  string
	}{{p.Read, "Lesen"}, {p.Create, "Anlegen"}, {p.Update, "Ändern"}, {p.Delete, "Löschen"}} {
		if x.on {
			out = append(out, x.w)
		}
	}
	return strings.Join(out, ", ")
}

// compareColumns vergleicht eine Tabelle mit dem Bauplan, über den Schlüssel der Spalten.
func compareColumns(t *tables.Table, defs []widgets.ColumnDef) (missing []widgets.ColumnDef, mismatch []string) {
	byKey := map[string]*tables.Column{}
	for _, c := range t.Columns {
		byKey[c.Key] = c
	}
	for _, d := range defs {
		c := byKey[d.Key]
		if c == nil {
			missing = append(missing, d)
		} else if c.Type != d.Type {
			mismatch = append(mismatch, "Spalte „"+c.Name+"“ ("+d.Key+") ist "+c.TypeLabel()+", das Widget erwartet "+d.Column().TypeLabel()+".")
		}
	}
	return missing, mismatch
}

func (s *Server) tableSetups(ctx context.Context, grants []widgets.Grant, tbls []*tables.Table) ([]tableSetup, error) {
	out := make([]tableSetup, len(grants))
	for i, g := range grants {
		ts := tableSetup{Grant: g, Rights: permWords(g.Requested), GrantedWords: permWords(g.Granted)}
		if g.TableID.Valid {
			t, err := s.tables.ByID(ctx, g.TableID.Int64)
			if err != nil && !errors.Is(err, tables.ErrNotFound) {
				return nil, err
			}
			if t != nil {
				ts.Table = t
				ts.Missing, ts.Mismatch = compareColumns(t, g.Columns)
			}
		} else {
			for _, t := range tbls {
				if strings.EqualFold(t.Name, g.Suggested) {
					ts.Match = t.ID
				}
			}
		}
		out[i] = ts
	}
	return out, nil
}

// setupHint sagt nach dem Hochladen, ob bei den Tabellen noch etwas zu tun ist.
func (s *Server) setupHint(ctx context.Context, wd *widgets.Widget) string {
	grants, err := s.widgets.Grants(ctx, wd)
	if err != nil {
		return ""
	}
	setups, err := s.tableSetups(ctx, grants, nil)
	if err != nil {
		return ""
	}
	open, missing := 0, 0
	for _, ts := range setups {
		if ts.Table == nil {
			open++
		} else {
			missing += len(ts.Missing)
		}
	}
	var parts []string
	if open == 1 {
		parts = append(parts, "1 Tabelle ist noch nicht eingerichtet")
	} else if open > 1 {
		parts = append(parts, strconv.Itoa(open)+" Tabellen sind noch nicht eingerichtet")
	}
	if missing == 1 {
		parts = append(parts, "1 Spalte fehlt")
	} else if missing > 1 {
		parts = append(parts, strconv.Itoa(missing)+" Spalten fehlen")
	}
	if len(parts) == 0 {
		return ""
	}
	return "Unter „Daten“: " + strings.Join(parts, ", ") + "."
}

// widgetRequest sucht die angeforderte Tabelle zum Alias aus dem Pfad.
func widgetRequest(wd *widgets.Widget, alias string) *widgets.TableRequest {
	for _, t := range widgets.ParseManifest(wd.Manifest).Tables {
		if t.Alias == alias {
			return &t
		}
	}
	return nil
}

// adminCreateWidgetTable legt nach dem Bauplan eine neue Tabelle an, ordnet sie zu,
// gibt die angeforderten Rechte frei und setzt auf Wunsch die Rechte der Rollen.
func (s *Server) adminCreateWidgetTable(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	req := widgetRequest(wd, r.PathValue("alias"))
	if req == nil {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	back := adminWidgetURL(wd.ID)
	user := CurrentUser(ctx)

	// Spaltennamen vorab prüfen, damit keine halbe Tabelle entsteht
	names := make([]string, len(req.Columns))
	seen := map[string]bool{}
	for i, d := range req.Columns {
		n, err := tables.CheckName(r.PostFormValue("col_" + d.Key))
		if err != nil {
			s.errorRedirect(w, r, "Spalte „"+d.Key+"“: "+err.Error(), back)
			return
		}
		if seen[strings.ToLower(n)] {
			s.errorRedirect(w, r, "Den Spaltennamen „"+n+"“ gibt es zweimal.", back)
			return
		}
		seen[strings.ToLower(n)] = true
		names[i] = n
	}
	id, err := s.tables.Create(ctx, r.PostFormValue("name"), req.Description, user.ID)
	var ie tables.InputError
	if errors.As(err, &ie) {
		s.errorRedirect(w, r, "Tabelle nicht angelegt: "+ie.Error(), back)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	for i, d := range req.Columns {
		c := d.Column()
		if _, err := s.tables.AddColumn(ctx, id, names[i], c.Key, c.Type, c.Size, c.Required); err != nil {
			s.tables.Delete(ctx, id)
			if errors.As(err, &ie) {
				s.errorRedirect(w, r, "Tabelle nicht angelegt: Spalte „"+names[i]+"“: "+ie.Error(), back)
				return
			}
			s.serverError(w, r, err)
			return
		}
	}
	if err := s.widgets.SetGrant(ctx, wd, req.Alias, id, req.Perms()); err != nil {
		s.serverError(w, r, err)
		return
	}
	t, err := s.tables.ByID(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Rollen, die in der Tabelle selbst die angeforderten Rechte bekommen
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	chosen := map[int64]bool{}
	for _, rid := range formIDs(r, "role") {
		chosen[rid] = true
	}
	perms := map[int64]tables.Perms{}
	var roleNames []string
	for _, role := range roles {
		if chosen[role.ID] && !role.System {
			perms[role.ID] = req.Perms()
			roleNames = append(roleNames, role.Name)
		}
	}
	if len(perms) > 0 {
		if err := s.tables.SetRolePerms(ctx, id, perms); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	detail := wd.Slug + " " + req.Alias + " → " + t.Name + " (" + permLetters(req.Perms()) + ") durch " + user.Username
	if len(roleNames) > 0 {
		detail += ", Rollen: " + strings.Join(roleNames, ", ")
	}
	s.auditDetail(r, 0, "widget_table_created", detail)
	msg := "Tabelle „" + t.Name + "“ mit " + strconv.Itoa(len(req.Columns)) + " Spalten angelegt und dem Widget freigegeben."
	if len(roleNames) == 0 {
		msg += " Rechte für Rollen setzt du unter Tabellen."
	}
	s.flashRedirect(w, r, msg, back)
}

// adminAddMissingColumns ergänzt in der zugeordneten Tabelle die Spalten aus dem Bauplan, die fehlen.
// Vorhandene Spalten bleiben unberührt, gelöscht wird nie etwas.
func (s *Server) adminAddMissingColumns(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadAdminWidget(w, r)
	if !ok {
		return
	}
	req := widgetRequest(wd, r.PathValue("alias"))
	if req == nil {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	back := adminWidgetURL(wd.ID)
	grants, err := s.widgets.Grants(ctx, wd)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var t *tables.Table
	for _, g := range grants {
		if g.Alias == req.Alias && g.TableID.Valid {
			t, err = s.tables.ByID(ctx, g.TableID.Int64)
		}
	}
	if t == nil || err != nil {
		s.errorRedirect(w, r, "Für „"+req.Alias+"“ ist keine Tabelle zugeordnet.", back)
		return
	}
	missing, _ := compareColumns(t, req.Columns)
	var added []string
	for _, d := range missing {
		c := d.Column()
		name := c.Name
		_, err := s.tables.AddColumn(ctx, t.ID, name, c.Key, c.Type, c.Size, c.Required)
		if errors.Is(err, tables.ErrNameTaken) {
			// Name schon vergeben (andere Spalte), Schlüssel anhängen
			name = c.Name + " (" + c.Key + ")"
			_, err = s.tables.AddColumn(ctx, t.ID, name, c.Key, c.Type, c.Size, c.Required)
		}
		var ie tables.InputError
		if errors.As(err, &ie) {
			s.errorRedirect(w, r, "Spalte „"+c.Name+"“ nicht ergänzt: "+ie.Error(), back)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		added = append(added, name)
	}
	if len(added) == 0 {
		s.flashRedirect(w, r, "In „"+t.Name+"“ fehlt keine Spalte.", back)
		return
	}
	s.auditDetail(r, 0, "widget_columns_added", wd.Slug+" "+req.Alias+" → "+t.Name+": "+strings.Join(added, ", ")+" durch "+CurrentUser(ctx).Username)
	s.flashRedirect(w, r, "In „"+t.Name+"“ ergänzt: "+strings.Join(added, ", ")+".", back)
}
