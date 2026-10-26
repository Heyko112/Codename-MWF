package widgets

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/Heyko112/codename-mwf/internal/tables"
)

// SettingsTableID liefert die Einstellungs-Tabelle eines Widgets (0, wenn es noch keine gibt).
func (s *Store) SettingsTableID(ctx context.Context, widgetID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM data_tables WHERE widget_id = ?`, widgetID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// SyncSettings sorgt dafür, dass das Widget seine Einstellungs-Tabelle mit genau einem Datensatz hat,
// und legt Spalten für neue Einstellungen aus dem Manifest an (mit ihrer Vorgabe).
// Einstellungen, die ein neues Manifest nicht mehr nennt, bleiben erhalten, damit keine Werte verloren gehen.
// Tabellen und Spalten anlegen sind DDL-Befehle, deshalb läuft das außerhalb der Installations-Transaktion.
func (s *Store) SyncSettings(ctx context.Context, ts *tables.Store, w *Widget, userID int64) (*tables.Table, error) {
	tid, err := s.SettingsTableID(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	if tid == 0 {
		name := "Einstellungen: " + w.Name
		for i := 2; ; i++ {
			tid, err = ts.Create(ctx, name, "Einstellungen des Widgets „"+w.Slug+"“", userID)
			if !errors.Is(err, tables.ErrNameTaken) || i > 20 {
				break
			}
			name = "Einstellungen: " + w.Name + " (" + strconv.Itoa(i) + ")"
		}
		if err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE data_tables SET widget_id = ? WHERE id = ?`, w.ID, tid); err != nil {
			return nil, err
		}
	}
	t, err := ts.ByID(ctx, tid)
	if err != nil {
		return nil, err
	}
	have := map[string]*tables.Column{}
	for _, c := range t.Columns {
		have[c.Key] = c
	}
	defaults := map[string]bool{}
	for _, d := range ParseManifest(w.Manifest).Settings {
		c := d.Column()
		if old := have[d.Key]; old != nil {
			// Name und Pflicht dürfen sich ändern, Text nur länger werden
			size := old.Size
			if c.Type == "text" && c.Size > size {
				size = c.Size
			}
			if old.Type == c.Type && (old.Name != c.Name || old.Required != c.Required || old.Size != size) {
				if err := ts.UpdateColumn(ctx, old, c.Name, size, c.Required); err != nil {
					var ie tables.InputError
					if !errors.As(err, &ie) {
						return nil, err
					}
				}
			}
			continue
		}
		if _, err := ts.AddColumn(ctx, t.ID, c.Name, c.Key, c.Type, c.Size, c.Required); err != nil {
			return nil, err
		}
		defaults[d.Key] = true
	}
	if t, err = ts.ByID(ctx, tid); err != nil {
		return nil, err
	}
	rec, err := s.SettingsRecord(ctx, ts, t)
	if err != nil {
		return nil, err
	}
	vals := map[int64]any{}
	for _, d := range ParseManifest(w.Manifest).Settings {
		if !defaults[d.Key] && rec != nil {
			continue
		}
		for _, c := range t.Columns {
			if c.Key == d.Key {
				v, err := tables.FromJSON(c, d.Default)
				if err != nil {
					return nil, err
				}
				vals[c.ID] = v
			}
		}
	}
	if rec == nil {
		_, err = ts.InsertRecord(ctx, t, vals, userID)
	} else if len(vals) > 0 {
		err = ts.UpdateRecord(ctx, t, rec.ID, vals, userID)
	}
	return t, err
}

// SettingsRecord liefert den Datensatz mit den Einstellungen (nil, wenn es noch keinen gibt).
func (s *Store) SettingsRecord(ctx context.Context, ts *tables.Store, t *tables.Table) (*tables.Record, error) {
	recs, _, err := ts.Records(ctx, t, "", 0, 1)
	if err != nil || len(recs) == 0 {
		return nil, err
	}
	return recs[0], nil
}

// DeleteSettings löscht die Einstellungs-Tabelle eines Widgets.
func (s *Store) DeleteSettings(ctx context.Context, ts *tables.Store, widgetID int64) error {
	tid, err := s.SettingsTableID(ctx, widgetID)
	if err != nil || tid == 0 {
		return err
	}
	return ts.Delete(ctx, tid)
}
