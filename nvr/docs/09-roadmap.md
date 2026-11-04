# 09 — Roadmap

> Fase 1 dan 2 selesai pada 2026-11-04. Urutan kerja berikutnya (stabilisasi, Fase 3,
> backlog) ada di [11 — Rencana pengembangan](11-rencana-pengembangan.md).

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
- Retensi (umur + watermark disk). URL sumber dibangun dengan `net/url` (password bisa mengandung `@`).
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

- ✅ UI kelola kamera (2026-11-04): tambah kamera RTSP, ubah sumber (mis. IP berubah; password lama dipertahankan), tes koneksi (codec/resolusi/fps + saran), hapus kamera beserta datanya; kamera Xiaomi hanya-baca. Deteksi Xiaomi kini dari `/api/config` go2rtc, bukan dari producer yang sedang jalan.
- Pengaturan rekaman & retensi dengan perkiraan disk.
- ✅ Export klip (2026-11-04): pilih mulai + durasi di halaman Rekaman, rentang ditandai di timeline, perkiraan ukuran; server concat + `-c copy` (tanpa re-encode), file sementara di `/data/exports` dihapus setelah diunduh.
- ✅ Notifikasi webhook (2026-11-04): kamera offline > 5 menit, disk < 10%, sesi Xiaomi `c301`
  kemungkinan kedaluwarsa (termasuk pengingat proaktif di hari ke-14).
- ✅ Multi-user (2026-11-04): kelola pengguna admin/viewer di Pengaturan (password acak ditampilkan sekali, reset mengeluarkan semua perangkat), daftar perangkat yang masuk + keluarkan; ganti password sendiri sudah ada sejak fase 1.
- ✅ PWA (2026-11-04): manifest + ikon (maskable, apple-touch), service worker yang hanya meng-cache shell aplikasi (`/api` tidak pernah di-cache), kartu "Pasang aplikasi" di Pengaturan (prompt Android/desktop, petunjuk iOS).

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

Dijalankan di `umma` 2026-11-04 malam (19:15–19:35 WIB+1), ffmpeg 6.1.2 dari Alpine
3.22, go2rtc 1.9.14. `cam1` sedang offline sehingga hanya `bardi` dan `c301` yang diuji.
Pembanding: Frigate di mesin yang sama memakai 123–215% CPU dan 460–580 MB RAM.

| # | Hasil | Status |
|---|---|---|
| S1 | 2 recorder `-c copy` + go2rtc, rata-rata 9 menit: `bardi` 3,2% CPU / 21 MB, `c301` 4,0% / 21 MB, go2rtc 8,8% / 13 MB. **Total ~16% CPU, ~55 MB RAM.** RAM tersedia host naik dari 660 MB ke 1,2 GB. Ukuran (malam hari): `bardi` ~2,4 MB/menit (~3,4 GB/hari), `c301` ~0,8 MB/menit (~1,2 GB/hari). | Lolos |
| S2 | `-segment_list pipe:1 -segment_list_type csv` mengeluarkan satu baris per segmen selesai (`nama,mulai,akhir`). Remux `.ts`→`.mp4` segmen 60 s: ~1,2 s, ~1 s CPU; `moov` di depan `mdat` (siap seek); perlu `-tag:v hvc1` untuk HEVC agar Safari mau memutar. Seek ke detik 40 dengan ffmpeg ~1 s. | Lolos (seek di browser menunggu S4) |
| S3 | Snapshot `-skip_frame nokey`: 2,3–3,3 s per pengambilan (menunggu keyframe), CPU < 1 s. Untuk 1 snapshot/menit/kamera ≈ 1–2% satu core. | Lolos |
| S4 | Opera 138 (Chromium 154) di Windows, lewat ZeroTier: H.264 & HEVC `canPlayType=probably`, MSE ya. Ketiga MP4 hasil remux terputar: `bardi` HEVC load 201 ms / seek 435 ms, `c301` HEVC 253 / 136 ms, `bardi_sub` H.264 358 / 56 ms. Belum diuji: browser HP, live view go2rtc. | Lolos di desktop; HP tertunda |
| S5 | `PUT /api/streams?name=&src=` **ditulis ke `go2rtc.yaml`** (persisten). `PATCH` hanya mengubah memori, tidak ditulis ke file. Gunakan `PUT` (atau `DELETE` + `PUT`) untuk sinkronisasi. | Lolos, dengan catatan |
| S6 | Decode `bardi_sub` (H.264 640x360) → `fps=2,scale=320,gray`: ~7 s CPU per 60 s (~12% satu core, ~3% total). Hanya keyframe: ~2,6 s per 60 s (~4% satu core). | Lolos |

### Temuan tambahan
- **Password dengan karakter khusus.** Password `bardi` mengandung `@`; URL RTSP harus
  di-percent-encode (`%40`) sebelum dikirim ke go2rtc/ffmpeg. `nvrd` wajib membangun URL
  dengan `net/url` (`url.UserPassword`), bukan penggabungan string.
- **Timestamp kosong.** ffmpeg memperingatkan "Timestamps are unset" untuk `bardi`. Tambah
  `-fflags +genpts` pada input recorder (Frigate juga memakai `+genpts` dan
  `-use_wallclock_as_timestamps 1`); validasi di Fase 1.
- **Batas segmen.** Dengan `-segment_atclocktime 1` segmen dipotong di keyframe pertama
  setelah batas jam (mis. `11:19:03`), bukan tepat di detik `:00`. Waktu mulai harus
  diambil dari CSV/nama file, bukan diasumsikan.
- **IP kamera berubah lagi.** `c301` pindah dari `.2` ke `.4`; `cam1` tidak terjangkau
  di `.14` maupun alamat lain (port 8554 tidak ada yang terbuka). Reservasi DHCP di router
  makin penting; fitur "cari ulang kamera berdasarkan MAC" layak masuk Fase 2.
- **Izin `/DATA`.** User `tarkiman` (uid 1001 di `umma`, bukan 1000) tidak bisa membuat
  folder langsung di `/DATA`; butuh `sudo` sekali untuk `/DATA/nvr`. Container harus
  berjalan sebagai uid 1001 atau folder di-`chown` ke uid container.
- **Kapasitas disk jauh lebih longgar** dari perkiraan awal: bila angka siang hari mirip,
  3 kamera 24 jam hanya ~5–10 GB/hari, sehingga retensi 30 hari pun muat. Perlu diukur
  ulang di siang hari.
