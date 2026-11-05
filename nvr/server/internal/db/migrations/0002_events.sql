-- +goose Up
-- Activity found by the detector: 'motion' now, 'person' with the AI model (fase 3).
CREATE TABLE events (
    id            INTEGER PRIMARY KEY,
    camera_id     INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    label         TEXT    NOT NULL CHECK (label IN ('motion', 'person')),
    score         REAL,                              -- motion: peak changed area 0..1; person: model score
    start_at      INTEGER NOT NULL,
    end_at        INTEGER,                           -- NULL while ongoing
    snapshot_path TEXT,
    box           TEXT,                              -- JSON [x,y,w,h] relative 0..1
    reviewed      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_events_camera_time ON events(camera_id, start_at);
CREATE INDEX idx_events_time        ON events(start_at);

-- +goose Down
DROP TABLE events;
