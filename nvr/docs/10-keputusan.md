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
- **Status (2026-11-04):** selesai — lihat D11.

## D11. `nvr-lite` digabung ke `main`, Frigate dihapus
- **Konteks:** pengguna menyetujui hasil Fase 1–2 pada 2026-11-04.
- **Keputusan:** `nvr-lite` di-fast-forward ke `main`; pengembangan selanjutnya di `main`.
  CI membangun `ghcr.io/tarkiman/cctv-nvr:latest` dan `:<sha7>` dari `main`. Stack
  Frigate (folder `frigate/`, `panel/`, `timelapse/`, `mosquitto/`, compose root,
  workflow `deploy.yml`) dihapus dari repo, beserta container, image, dan datanya di
  `umma` dan Pi.
- **Konsekuensi:** tidak ada lagi jalur rollback ke Frigate. Rollback berarti kembali ke
  image NVR Lite sebelumnya (`NVR_TAG=<sha7>`).

## D12. Deteksi bisa diatur per kamera; model orang di layanan terpisah
- **Konteks:** pengguna (2026-11-05) ingin deteksi orang bisa dinyalakan/dimatikan per
  kamera sesuai kemampuan server. Spike S7: YOLOX-nano 562 ms/frame, ~300 MB RAM di `umma`.
  `bardi` adalah kamera PTZ yang **mengikuti gerakan** (auto-tracking).
- **Keputusan:**
  - Saklar `detect_enabled` per kamera + pengaturan global (frame/detik, ambang, luas gerak),
    berlaku tanpa restart (fps → worker tersambung ulang sendiri).
  - Motion gate Go murni di dalam `nvrd` (biaya ~0); model orang (tahap B2) di layanan
    `nvr-detect` terpisah lewat HTTP, dipanggil hanya saat ada gerak. Kalau semua kamera
    mati deteksinya, layanan itu tidak dipakai sama sekali; bisa dipindah ke mesin lain
    dengan mengganti URL.
  - Perubahan gambar total (PTZ berputar, pergantian IR) dihitung sebagai gerak: pada
    kamera auto-tracking itu sinyal ada sesuatu bergerak.
- **Konsekuensi:** zona (poligon) tidak berlaku untuk kamera PTZ; false positive (kucing)
  ditekan dengan ambang skor + konfirmasi 2 frame. Crash detektor tidak menghentikan
  perekaman.

## D13. Lokasi sebagai grup kamera dan dasar hak akses
- **Konteks:** pengguna punya CCTV di lokasi lain, direkam terpusat di `umma` lewat gerbang
  (`pipit`). Pilihan yang dibahas: akses per user langsung, grup user, atau grup kamera.
- **Keputusan:** setiap kamera masuk tepat satu **lokasi**. Akses viewer: `access_all`
  (semua, termasuk yang baru), atau daftar lokasi (otomatis mencakup kamera baru) dengan
  pengecualian kamera (`deny_camera`), plus kamera tunggal dari lokasi lain. Admin selalu
  semua. Belum ada grup user.
- **Penegakan:** satu middleware memuat izin per request; `cameraFromPath`, live (WS/WebRTC),
  rekaman per ID, timelapse, statistik, dan SSE (event kamera difilter per koneksi)
  memeriksanya. Kamera yang tidak boleh dilihat dijawab `404`, bukan `403`.
- **Konsekuensi:** perubahan izin berlaku pada request berikutnya; koneksi SSE yang sudah
  terbuka memakai izin saat tersambung sampai tersambung ulang. Notifikasi Discord masih
  satu channel untuk semua (webhook per lokasi bisa ditambahkan).

## D14. Suara direkam sebagai AAC, dengan jalan mundur ke video saja
- **Konteks:** pengguna ingin rekaman bersuara (2026-11-09). Ketiga kamera mengirim audio
  berbeda: `bardi` G.711 A-law 8 kHz, `c301` Opus 48 kHz (timestamp-nya dari go2rtc maju
  ±0,06× kecepatan nyata), `cam1` MP2 yang tidak bisa di-decode sama sekali (ffmpeg
  berhenti "Decode error rate", sehingga video pun tidak terekam).
- **Keputusan:** `record_audio` per kamera, **nyala secara default**. Audio selalu
  di-transcode ke AAC 16 kHz mono 32 kbps (±350 MB/kamera/hari); video tetap stream copy.
  Sumber Xiaomi memakai timestamp waktu kedatangan untuk audio dan video sekaligus,
  supaya sinkron dan tidak bergeser seiring waktu. Audio yang tidak terbaca membuat
  perekam lanjut tanpa suara (status `unavailable`) sampai pengaturan kamera diubah
  atau nvrd dimulai ulang; percobaan yang gagal memakan ±1 menit rekaman (ffmpeg
  memeriksa stream audio rusak selama itu), jadi tidak diulang berkala. Timeout tanpa
  error audio (sumber lambat) hanya memblokir audio 1 jam.
- **Alternatif ditolak:** copy audio apa adanya (G.711/MP2 tidak valid atau tidak bisa
  diputar di MP4 browser); transcode di go2rtc (`ffmpeg:` source) — timestamp tetap rusak
  dan menambah proses per kamera; `asetpts=N/SR/TB` untuk Xiaomi — benar di awal tetapi
  bergeser bila ada paket hilang.
- **Konsekuensi:** satu encoder AAC per kamera (beban kecil; diukur di `umma`). Live dan
  rekaman mulai bisu (aturan autoplay browser) dengan tombol suara. Bicara balik belum.
