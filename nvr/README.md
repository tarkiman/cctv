# NVR Lite — server CCTV rumah buatan sendiri

Pengganti stack Frigate (sudah dihapus dari repo) untuk CCTV rumah. Tujuannya satu: **ringan** di hardware kecil
(target utama STB `umma`: ZTE B860H, ARM Cortex-A53 4 core, RAM 1,8 GB), tanpa kehilangan
fungsi yang benar-benar dipakai: live view, rekaman yang bisa diputar ulang, timelapse,
dan deteksi orang di kamera prioritas.

> Status: **Fase 1 & 2 selesai, berjalan di `umma` sejak 2026-11-04** di
> https://cctv.tarkiman.com. Pengembangan di `main`; rencana berikutnya ada di
> [11 — Rencana pengembangan](docs/11-rencana-pengembangan.md).

## Kenapa bukan Frigate lagi

Pengukuran di `umma` (2026-11-04) dengan Frigate 0.17 dan 3 kamera:

| Proses | CPU |
|---|---|
| ffmpeg decode stream utama `bardi` (HEVC 2304x1296) | ~90% |
| ffmpeg decode stream kedua | ~63% |
| Detektor AI | inferensi ~10 ms — bukan masalah |
| Proses Python Frigate | ~10% CPU, ~200 MB RAM |

Yang berat adalah **decode video terus-menerus**, bukan AI-nya. Karena itu prinsip utama
proyek ini:

> **Video hanya di-copy, tidak pernah di-decode terus-menerus.** Decode hanya untuk satu
> keyframe sesekali (snapshot/timelapse), atau untuk sub-stream resolusi rendah pada
> kamera yang deteksinya aktif.

## Stack

| Lapisan | Pilihan |
|---|---|
| Backend | Go, router [chi](https://github.com/go-chi/chi), `sqlx` (tanpa ORM) |
| Database | SQLite (WAL) via `modernc.org/sqlite` — lihat [keputusan D3](docs/10-keputusan.md#d3-sqlite-bukan-mariadb) |
| Frontend | Vue 3 + Vite + TypeScript, Pinia, Vue Router, Tailwind CSS |
| Stream hub | [go2rtc](https://github.com/AlexxIT/go2rtc) (sudah dipakai untuk `c301`) |
| Rekaman/snapshot | `ffmpeg` sebagai subprocess (`-c copy`) |
| Deploy | Docker Compose, image arm64 dari GHCR (`:latest`, `:<sha7>`) |

## Dokumentasi

| Dokumen | Isi |
|---|---|
| [01 — Tujuan & lingkup](docs/01-tujuan-dan-lingkup.md) | Goal, non-goal, kebutuhan fungsional, target resource |
| [02 — Arsitektur](docs/02-arsitektur.md) | Komponen, alur data, prinsip hemat CPU |
| [03 — Kamera & stream](docs/03-kamera-dan-stream.md) | Inventaris kamera, stream mana dipakai untuk apa |
| [04 — Backend](docs/04-backend.md) | Struktur kode Go, modul, proses latar, konfigurasi |
| [05 — Database](docs/05-database.md) | Skema SQLite, migrasi, konvensi query |
| [06 — API](docs/06-api.md) | Spesifikasi REST + SSE + proxy live |
| [07 — Frontend & UI/UX](docs/07-frontend-uiux.md) | Halaman, alur, prinsip UX, design token, aksesibilitas |
| [08 — Deployment](docs/08-deployment.md) | CI, deploy & update di `umma`, notifikasi, Cloudflare, rollback |
| [09 — Roadmap](docs/09-roadmap.md) | Fase, kriteria selesai |
| [10 — Keputusan](docs/10-keputusan.md) | Catatan keputusan arsitektur (ADR) |
| [11 — Rencana pengembangan](docs/11-rencana-pengembangan.md) | Pekerjaan berikutnya: stabilisasi, Fase 3 deteksi, backlog |

## Struktur folder

```
nvr/
├── README.md
├── docs/                 # dokumen ini
├── server/               # Go module (cmd/nvrd, internal/...)
├── web/                  # Vue 3 app (di-embed ke binary saat build)
├── deploy/               # docker-compose.yml, contoh .env & go2rtc.yaml
└── Dockerfile            # multi-stage: web build -> go build -> runtime + ffmpeg
```
