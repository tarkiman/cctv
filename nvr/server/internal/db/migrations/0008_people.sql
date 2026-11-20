-- +goose Up
-- People named in the face gallery (Tahap E step 3). face_samples.person_id is the
-- admin's label (kept forever); match_id/match_score is what recognition said.
CREATE TABLE people (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    notify     INTEGER NOT NULL DEFAULT 1, -- notify when this person arrives
    created_at INTEGER NOT NULL
);
ALTER TABLE face_samples ADD COLUMN match_id INTEGER;
ALTER TABLE face_samples ADD COLUMN match_score REAL;
CREATE INDEX face_samples_person ON face_samples (person_id);

-- +goose Down
DROP INDEX face_samples_person;
ALTER TABLE face_samples DROP COLUMN match_score;
ALTER TABLE face_samples DROP COLUMN match_id;
DROP TABLE people;
