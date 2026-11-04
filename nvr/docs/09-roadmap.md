# 09 — Roadmap

Setiap fase selesai bila semua kriteria terpenuhi dan diverifikasi di `umma` (kecuali
Fase 0 yang boleh di Pi).

## Fase 0 — Dokumentasi & spike

Tujuan: memastikan asumsi teknis benar sebelum menulis banyak kode.

| # | Spike | Pertanyaan yang dijawab |
|---|---|---|
| S1 | Jalankan 3× `ffmpeg -c copy -f segment` dari go2rtc di `umma` selama 1 jam | Berapa CPU/RAM sebenarnya? Bitrate & ukuran per hari per kamera? |
| S2 | `-segment_list pipe:1 -segment_list_type csv` + remux ts→mp4 | Apakah notifikasi segmen andal? Apakah MP4 hasil remux bisa di-seek mulus di Chrome & Safari? |
| S3 | Snapshot `-skip_frame nokey` dari stream HEVC `bardi` | Berapa lama menunggu keyframe & berapa CPU per snapshot? |
| S4 | Putar HEVC (`bardi` utama, `c301`) via go2rtc MSE/WebRTC di HP & laptop pengguna | Browser mana yang bisa? Perlu fallback apa? |
| S5 | `PUT /api/streams` di go2rtc 1.9.14 | Apakah sinkron stream lewat API persisten/ditulis ke `go2rtc.yaml`? |
| S6 | Decode `bardi_sub` (H.264 640x360) ke `fps=2` gray 320 px | Berapa CPU untuk dasar deteksi? |

Selesai bila: dokumen ini disetujui, hasil S1–S6 dicatat di sini, dan dokumen lain
diperbarui bila ada asumsi yang salah.

## Fase 1 — MVP: rekam, putar, live

- Kerangka `server/` (config, db + migrasi, chi, slog, shutdown rapi) dan `web/`
  (Vite, router, Tailwind, token, AppShell).
- Auth: admin pertama dari env, login/logout, sesi, rate limit.
- Kamera dari seed/`go2rtc.yaml` (UI kelola kamera di fase 2).
- Recorder + indexer + remux + pemulihan `.ts` yatim; supervisor dengan backoff.
- Retensi (umur + watermark disk).
- Snapshot per menit + timelapse harian.
- Health + SSE; halaman Status.
- Halaman Login, Live, Satu kamera (live + playback dengan timeline), Timelapse.
- Dockerfile, compose, CI ke GHCR; deploy & cutover di `umma`.

Kriteria selesai:
- 3 kamera merekam 24 jam tanpa celah selain saat kamera benar-benar mati.
- Playback & live berjalan dari HP (Chrome Android) lewat LAN dan ZeroTier.
- Target CPU/RAM fase 1 tercapai (lihat [01](01-tujuan-dan-lingkup.md)).
- Retensi terbukti menghapus segmen lama (uji dengan retensi 1 hari).
- Restart `umma` → semua pulih sendiri.

## Fase 2 — Kelola & pantau

- UI kelola kamera + tes koneksi + sinkron ke go2rtc.
- Pengaturan rekaman & retensi dengan perkiraan disk.
- Export klip.
- Notifikasi webhook: kamera offline > 5 menit, disk < 10%, sesi Xiaomi `c301`
  kemungkinan kedaluwarsa (termasuk pengingat proaktif di hari ke-14).
- Multi-user (admin/viewer), ganti password.
- PWA (install, ikon, offline shell).

## Fase 3 — Deteksi

- Detector `bardi_sub`: motion gate + model orang (ONNX, image varian `-detect`).
- Event: tabel, proteksi segmen, halaman Event, penanda di timeline.
- Notifikasi orang terdeteksi dengan snapshot, dengan jeda (cooldown) per kamera.
- Ukur ulang CPU; target < 50% total.

## Nanti (belum dijadwalkan)

- Zona sederhana (poligon) untuk deteksi.
- Rekam audio (AAC) per kamera.
- Integrasi MQTT/Home Assistant.
- 2FA (TOTP).
- Memindahkan inferensi ke mesin lain lewat HTTP bila `umma` kurang kuat.

## Hasil spike

_Diisi setelah Fase 0 dijalankan._

| # | Hasil | Tanggal |
|---|---|---|
| S1 | | |
| S2 | | |
| S3 | | |
| S4 | | |
| S5 | | |
| S6 | | |
