-- +goose Up
-- Jedes Widget hat eine eigene Einstellungs-Tabelle (dt_N). Sie gehört dem Widget und taucht nicht unter Tabellen/Daten auf.
ALTER TABLE data_tables
    ADD COLUMN widget_id BIGINT UNSIGNED NULL,
    ADD UNIQUE KEY data_tables_widget_uq (widget_id),
    ADD CONSTRAINT data_tables_widget_fk FOREIGN KEY (widget_id) REFERENCES widgets (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE data_tables DROP FOREIGN KEY data_tables_widget_fk, DROP KEY data_tables_widget_uq, DROP COLUMN widget_id;
