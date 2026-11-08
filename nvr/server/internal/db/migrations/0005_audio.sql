-- +goose Up
-- Record the camera's sound with its video (transcoded to AAC). On by default; a camera
-- whose audio cannot be read falls back to video-only automatically.
ALTER TABLE cameras ADD COLUMN record_audio INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE cameras DROP COLUMN record_audio;
