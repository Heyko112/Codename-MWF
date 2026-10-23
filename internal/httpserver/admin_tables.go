package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Heyko112/codename-mwf/internal/tables"
)

func tableURL(id int64) string { return "/admin/tabellen/" + strconv.FormatInt(id, 10) }

// userError meldet Fehler, die der Benutzer selbst beheben kann.
func userError(err error) bool {
	var ie tables.InputError
	return errors.As(err, &ie)
}

func (s *Server) adminTables(w http.ResponseWriter, r *http.Request) {
	s.renderAdminTables(w, r, http.StatusOK, nil)
}

func (s *Server) renderAdminTables(w http.ResponseWriter, r *http.Request, status int, form map[string]any) {
	ctx := r.Context()
	list, err := s.tables.List(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, status, "admin_tables", map[string]any{
		"User":   CurrentUser(ctx),
		"Tables": list,
		"Flash":  s.sessions.PopString(ctx, sessFlash),
		"Form":   form,
	})
}

func (s *Server) adminCreateTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := r.PostFormValue("name")
	description := strings.TrimSpace(r.PostFormValue("description"))
	id, err := s.tables.Create(ctx, name, description, CurrentUser(ctx).ID)
	if err != nil && userError(err) {
		s.renderAdminTables(w, r, http.StatusUnprocessableEntity, map[string]any{
			"Name": name, "Description": description, "Error": err.Error(),
		})
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	name, _ = tables.CheckName(name)
	s.auditDetail(r, 0, "table_created", name+" (ID "+strconv.FormatInt(id, 10)+") durch "+CurrentUser(ctx).Username)
	s.flashRedirect(w, r, "Tabelle „"+name+"“ angelegt. Lege jetzt die Spalten an.", tableURL(id))
}

// loadTable liest die Tabelle aus dem Pfad oder antwortet mit 404.
func (s *Server) loadTable(w http.ResponseWriter, r *http.Request) (*tables.Table, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		s.notFound(w, r)
		return nil, false
	}
	t, err := s.tables.ByID(r.Context(), id)
	if errors.Is(err, tables.ErrNotFound) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, false
	}
	return t, true
}

// loadColumn liest Tabelle und Spalte aus dem Pfad.
func (s *Server) loadColumn(w http.ResponseWriter, r *http.Request) (*tables.Table, *tables.Column, bool) {
	t, ok := s.loadTable(w, r)
	if !ok {
		return nil, nil, false
	}
	colID, ok := pathID(r, "col")
	if !ok {
		s.notFound(w, r)
		return nil, nil, false
	}
	c, err := s.tables.Column(r.Context(), t.ID, colID)
	if errors.Is(err, tables.ErrNotFound) {
		s.notFound(w, r)
		return nil, nil, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return nil, nil, false
	}
	return t, c, true
}

func (s *Server) adminTableDetail(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTable(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	s.render(w, r, http.StatusOK, "admin_table", map[string]any{
		"User":  CurrentUser(ctx),
		"Table": t,
		"Types": tables.Types,
		"Flash": s.sessions.PopString(ctx, sessFlash),
	})
}

func (s *Server) adminUpdateTable(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTable(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	err := s.tables.Update(r.Context(), t.ID, name, strings.TrimSpace(r.PostFormValue("description")))
	if err != nil && userError(err) {
		s.flashRedirect(w, r, err.Error(), tableURL(t.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	name, _ = tables.CheckName(name)
	s.auditDetail(r, 0, "table_updated", t.Name+" → "+name+" (ID "+strconv.FormatInt(t.ID, 10)+") durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Tabelle „"+name+"“ gespeichert.", tableURL(t.ID))
}

func (s *Server) adminDeleteTable(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTable(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(r.PostFormValue("confirm")) != t.Name {
		s.flashRedirect(w, r, "Zum Löschen bitte den Tabellennamen genau so eintippen.", tableURL(t.ID))
		return
	}
	if err := s.tables.Delete(r.Context(), t.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "table_deleted", t.Name+" (ID "+strconv.FormatInt(t.ID, 10)+", "+strconv.FormatInt(t.RowCount, 10)+" Datensätze) durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Tabelle „"+t.Name+"“ mit allen Datensätzen gelöscht.", "/admin/tabellen")
}

func formSize(r *http.Request) int {
	n, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("size")))
	return n
}

func (s *Server) adminAddColumn(w http.ResponseWriter, r *http.Request) {
	t, ok := s.loadTable(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	typ := r.PostFormValue("type")
	size := formSize(r)
	if ct := tables.TypeByCode(typ); ct != nil && ct.SizeLabel != "" && strings.TrimSpace(r.PostFormValue("size")) == "" {
		size = ct.DefaultSize
	}
	_, err := s.tables.AddColumn(r.Context(), t.ID, name, typ, size, r.PostFormValue("required") == "on")
	if err != nil && userError(err) {
		s.flashRedirect(w, r, err.Error(), tableURL(t.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	name, _ = tables.CheckName(name)
	s.auditDetail(r, 0, "column_added", name+" in "+t.Name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Spalte „"+name+"“ hinzugefügt.", tableURL(t.ID))
}

func (s *Server) adminUpdateColumn(w http.ResponseWriter, r *http.Request) {
	t, c, ok := s.loadColumn(w, r)
	if !ok {
		return
	}
	name := r.PostFormValue("name")
	size := c.Size
	if r.PostFormValue("size") != "" {
		size = formSize(r)
	}
	err := s.tables.UpdateColumn(r.Context(), c, name, size, r.PostFormValue("required") == "on")
	if err != nil && userError(err) {
		s.flashRedirect(w, r, err.Error(), tableURL(t.ID))
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	name, _ = tables.CheckName(name)
	s.auditDetail(r, 0, "column_updated", c.Name+" → "+name+" in "+t.Name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Spalte „"+name+"“ gespeichert.", tableURL(t.ID))
}

func (s *Server) adminMoveColumnUp(w http.ResponseWriter, r *http.Request) {
	s.adminMoveColumn(w, r, -1)
}
func (s *Server) adminMoveColumnDown(w http.ResponseWriter, r *http.Request) {
	s.adminMoveColumn(w, r, 1)
}

func (s *Server) adminMoveColumn(w http.ResponseWriter, r *http.Request, dir int) {
	t, c, ok := s.loadColumn(w, r)
	if !ok {
		return
	}
	if err := s.tables.MoveColumn(r.Context(), c, dir); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, tableURL(t.ID), http.StatusSeeOther)
}

func (s *Server) adminDeleteColumn(w http.ResponseWriter, r *http.Request) {
	t, c, ok := s.loadColumn(w, r)
	if !ok {
		return
	}
	if r.PostFormValue("confirm") != "on" {
		s.flashRedirect(w, r, "Zum Löschen bitte das Häkchen „wirklich löschen“ setzen.", tableURL(t.ID))
		return
	}
	if err := s.tables.DeleteColumn(r.Context(), c); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.auditDetail(r, 0, "column_deleted", c.Name+" aus "+t.Name+" durch "+CurrentUser(r.Context()).Username)
	s.flashRedirect(w, r, "Spalte „"+c.Name+"“ mit allen Werten gelöscht.", tableURL(t.ID))
}
