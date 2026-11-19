-- +goose Up
-- Person events from nvr-vision carry the zone the person was in.
ALTER TABLE events ADD COLUMN zone TEXT; -- 'inside' | 'outside' for label 'person'
-- Faces seen inside the fence (step 2 of Tahap E). Labelled with a person in step 3;
-- unlabelled ones are deleted after a week. Embeddings: 128 float32 little-endian,
-- L2-normalised (SFace).
CREATE TABLE face_samples (
    id        INTEGER PRIMARY KEY,
    camera_id INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    at        INTEGER NOT NULL, -- unix milliseconds
    zone      TEXT    NOT NULL,
    path      TEXT    NOT NULL, -- JPEG, relative to the data dir
    score     REAL    NOT NULL,
    size      INTEGER NOT NULL, -- face height in pixels (main stream)
    embedding BLOB    NOT NULL,
    person_id INTEGER
);
CREATE INDEX face_samples_camera_at ON face_samples (camera_id, at);

-- +goose Down
DROP TABLE face_samples;
ALTER TABLE events DROP COLUMN zone;
