# 10 — Catatan keputusan (ADR)

Format: konteks → keputusan → konsekuensi. Keputusan baru ditambahkan di bawah; yang
dibatalkan tidak dihapus, cukup ditandai **Diganti oleh Dn**.

## D1. Membuat NVR sendiri, bukan tetap di Frigate
- **Konteks:** Frigate di `umma` memakai ~150–215% CPU dan ~500–580 MB RAM untuk 3 kamera;
  swap habis. Fitur yang dipakai hanya live, rekaman, timelapse, deteksi orang di satu
  kamera.
- **Keputusan:** buat NVR ringan khusus kebutuhan ini.
- **Konsekuensi:** kehilangan pelacakan objek, UI review Frigate, ekosistem integrasi.
  Biaya pengembangan & perawatan ditanggung sendiri.

## D2. Go + chi untuk backend
- **Konteks:** proses jangka panjang di hardware kecil; pengguna terbiasa dengan Go.
- **Keputusan:** Go dengan router `chi` (bukan Gin), `net/http` standar.
- **Konsekuensi:** binary tunggal, RAM kecil, cross-compile mudah. `chi` kompatibel
  penuh dengan `http.Handler`, jadi middleware standar bisa dipakai langsung.

## D3. SQLite, bukan MariaDB
- **Konteks:** permintaan awal menyebut MariaDB. Data yang disimpan kecil (indeks segmen
  ~13 ribu baris/bulan, kamera, pengguna, event), satu penulis, satu mesin dengan RAM
  1,8 GB.
- **Keputusan:** SQLite mode WAL via `modernc.org/sqlite` + `sqlx`.
- **Alasan:**
  - Tidak ada server database tambahan — MariaDB butuh ~100–200 MB RAM dan satu container
    lagi di host yang sudah kekurangan memori.
  - Backup cukup satu file (`VACUUM INTO`).
  - Performa jauh lebih dari cukup untuk beban ini.
- **Konsekuensi:** tidak cocok bila nanti banyak instans menulis ke satu database. Karena
  memakai `sqlx` dan SQL portabel, pindah ke MariaDB/PostgreSQL tetap mungkin dengan
  mengganti driver dan sedikit penyesuaian migrasi.

## D4. go2rtc sebagai hub semua stream
- **Konteks:** `c301` hanya bisa diakses lewat go2rtc. Kamera murah tidak kuat banyak
  koneksi.
- **Keputusan:** semua kamera masuk go2rtc; rekaman, snapshot, live, deteksi mengambil
  dari restream go2rtc.
- **Konsekuensi:** go2rtc menjadi titik tunggal kegagalan — dipantau oleh `health` dan
  di-restart Docker. Satu hop tambahan, tapi hanya remux di memori.

## D5. ffmpeg subprocess, bukan RTSP native di Go
- **Konteks:** library Go seperti `gortsplib` bisa merekam tanpa ffmpeg, tetapi menulis
  MP4/TS dan menangani variasi codec/timestamp kamera murah butuh banyak kode.
- **Keputusan:** pakai ffmpeg (`-c copy`) yang diawasi supervisor Go.
- **Konsekuensi:** satu proses ffmpeg per kamera (beberapa MB RAM masing-masing).
  Bisa ditinjau ulang bila overhead ffmpeg ternyata signifikan di spike S1.

## D6. Rekam ke MPEG-TS, remux ke MP4 setelah segmen selesai
- **Konteks:** MP4 biasa rusak bila proses mati sebelum file ditutup; fragmented MP4
  tanpa indeks sering sulit di-seek di browser.
- **Keputusan:** ffmpeg menulis `.ts`; setelah segmen selesai, `nvrd` me-remux ke `.mp4`
  `faststart` (`-c copy`).
- **Konsekuensi:** tulis dua kali per segmen (I/O, bukan CPU). Divalidasi di spike S2.

## D7. Tidak pernah men-decode stream utama terus-menerus
- **Konteks:** decode HEVC 2304x1296 adalah penyebab utama beban Frigate.
- **Keputusan:** stream utama hanya di-copy. Snapshot pakai keyframe saja. Deteksi hanya
  dari sub-stream.
- **Konsekuensi:** kamera tanpa sub-stream tidak bisa diberi deteksi murah; untuk itu
  pakai deteksi gerak bawaan kamera (Dafang-hacks → MQTT) atau tanpa deteksi.

## D8. Frontend Vue 3 di-embed ke binary
- **Keputusan:** Vue 3 + Vite + TypeScript + Tailwind + Reka UI; hasil build di-embed
  `embed.FS` ke `nvrd`.
- **Konsekuensi:** satu artefak deploy, satu origin (tanpa CORS), versi frontend & backend
  selalu cocok.

## D9. SSE untuk push realtime
- **Keputusan:** Server-Sent Events untuk status/event; WebSocket hanya untuk proxy live
  view go2rtc.
- **Konsekuensi:** lebih sederhana dari WebSocket, reconnect otomatis di browser.

## D10. Deploy dari branch `nvr-lite`
- **Konteks:** permintaan pengguna: kerja dan deploy dari branch khusus dulu.
- **Keputusan:** CI membangun image `:nvr-lite` dari branch ini; `umma` men-clone branch
  ini ke `~/cctv-nvr`. Stack Frigate di root repo dan `~/cctv` tetap utuh sampai cutover
  dinyatakan stabil.
- **Konsekuensi:** `main` belum berubah; setelah stabil, branch digabung lewat PR dan
  stack Frigate dihapus dari repo.
