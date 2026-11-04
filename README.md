# CCTV Rumah

Server CCTV rumah buatan sendiri, **NVR Lite**: rekam 24 jam, live view, playback dengan
timeline, export klip, timelapse harian, notifikasi Discord, dan aplikasi web yang bisa
di-install di HP. Dibuat untuk hardware kecil — berjalan di STB `umma` (ARM A53, RAM
1,8 GB) dengan beban ~15% CPU untuk 3 kamera, karena video hanya di-copy, tidak
di-decode.

- **Aplikasi:** https://cctv.tarkiman.com
- **Kode & dokumentasi:** [`nvr/`](nvr/README.md)
- **Daftar kamera & cara aksesnya:** [CAMERAS.md](CAMERAS.md)
- **Deploy & operasional:** [nvr/docs/08-deployment.md](nvr/docs/08-deployment.md)
- **Rencana berikutnya:** [nvr/docs/11-rencana-pengembangan.md](nvr/docs/11-rencana-pengembangan.md)

| Lapisan | Pilihan |
|---|---|
| Backend | Go, chi, sqlx, SQLite (WAL) |
| Frontend | Vue 3, Vite, TypeScript, Pinia, Tailwind CSS (PWA) |
| Stream hub | go2rtc (termasuk Xiaomi P2P untuk `c301`) |
| Video | ffmpeg `-c copy` (rekam, remux, export), keyframe saja untuk snapshot |
| Deploy | Docker Compose di `umma`, image arm64 dari GHCR lewat GitHub Actions |

Sebelumnya repo ini berisi stack Frigate + Mosquitto + panel + timelapse; stack itu
diganti NVR Lite dan dihapus pada 2026-11-04 (alasan & angka pengukuran di
[nvr/README.md](nvr/README.md#kenapa-bukan-frigate-lagi)).
