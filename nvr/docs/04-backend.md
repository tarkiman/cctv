# 04 — Backend (Go)

## Prinsip

- **Standard library dulu.** Dependensi hanya bila jelas menghemat kerja: `chi`, `sqlx`,
  driver SQLite, `goose` (migrasi), `golang.org/x/crypto` (bcrypt).
- **Tanpa ORM.** SQL ditulis tangan di package repository, dipetakan dengan `sqlx`
  (`db:"kolom"`).
- **Tanpa CGO** di build utama (`modernc.org/sqlite` pure Go) supaya cross-compile ke
  `linux/arm64` mudah. Pengecualian: detektor ONNX di fase 3 (lihat bawah).
- **Log terstruktur** dengan `log/slog` (JSON di produksi, teks saat development).
- **Context di mana-mana.** Semua worker berhenti rapi saat `SIGTERM` (Docker stop).

## Dependensi

| Paket | Kegunaan |
|---|---|
| `github.com/go-chi/chi/v5` | Router + middleware (`RequestID`, `RealIP`, `Recoverer`, `Timeout`) |
| `github.com/jmoiron/sqlx` | Query → struct, tanpa ORM |
| `modernc.org/sqlite` | Driver SQLite pure Go |
| `github.com/pressly/goose/v3` | Migrasi SQL, file di-embed |
| `golang.org/x/crypto/bcrypt` | Hash password |
| `github.com/coder/websocket` | Proxy WebSocket live view ke go2rtc |
| `github.com/yalue/onnxruntime_go` | Fase 3, inferensi deteksi orang (CGO) |

## Struktur kode

```
server/
├── cmd/nvrd/main.go           # wiring: config, db, workers, http server, shutdown
├── internal/
│   ├── config/                # baca env NVR_*, validasi, default
│   ├── db/                    # buka SQLite (pragma), goose migrate
│   │   └── migrations/        # 0001_init.sql, ... (embed)
│   ├── store/                 # repository per tabel: cameras.go, recordings.go, ...
│   ├── go2rtc/                # klien API go2rtc: sinkron stream, status, probe
│   ├── ffmpeg/                # builder argumen + runner subprocess (stdout/stderr, kill)
│   ├── supervisor/            # jalankan & restart worker per kamera dengan backoff
│   ├── recorder/              # rekaman segmen, remux ts->mp4, indexing
│   ├── snapshot/              # keyframe per menit, latest.jpg, frame timelapse
│   ├── timelapse/             # kompilasi harian (scheduler internal, bukan cron)
│   ├── retention/             # hapus berdasarkan umur & watermark disk
│   ├── health/                # status kamera & sistem, broadcast ke SSE
│   ├── detect/                # fase 3: motion gate + detektor (interface)
│   ├── events/                # fase 3: event, proteksi segmen, notifikasi
│   ├── notify/                # fase 2: webhook (WhatsApp gowa, Discord)
│   ├── auth/                  # login, sesi, middleware, rate limit
│   ├── sse/                   # hub Server-Sent Events
│   └── api/                   # handler chi per resource + router.go
└── web/                       # embed.FS hasil build Vue (dist/)
```

Aturan dependensi antar-package: `api` → (`store`, worker) → (`ffmpeg`, `go2rtc`, `db`).
Worker tidak mengimpor `api`. Komunikasi worker → UI lewat `health`/`sse`.

## Konfigurasi (environment)

Semua konfigurasi infrastruktur lewat env. Konfigurasi per kamera dan retensi disimpan di
database dan diubah lewat UI.

| Variabel | Default | Keterangan |
|---|---|---|
| `NVR_LISTEN` | `:8090` | Alamat HTTP |
| `NVR_DATA_DIR` | `/data` | Root penyimpanan (di-mount dari `/DATA/nvr`) |
| `NVR_GO2RTC_URL` | `http://127.0.0.1:1984` | API go2rtc |
| `NVR_GO2RTC_RTSP` | `rtsp://127.0.0.1:8556` | Restream RTSP go2rtc |
| `NVR_FFMPEG` | `ffmpeg` | Path binary ffmpeg |
| `NVR_TZ` | `Asia/Jakarta` | Zona waktu tampilan & nama folder |
| `NVR_ADMIN_USER` / `NVR_ADMIN_PASSWORD` | — | Hanya dipakai sekali untuk membuat admin pertama bila tabel `users` kosong |
| `NVR_SECURE_COOKIE` | `false` | `true` bila diakses lewat HTTPS (reverse proxy) |
| `NVR_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `NVR_SEGMENT_SECONDS` | `600` | Panjang segmen rekaman |

## Worker

### Supervisor
Setiap worker per kamera (`recorder`, `detector`) dijalankan oleh supervisor yang:
- me-restart dengan backoff eksponensial 1 s → 60 s, reset setelah 5 menit stabil;
- menyimpan 50 baris terakhir stderr ffmpeg untuk ditampilkan di halaman status;
- melaporkan state `starting | running | degraded | offline | disabled` ke `health`;
- menghentikan dan menyalakan ulang worker saat konfigurasi kamera berubah (tanpa restart
  `nvrd`).

### Recorder
- Argumen ffmpeg: lihat [02 — Arsitektur](02-arsitektur.md#rekaman).
- `-segment_list pipe:1 -segment_list_type csv` memberi notifikasi tiap segmen selesai
  beserta waktu mulai/akhir relatif; waktu absolut dihitung dari nama file
  (`-strftime 1`, UTC).
- Remux `.ts` → `.mp4` dijalankan berurutan (satu antrean global, bukan paralel) agar
  lonjakan I/O kecil.
- Saat start, recorder memindai folder untuk `.ts` yatim (sisa crash) dan me-remux /
  mengindeksnya.
- Stall detection: bila tidak ada segmen baru dalam `2 × NVR_SEGMENT_SECONDS`, proses
  dibunuh dan direstart.

### Snapshot
- Ticker 60 detik per kamera, dengan jitter agar kamera tidak diambil bersamaan.
- Timeout 15 detik per pengambilan (menunggu keyframe; GOP kamera ini 1–4 detik).
- Hasil: `snapshots/<kamera>/latest.jpg` (atomic rename) dan, bila timelapse aktif,
  salinan ke `timelapse/<kamera>/frames/<tanggal>/`.

### Timelapse
- Scheduler internal pukul 00:15 WIB menggabung frame hari sebelumnya:
  `nice -n 19 ffmpeg -framerate 24 -pattern_type glob -i '*.jpg' -c:v libx264 -preset veryfast -crf 28`.
- Satu kamera dalam satu waktu. Frame mentah dihapus setelah N hari (setting).

### Retention
Dijalankan tiap 15 menit:
1. Hapus segmen non-protected yang lebih tua dari `retention_days` kamera itu.
2. Hapus segmen protected yang lebih tua dari `event_retention_days`.
3. Bila sisa disk < `min_free_percent` (default 10%), hapus segmen non-protected tertua
   lintas kamera sampai sisa disk aman. Catat peringatan dan kirim notifikasi.
4. Hapus folder tanggal yang sudah kosong.
File dihapus dulu, baru baris database (baris yatim lebih aman daripada file yatim yang
tidak terlihat).

### Health
- Polling go2rtc `GET /api/streams` tiap 10 detik (status producer & error).
- Pola error `c301` yang sudah dikenal dipetakan ke pesan yang bisa ditindaklanjuti:
  `read udp ... i/o timeout` → "Sesi Xiaomi kemungkinan kedaluwarsa, login ulang di go2rtc";
  `lookup account.xiaomi.com ... i/o timeout` → "Masalah DNS".
- Statistik sistem dari `/proc` (CPU, RAM, load) dan `statfs` untuk `/data`.
- Perubahan status dipush ke browser lewat SSE.

### Detector (fase 3)
```go
type Detector interface {
    Detect(ctx context.Context, img image.Image) ([]Detection, error)
}
```
- Implementasi awal: ONNX Runtime (CGO, `libonnxruntime` arm64 di image) dengan model
  deteksi orang kecil (kelas YOLO nano, input 320). Karena CGO, detektor dibangun dengan
  build tag `onnx`; build tanpa tag tetap jalan dengan deteksi gerak saja.
- Motion gate dihitung di Go murni pada frame grayscale 320 px: AI hanya dipanggil bila
  ada gerak, maksimal 2 inferensi/detik per kamera.
- Interface yang sama memungkinkan memindahkan inferensi ke mesin lain nanti (mis. Pi)
  lewat HTTP tanpa mengubah alur.

## HTTP server

- `chi` dengan middleware: `RequestID`, `RealIP` (hanya bila di belakang reverse proxy
  tepercaya), `Recoverer`, logger slog, `Timeout` (kecuali rute streaming/SSE/file).
- File rekaman disajikan dengan `http.ServeContent` (dukungan `Range` untuk seek video).
- Frontend: `embed.FS`, fallback ke `index.html` untuk rute SPA, header cache panjang
  untuk aset ber-hash.
- Security header: `Content-Security-Policy` ketat, `X-Content-Type-Options`,
  `Referrer-Policy`, `frame-ancestors 'none'`.

## Error dan validasi

- Respons error seragam: `{"error": {"code": "camera_not_found", "message": "..."}}`.
- Validasi input di handler, sebelum menyentuh store.
- Error ffmpeg/go2rtc tidak pernah dikirim mentah ke klien kecuali di halaman status admin.

## Testing

- Unit test untuk builder argumen ffmpeg, parser CSV segmen, logika retensi (pakai
  filesystem sementara), parser error go2rtc.
- Test store terhadap SQLite in-memory dengan migrasi asli.
- Integration test opsional dengan file video kecil (`testdata/`) sebagai sumber RTSP
  tiruan via go2rtc.
- `go vet`, `staticcheck`, `go test -race` di CI.
