-- +goose Up
-- Detection zones per camera, drawn on the snapshot. Points are a JSON array of [x, y]
-- pairs relative to the picture (0..1). "inside" = within the fence (people entering;
-- face recognition), "outside" = right in front of it (loitering). When a camera has
-- zones, motion outside all of them is ignored.
CREATE TABLE camera_zones (
    id         INTEGER PRIMARY KEY,
    camera_id  INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL CHECK (kind IN ('inside', 'outside')),
    name       TEXT    NOT NULL,
    points     TEXT    NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX camera_zones_camera ON camera_zones (camera_id, sort_order);

-- +goose Down
DROP TABLE camera_zones;
