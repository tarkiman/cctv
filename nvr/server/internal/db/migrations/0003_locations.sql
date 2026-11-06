-- +goose Up
-- Locations group cameras (one location per camera) and are the unit of user access.
CREATE TABLE locations (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
INSERT INTO locations (id, name, sort_order, created_at) VALUES (1, 'Rumah', 0, strftime('%s', 'now'));

-- SQLite only allows a NULL default on an added REFERENCES column; the app always sets it.
ALTER TABLE cameras ADD COLUMN location_id INTEGER REFERENCES locations(id);
UPDATE cameras SET location_id = 1;

-- access_all = 1: every camera, including future ones (existing users keep full access).
ALTER TABLE users ADD COLUMN access_all INTEGER NOT NULL DEFAULT 1;

-- Grants for users with access_all = 0: a whole location, a single camera, or a camera
-- excluded from a granted location.
CREATE TABLE user_access (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL CHECK (kind IN ('location', 'camera', 'deny_camera')),
    target_id   INTEGER NOT NULL,
    PRIMARY KEY (user_id, kind, target_id)
);

-- +goose Down
DROP TABLE user_access;
ALTER TABLE users DROP COLUMN access_all;
ALTER TABLE cameras DROP COLUMN location_id;
DROP TABLE locations;
