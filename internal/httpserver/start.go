package httpserver

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Heyko112/codename-mwf/internal/auth"
	"github.com/Heyko112/codename-mwf/internal/authz"
	"github.com/Heyko112/codename-mwf/internal/start"
	"github.com/Heyko112/codename-mwf/internal/widgets"
)

// widgetSrc ist die Adresse der Startdatei eines Widgets für den abgeschotteten Rahmen.
// ansicht (kachel oder vollbild) und design lesen Widgets aus der Adresse, mwf-widget.js setzt das Design.
func (s *Server) widgetSrc(wd *widgets.Widget, u *auth.User, view string) string {
	q := url.Values{"ansicht": {view}}
	if u.Theme != "" {
		q.Set("design", u.Theme)
	}
	return "/w/" + strconv.FormatInt(wd.ID, 10) + "/" + s.widgetToken(wd.ID, u.ID) + "/" + wd.Entry + "?" + q.Encode()
}

func widgetAPIURL(id int64) string { return "/api/widgets/" + strconv.FormatInt(id, 10) + "/call" }

// favoriteWidgets liefert die Favoriten des Benutzers, die er noch sehen darf.
func (s *Server) favoriteWidgets(r *http.Request) ([]*widgets.Widget, error) {
	ctx := r.Context()
	ids, err := s.widgets.Favorites(ctx, CurrentUser(ctx).ID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	all, err := s.widgets.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*widgets.Widget{}
	for _, wd := range all {
		byID[wd.ID] = wd
	}
	g, acc := CurrentAccess(ctx)
	var out []*widgets.Widget
	for _, id := range ids {
		if wd := byID[id]; wd != nil && canSee(g, acc, wd) {
			out = append(out, wd)
		}
	}
	return out, nil
}

// sectionVisible: Bereiche ohne Rolle sehen alle, sonst additiv über Rollen. Administratoren sehen alle.
func sectionVisible(g *authz.Graph, acc *authz.Access, sec *start.Section) bool {
	if len(sec.RoleIDs) == 0 || acc.IsAdmin(g) {
		return true
	}
	for _, id := range sec.RoleIDs {
		if _, ok := acc.Roles[id]; ok {
			return true
		}
	}
	return false
}

type tileView struct {
	TileID int64
	Widget *widgets.Widget
	Src    string
	API    string
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := CurrentUser(ctx)
	g, acc := CurrentAccess(ctx)
	all, err := s.start.Sections(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var secs []*start.Section
	for _, sec := range all {
		if sectionVisible(g, acc, sec) {
			secs = append(secs, sec)
		}
	}
	var cur *start.Section
	if id, err := strconv.ParseInt(r.URL.Query().Get("bereich"), 10, 64); err == nil {
		for _, sec := range secs {
			if sec.ID == id {
				cur = sec
			}
		}
	}
	if cur == nil && len(secs) > 0 {
		cur = secs[0]
	}
	var tiles, hiddenTiles []tileView
	if cur != nil {
		list, err := s.widgets.List(ctx)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		byID := map[int64]*widgets.Widget{}
		for _, wd := range list {
			byID[wd.ID] = wd
		}
		hidden, err := s.start.Hidden(ctx, u.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		for _, t := range cur.Tiles {
			wd := byID[t.WidgetID]
			if wd == nil || !canSee(g, acc, wd) {
				continue
			}
			v := tileView{TileID: t.ID, Widget: wd}
			if hidden[t.ID] {
				hiddenTiles = append(hiddenTiles, v)
				continue
			}
			v.Src, v.API = s.widgetSrc(wd, u, "kachel"), widgetAPIURL(wd.ID)
			tiles = append(tiles, v)
		}
	}
	s.render(w, r, http.StatusOK, "home", map[string]any{
		"User":     u,
		"Sections": secs,
		"Current":  cur,
		"Tiles":    tiles,
		"Hidden":   hiddenTiles,
		"Sandbox":  widgetSandbox,
	})
}

// safeBack liefert ein Rücksprungziel auf dieser Seite, nie eine fremde Adresse.
func safeBack(v string) string {
	if strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") && !strings.ContainsAny(v, "\\\r\n") {
		return v
	}
	return "/"
}

func (s *Server) setTileHidden(w http.ResponseWriter, r *http.Request, hidden bool) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return
	}
	t, err := s.start.TileByID(ctx, id)
	if errors.Is(err, start.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.start.SetHidden(ctx, CurrentUser(ctx).ID, t.ID, hidden); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/?bereich="+strconv.FormatInt(t.SectionID, 10), http.StatusSeeOther)
}

func (s *Server) hideTile(w http.ResponseWriter, r *http.Request)   { s.setTileHidden(w, r, true) }
func (s *Server) unhideTile(w http.ResponseWriter, r *http.Request) { s.setTileHidden(w, r, false) }

func (s *Server) toggleFavorite(w http.ResponseWriter, r *http.Request) {
	wd, ok := s.loadVisibleWidget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if err := s.widgets.SetFavorite(ctx, CurrentUser(ctx).ID, wd.ID, r.PostFormValue("on") == "1"); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, safeBack(r.PostFormValue("back")), http.StatusSeeOther)
}

func (s *Server) setTheme(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	theme := r.PostFormValue("theme")
	if !auth.Themes[theme] {
		theme = ""
	}
	if err := s.users.SetTheme(ctx, CurrentUser(ctx).ID, theme); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, safeBack(r.PostFormValue("back")), http.StatusSeeOther)
}

// --- Verwaltung der Startseite ---

const adminStartURL = "/admin/startseite"

type adminTile struct {
	ID     int64
	Widget *widgets.Widget
}

type adminSection struct {
	*start.Section
	RoleNames []string
	Selected  map[int64]bool
	Items     []adminTile
}

func (s *Server) adminStart(w http.ResponseWriter, r *http.Request) {
	s.renderAdminStart(w, r, http.StatusOK, "")
}

func (s *Server) renderAdminStart(w http.ResponseWriter, r *http.Request, status int, problem string) {
	ctx := r.Context()
	secs, err := s.start.Sections(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	list, err := s.widgets.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	roles, err := s.roles.ListRoles(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	byID := map[int64]*widgets.Widget{}
	for _, wd := range list {
		byID[wd.ID] = wd
	}
	g, _ := CurrentAccess(ctx)
	out := make([]adminSection, len(secs))
	for i, sec := range secs {
		as := adminSection{Section: sec, RoleNames: g.PathNames(sec.RoleIDs), Selected: map[int64]bool{}}
		for _, id := range sec.RoleIDs {
			as.Selected[id] = true
		}
		for _, t := range sec.Tiles {
			if wd := byID[t.WidgetID]; wd != nil {
				as.Items = append(as.Items, adminTile{ID: t.ID, Widget: wd})
			}
		}
		out[i] = as
	}
	s.render(w, r, status, "admin_start", map[string]any{
		"User":     CurrentUser(ctx),
		"Sections": out,
		"Widgets":  list,
		"Roles":    roles,
		"Flash":    s.sessions.PopString(ctx, sessFlash),
		"Error":    problem,
	})
}

// startInputError zeigt Eingabefehler auf der Verwaltungsseite, alles andere ist ein Serverfehler.
func (s *Server) startInputError(w http.ResponseWriter, r *http.Request, err error) {
	var ie start.InputError
	if errors.As(err, &ie) {
		s.renderAdminStart(w, r, http.StatusUnprocessableEntity, ie.Error())
		return
	}
	s.serverError(w, r, err)
}

func (s *Server) auditStart(r *http.Request, what string) {
	s.auditDetail(r, 0, "start_changed", what+" durch "+CurrentUser(r.Context()).Username)
}

func (s *Server) adminCreateSection(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name")
	if _, err := s.start.CreateSection(r.Context(), name); err != nil {
		s.startInputError(w, r, err)
		return
	}
	s.auditStart(r, "Bereich „"+strings.TrimSpace(name)+"“ angelegt")
	s.flashRedirect(w, r, "Bereich „"+strings.TrimSpace(name)+"“ angelegt.", adminStartURL)
}

func (s *Server) adminRenameSection(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r, "id")
	if err := s.start.RenameSection(r.Context(), id, r.PostFormValue("name")); err != nil {
		s.startInputError(w, r, err)
		return
	}
	s.flashRedirect(w, r, "Bereich umbenannt.", adminStartURL)
}

func (s *Server) adminSetSectionRoles(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r, "id")
	ids := formIDs(r, "role")
	if err := s.start.SetSectionRoles(r.Context(), id, ids); err != nil {
		s.serverError(w, r, err)
		return
	}
	g, _ := CurrentAccess(r.Context())
	names := strings.Join(g.PathNames(ids), ", ")
	if names == "" {
		names = "alle"
	}
	s.auditStart(r, "Bereich "+strconv.FormatInt(id, 10)+" sichtbar für "+names)
	s.flashRedirect(w, r, "Sichtbarkeit gespeichert.", adminStartURL)
}

func (s *Server) adminMoveSection(dir int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := pathID(r, "id")
		if err := s.start.MoveSection(r.Context(), id, dir); err != nil {
			s.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, adminStartURL, http.StatusSeeOther)
	}
}

func (s *Server) adminDeleteSection(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r, "id")
	if r.PostFormValue("confirm") != "on" {
		s.flashRedirect(w, r, "Zum Löschen bitte das Häkchen „wirklich löschen“ setzen.", adminStartURL)
		return
	}
	if err := s.start.DeleteSection(r.Context(), id); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditStart(r, "Bereich "+strconv.FormatInt(id, 10)+" gelöscht")
	s.flashRedirect(w, r, "Bereich gelöscht.", adminStartURL)
}

func (s *Server) adminAddTile(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r, "id")
	wid, err := strconv.ParseInt(r.PostFormValue("widget"), 10, 64)
	if err != nil {
		s.flashRedirect(w, r, "Bitte ein Widget auswählen.", adminStartURL)
		return
	}
	if _, err := s.widgets.ByID(r.Context(), wid); err != nil {
		s.flashRedirect(w, r, "Dieses Widget gibt es nicht mehr.", adminStartURL)
		return
	}
	if err := s.start.AddTile(r.Context(), id, wid); err != nil {
		s.startInputError(w, r, err)
		return
	}
	s.flashRedirect(w, r, "Kachel hinzugefügt.", adminStartURL)
}

func (s *Server) loadTile(w http.ResponseWriter, r *http.Request) (*start.Tile, bool) {
	id, _ := pathID(r, "tile")
	t, err := s.start.TileByID(r.Context(), id)
	if errors.Is(err, start.ErrNotFound) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, false
	}
	return t, true
}

func (s *Server) adminMoveTile(dir int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := s.loadTile(w, r)
		if !ok {
			return
		}
		if err := s.start.MoveTile(r.Context(), t, dir); err != nil {
			s.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, adminStartURL, http.StatusSeeOther)
	}
}

func (s *Server) adminRemoveTile(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTile(w, r)
	if !ok {
		return
	}
	if err := s.start.RemoveTile(r.Context(), t.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.flashRedirect(w, r, "Kachel entfernt.", adminStartURL)
}
