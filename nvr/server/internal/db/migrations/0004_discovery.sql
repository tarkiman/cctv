-- +goose Up
-- MAC learned for a camera (from the network scan), so it is recognised after an IP change.
ALTER TABLE cameras ADD COLUMN mac TEXT;
-- Devices the admin chose to hide from scan results (e.g. a TV that speaks RTSP).
CREATE TABLE discovery_ignored (
    mac        TEXT    PRIMARY KEY,
    created_at INTEGER NOT NULL
);

-- +goose Down
DROP TABLE discovery_ignored;
ALTER TABLE cameras DROP COLUMN mac;
