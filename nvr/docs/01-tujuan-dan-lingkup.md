# 01 — Tujuan & lingkup

## Tujuan

1. **Ringan.** Berjalan nyaman di `umma` (Cortex-A53 4 core, RAM 1,8 GB) bersama layanan
   lain, tanpa menghabiskan swap.
2. **Rekaman yang bisa diandalkan.** Rekaman 24 jam per kamera (bisa dimatikan per
   kamera), mudah dicari dan diputar ulang dari HP.
3. **Live view cepat** dari LAN maupun ZeroTier.
4. **Deteksi orang** di kamera prioritas (`bardi`), dengan klip yang disimpan lebih lama
   dan notifikasi.
5. **Mudah dirawat.** Satu binary Go + go2rtc, konfigurasi kamera lewat UI, status
   kesehatan terlihat jelas (termasuk sesi Xiaomi `c301` yang kedaluwarsa).

## Non-goal (sengaja tidak dikerjakan)

- Pelacakan objek antar-frame, zona kompleks, face/plate recognition.
- Transcode video di server (kecuali timelapse harian yang dijalankan malam hari).
- Multi-site / cloud. Sistem ini lokal; akses jarak jauh lewat ZeroTier.
- Aplikasi mobile native. Cukup PWA yang bisa di-install di HP.
- Kompatibilitas API Frigate atau integrasi Home Assistant (bisa dipertimbangkan nanti).

## Kebutuhan fungsional

| Kode | Kebutuhan | Fase |
|---|---|---|
| F1 | Login (satu admin; multi-user menyusul), sesi aman | 1 |
| F2 | Live view grid semua kamera + tampilan satu kamera layar penuh | 1 |
| F3 | Rekaman kontinu per kamera dalam segmen (default 10 menit) | 1 |
| F4 | Playback dengan timeline 24 jam, lompat ke waktu tertentu, kecepatan 1–8x | 1 |
| F5 | Retensi otomatis: berdasarkan umur per kamera + batas sisa disk | 1 |
| F6 | Snapshot terbaru per kamera + timelapse harian | 1 |
| F7 | Status kesehatan per kamera dan sistem (disk, CPU, RAM) | 1 |
| F8 | Kelola kamera lewat UI, termasuk tes koneksi (codec, resolusi) | 2 |
| F9 | Export klip (rentang waktu → satu file MP4) | 2 |
| F10 | Notifikasi kamera offline / disk hampir penuh (webhook: WhatsApp/Discord) | 2 |
| F11 | Deteksi gerak + orang pada sub-stream, daftar event, klip event | 3 |
| F12 | Notifikasi event orang dengan snapshot | 3 |

## Kebutuhan non-fungsional

| Aspek | Target |
|---|---|
| CPU (3 kamera, tanpa deteksi) | `nvrd` + `go2rtc` + ffmpeg < 15% total (dari 400%) |
| CPU (dengan deteksi `bardi` sub-stream) | < 50% total |
| RAM | `nvrd` + `go2rtc` + ffmpeg < 200 MB |
| Latensi live (LAN) | < 1 detik (WebRTC/MSE) |
| Mulai playback | < 2 detik setelah klik di timeline |
| Kehilangan rekaman saat crash | maksimal 1 segmen yang sedang ditulis, dan itu pun tetap bisa diputar (MPEG-TS) |
| Penulisan disk | hanya ke `/DATA` (HDD), **tidak pernah** ke eMMC root `umma` (6,5 GB) |
| Zona waktu | simpan UTC, tampilkan WIB (Asia/Jakarta) eksplisit — host `umma` memakai CST |

Angka target di atas adalah sasaran, bukan hasil ukur. Fase 0 punya spike untuk
memvalidasinya (lihat [09 — Roadmap](09-roadmap.md)).
