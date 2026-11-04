-- +goose Up
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin','viewer')),
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    id          TEXT    PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL,
    user_agent  TEXT,
    ip          TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE cameras (
    id                   INTEGER PRIMARY KEY,
    slug                 TEXT    NOT NULL UNIQUE,
    name                 TEXT    NOT NULL,
    main_stream          TEXT    NOT NULL,
    sub_stream           TEXT,
    source_managed       INTEGER NOT NULL DEFAULT 1,
    record_enabled       INTEGER NOT NULL DEFAULT 1,
    record_stream        TEXT    NOT NULL DEFAULT 'main' CHECK (record_stream IN ('main','sub')),
    retention_days       INTEGER NOT NULL DEFAULT 14,
    event_retention_days INTEGER NOT NULL DEFAULT 30,
    snapshot_enabled     INTEGER NOT NULL DEFAULT 1,
    timelapse_enabled    INTEGER NOT NULL DEFAULT 1,
    detect_enabled       INTEGER NOT NULL DEFAULT 0,
    sort_order           INTEGER NOT NULL DEFAULT 0,
    created_at           INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL
);

CREATE TABLE camera_sources (
    camera_id   INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    stream      TEXT    NOT NULL CHECK (stream IN ('main','sub')),
    url         TEXT    NOT NULL,
    PRIMARY KEY (camera_id, stream)
);

CREATE TABLE recordings (
    id          INTEGER PRIMARY KEY,
    camera_id   INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_at    INTEGER NOT NULL,
    end_at      INTEGER NOT NULL,
    path        TEXT    NOT NULL UNIQUE,
    size_bytes  INTEGER NOT NULL,
    codec       TEXT,
    width       INTEGER,
    height      INTEGER,
    protected   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_recordings_camera_time ON recordings(camera_id, start_at);

CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  INTEGER NOT NULL
);

-- +goose Down
DROP TABLE settings;
DROP TABLE recordings;
DROP TABLE camera_sources;
DROP TABLE cameras;
DROP TABLE sessions;
DROP TABLE users;
