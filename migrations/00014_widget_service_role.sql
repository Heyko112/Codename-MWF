-- +goose Up
-- Service-Rolle eines Widgets (Schritt E3): Server-Skripte dürfen mit mwf.asService die Rechte
-- dieser Rolle nutzen, immer begrenzt auf die Freigaben des Widgets. Ohne Rolle gibt es kein asService.
ALTER TABLE widgets
    ADD COLUMN service_role_id BIGINT UNSIGNED NULL,
    ADD CONSTRAINT widgets_service_role_fk FOREIGN KEY (service_role_id) REFERENCES roles (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE widgets DROP FOREIGN KEY widgets_service_role_fk, DROP COLUMN service_role_id;
