# 05 — Database

SQLite dalam mode WAL, satu file di `/DATA/nvr/nvr.db`. Alasan memilih SQLite dibanding
MariaDB ada di [keputusan D3](10-keputusan.md#d3-sqlite-bukan-mariadb).

## Koneksi

```
file:/data/nvr.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)
    &_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)
```

- Satu `*sqlx.DB` untuk seluruh aplikasi. `SetMaxOpenConns(1)` untuk koneksi tulis
  (SQLite hanya punya satu penulis); pembaca boleh memakai pool terpisah bila diperlukan.
- Semua waktu disimpan sebagai **INTEGER detik Unix UTC**. Konversi ke WIB hanya di
  lapisan tampilan.
- SQL ditulis portabel sebisa mungkin (hindari fungsi khusus SQLite di query aplikasi)
  supaya pindah ke MariaDB/PostgreSQL nanti tetap mungkin.

## Migrasi

- `goose` dengan file SQL di-embed: `server/internal/db/migrations/NNNN_nama.sql`.
- Migrasi dijalankan otomatis saat `nvrd` start. Tidak ada migrasi turun (down) di
  produksi; perbaikan dilakukan dengan migrasi maju.
- Backup sebelum migrasi: salin `nvr.db` dengan `VACUUM INTO` ke
  `/DATA/nvr/backup/nvr-<versi>.db`.

## Skema awal (`0001_init.sql`)

```sql
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,              -- bcrypt
    role          TEXT    NOT NULL DEFAULT 'admin' CHECK (role IN ('admin','viewer')),
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    id          TEXT    PRIMARY KEY,             -- SHA-256 dari token acak 32 byte
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
    slug                 TEXT    NOT NULL UNIQUE,   -- 'cam1', 'bardi' (= nama stream go2rtc)
    name                 TEXT    NOT NULL,          -- nama tampilan, mis. 'Teras depan'
    main_stream          TEXT    NOT NULL,          -- nama stream go2rtc
    sub_stream           TEXT,                      -- nama stream go2rtc, boleh kosong
    source_managed       INTEGER NOT NULL DEFAULT 1,-- 0 = sumber dikelola manual di go2rtc.yaml (xiaomi://)
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

-- Rahasia sumber stream (URL RTSP berisi password). Dipisah agar tidak ikut ter-SELECT
-- di query daftar kamera dan tidak pernah dikirim ke klien.
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
    path        TEXT    NOT NULL UNIQUE,            -- relatif terhadap NVR_DATA_DIR
    size_bytes  INTEGER NOT NULL,
    codec       TEXT,                               -- 'h264' | 'hevc'
    width       INTEGER,
    height      INTEGER,
    protected   INTEGER NOT NULL DEFAULT 0          -- 1 = berisi event, retensi panjang
);
CREATE INDEX idx_recordings_camera_time ON recordings(camera_id, start_at);

CREATE TABLE settings (
    key         TEXT PRIMARY KEY,                   -- 'min_free_percent', 'notify_webhook', ...
    value       TEXT NOT NULL,
    updated_at  INTEGER NOT NULL
);
```

## Skema fase 2–3

```sql
-- 0002_exports.sql
CREATE TABLE exports (
    id          INTEGER PRIMARY KEY,
    camera_id   INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_at    INTEGER NOT NULL,
    end_at      INTEGER NOT NULL,
    status      TEXT    NOT NULL CHECK (status IN ('queued','running','done','failed')),
    path        TEXT,
    size_bytes  INTEGER,
    error       TEXT,
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL,
    finished_at INTEGER
);

-- 0003_events.sql
CREATE TABLE events (
    id            INTEGER PRIMARY KEY,
    camera_id     INTEGER NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    label         TEXT    NOT NULL,                 -- 'person', 'motion'
    score         REAL,
    start_at      INTEGER NOT NULL,
    end_at        INTEGER,                          -- NULL = masih berlangsung
    snapshot_path TEXT,
    box           TEXT,                             -- JSON [x,y,w,h] relatif 0..1
    reviewed      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_events_camera_time ON events(camera_id, start_at);
CREATE INDEX idx_events_time        ON events(start_at);
```

## Query penting

```sql
-- Timeline satu hari (dipakai halaman playback)
SELECT id, start_at, end_at, protected
FROM recordings
WHERE camera_id = ? AND end_at > ? AND start_at < ?
ORDER BY start_at;

-- Segmen yang memuat waktu tertentu
SELECT id, path, start_at
FROM recordings
WHERE camera_id = ? AND start_at <= ? AND end_at > ?
LIMIT 1;

-- Kandidat retensi berdasarkan umur
SELECT id, path FROM recordings
WHERE camera_id = ? AND protected = 0 AND end_at < ?
ORDER BY start_at
LIMIT 500;
```

## Konvensi kode store

```go
type Recording struct {
    ID        int64  `db:"id"`
    CameraID  int64  `db:"camera_id"`
    StartAt   int64  `db:"start_at"`
    EndAt     int64  `db:"end_at"`
    Path      string `db:"path"`
    SizeBytes int64  `db:"size_bytes"`
    Protected bool   `db:"protected"`
}

func (s *RecordingStore) Range(ctx context.Context, cameraID int64, from, to time.Time) ([]Recording, error) {
    var out []Recording
    err := s.db.SelectContext(ctx, &out, `
        SELECT id, camera_id, start_at, end_at, path, size_bytes, protected
        FROM recordings
        WHERE camera_id = ? AND end_at > ? AND start_at < ?
        ORDER BY start_at`, cameraID, from.Unix(), to.Unix())
    return out, err
}
```

- Satu file per tabel di `internal/store/`. Kolom disebut eksplisit, tidak pakai
  `SELECT *`.
- Operasi multi-langkah memakai `sqlx.Tx` dengan helper `store.WithTx`.
- Volume terbesar adalah `recordings`: ~144 baris/hari/kamera (segmen 10 menit), jadi
  ~13 ribu baris untuk 3 kamera × 30 hari — sangat kecil untuk SQLite.
