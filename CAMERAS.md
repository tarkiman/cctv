# Daftar Kamera CCTV

Referensi cara akses tiap kamera CCTV di rumah. Kamera-kamera ini beda merek/tipe/status
firmware (custom Dafang-hacks vs stock Mi Home), jadi cara aksesnya TIDAK SERAGAM —
jangan asumsikan pola kamera lama otomatis berlaku ke kamera baru. Update file ini
setiap kali ada kamera baru ditambahkan atau ada perubahan (IP, kredensial, dll).

Kredensial asli (password) sengaja TIDAK ditulis di sini karena file ini di-commit ke
git. Sumber stream (termasuk kredensial) tersimpan di `go2rtc.yaml` NVR Lite di `umma`
(`/DATA/cctv/nvr/go2rtc/go2rtc.yaml`, tidak di git) dan bisa diubah lewat web:
**Pengaturan → Kamera → Sumber & koneksi** (password tidak pernah ditampilkan).

---

## cam1

| | |
|---|---|
| IP | `192.168.1.14` (per 2026-08-13; sudah berubah 3x dalam beberapa hari: `.12` → `.13` → `.14` -- lihat catatan) |
| Firmware | [Xiaomi-Dafang-Hacks](https://github.com/EliasKotlyar/Xiaomi-Dafang-Hacks) |
| Akses SSH/root | `root` / `ismart12` (default firmware, lihat README proyek Dafang-hacks) |
| Web UI | `http://192.168.1.14` (port 80) |
| RTSP | `rtsp://192.168.1.14:8554/unicast` — **tanpa auth** |
| Jalur di NVR | Sejak 2026-11-06 (uji gerbang lokasi) NVR menarik `cam1` **lewat `pipit`**: `rtsp://172.22.195.55:8554/cam1` (user `nvr`, password RTSP gerbang). Kembalikan ke `rtsp://192.168.1.14:8554/unicast` lewat *Sumber & koneksi* bila pipit dicabut. |
| Video | H.264, 1280x720, 25 fps, audio MP3 |
| Catatan | **IP sudah 3x berubah dalam beberapa hari** (`.12`→`.13`→`.14`) meski tercatat "statis" -- kemungkinan sebenarnya masih DHCP biasa (bukan reservation) di router, worth dicek/diperbaiki di sisi router supaya tidak perlu update config manual tiap kali berubah. Cara ganti IP: **Pengaturan → Kamera → cam1 → Sumber & koneksi**, ganti IP, *Tes koneksi*, lalu *Simpan sumber* (perekam tersambung ulang sendiri). Kalau kamera cuma mati/nyala tanpa ganti IP, perekam reconnect otomatis. Cara dapat RTSP URL: aktifkan RTSP lewat web UI kamera, defaultnya di path `/unicast` port `8554`. |

## bardi

| | |
|---|---|
| IP | `192.168.1.27` (statis) |
| Firmware | Dafang-hacks juga (chip **HI3518C**, sama keluarga dengan cam1), tapi dengan **ONVIF diaktifkan** |
| Web UI / SSH / Telnet | Port 80/22/23 **tertutup/unreachable** dari Pi ini — belum ditemukan cara akses langsung, hanya bisa lewat ONVIF |
| ONVIF device service | `http://192.168.1.27:10000/onvif/device_service` |
| ONVIF media service | `http://192.168.1.27:10000/onvif/media_service` (didapat dari `GetCapabilities`) |
| ONVIF auth | WS-Security UsernameToken + PasswordDigest. User: `admin`. Password: sama dengan RTSP (lihat go2rtc.yaml NVR Lite). |
| RTSP | `rtsp://192.168.1.27:554/V_ENC_000` — **butuh auth** (user `admin`, password sama seperti ONVIF; tersimpan di go2rtc.yaml NVR Lite) |
| Video | **HEVC/H.265**, 2304x1296, audio `pcm_alaw` — beda codec dari cam1 (H.264)! |
| Sub-stream | `rtsp://192.168.1.27:554/V_ENC_001` — **H.264 640x360 @15 fps**, auth sama (dicek `ffprobe` 2026-11-04; `V_ENC_002` = 404). Dipakai NVR Lite sebagai stream `bardi_sub` (grid live) dan nanti untuk deteksi (fase 3). |
| Profile / encoder token | `PROFILE_000` / `V_ENC_000` (bisa beda kalau kamera di-reset firmware) |

**Cara dapat ulang RTSP URL bardi kalau lupa / berubah** (mis. setelah reset firmware):
1. WS-Discovery: kirim SOAP Probe ke UDP `<ip>:3702` (unicast langsung ke IP kamera,
   tidak perlu multicast) → response berisi `XAddrs` = URL device service ONVIF
   (biasanya port `10000`).
2. `POST` SOAP `GetCapabilities` ke device service (tidak perlu auth) → ambil
   `Media.XAddr` dari response.
3. `POST` SOAP `GetProfiles` ke media service (**butuh** WS-Security UsernameToken
   PasswordDigest) → ambil `ProfileToken` (mis. `PROFILE_000`).
4. `POST` SOAP `GetStreamUri` dengan `ProfileToken` itu → dapat `Uri` RTSP resmi di
   response (`tt:Uri`).
5. Test dengan `ffprobe -rtsp_transport tcp "rtsp://user:pass@<ip>:554/<path>"`.

Detail langkah 1-4 (termasuk cara hitung WS-Security PasswordDigest) ada di riwayat
kerja proyek ini — kalau perlu script Python-nya lagi, minta dibuatkan ulang, polanya
standar ONVIF (WS-Discovery UDP probe + SOAP dengan `UsernameToken`/`PasswordDigest`).

## c301

| | |
|---|---|
| IP | `192.168.1.4` (per 2026-11-04; riwayat: `.7`→`.6`→`.2`→`.4` dalam ~1 bulan -- kuat menunjukkan DHCP biasa, bukan reservation, lihat catatan CAMERAS.md cam1). MAC `50:7B:91:79:02:CA` (tetap sama tiap kali, konfirmasi device fisik sama). |
| Model | Xiaomi Smart Camera C301 (model code go2rtc/cloud: `mxiang.camera.c301`) |
| Firmware | **Stock Mi Home** — bukan custom firmware seperti cam1/bardi. Tidak ada opsi ONVIF di app kameranya sama sekali. |
| Web UI / SSH / Telnet / RTSP / ONVIF langsung | **Semua tertutup** — port 80, 22, 23, 554, 8554, 10000 semuanya closed/filtered dari Pi ini. Satu-satunya jalan masuk: protokol P2P proprietary Xiaomi (disebut `cs2` di go2rtc). |
| Cara akses | Lewat container `nvr-go2rtc` (`nvr/deploy/docker-compose.yml`, image `alexxit/go2rtc:1.9.14`; dukungan Xiaomi `cs2` butuh go2rtc ≥1.9.13). Wajib `network_mode: host` karena protokol P2P butuh UDP hole-punching yang gagal di belakang NAT bridge Docker (error kalau salah: `read punch: i/o timeout`). |
| Login akun cloud | Mi Home account, **region `sg` (Singapura)** — bukan `cn`/China meski akun terdaftar dari Indonesia (Xiaomi tidak punya server khusus Indonesia, Asia Tenggara termasuk Indonesia dilayani server Singapura). `account_id` dan `did` (device ID) TIDAK ditulis di sini (repo publik) — cek `/DATA/cctv/nvr/go2rtc/go2rtc.yaml` di `umma` (tidak di git, berisi sesi login cloud + config stream asli). **Sesi login ini kedaluwarsa setelah ~2 minggu idle** (lihat Catatan). |
| go2rtc stream URL | `xiaomi://<account_id>:sg@192.168.1.2?did=<did>&model=mxiang.camera.c301` (nilai asli ada di go2rtc.yaml NVR Lite, key stream `c301`) |
| go2rtc listen port | API/dashboard `127.0.0.1:1984` (buka lewat `ssh -L 1984:127.0.0.1:1984 tarkiman@192.168.1.20`), RTSP restream `127.0.0.1:8556`, WebRTC `8557` |
| Video | **HEVC/H.265**, sekarang 848x480 (sempat 2304x1296 di awal setup -- kemungkinan Mi Home app/camera menurunkan profil stream default, belum dikonfirmasi kenapa), audio Opus (di-transcode go2rtc dari format asli) |
| Catatan | **Pola berulang, sejauh ini 2x** (2026-08-26 dan 2026-09-11, jarak ~16 hari -- konsisten dengan "sesi basi setelah ~2 minggu idle"): koneksi P2P putus total, go2rtc log `read udp ... i/o timeout` + perekam `404 Not Found` saat DESCRIBE. NVR Lite mengirim notifikasi Discord **"🔑 c301: sesi Xiaomi perlu login ulang"** lengkap dengan langkahnya. **Fix-nya SELALU sama dan sudah terbukti 2x**: login ulang manual lewat dashboard go2rtc (`/add.html`) dengan akun Mi Home yang sama, region `sg`. Tidak perlu coba-coba hal lain (router/firewall/upgrade versi) dulu -- langsung ke login ulang. IP kamera juga cenderung ikut berubah tiap kali ini terjadi (riwayat `.7`→`.6`→`.2`), jadi setelah login ulang selalu cek device list untuk IP terbaru, bukan asumsi IP lama masih berlaku. Sejak NVR Lite, kejadian ini terdeteksi otomatis dalam ~5 menit lewat notifikasi. |

**Cara setup ulang / kamera Xiaomi Mi Home lain yang mirip (stock firmware, tanpa ONVIF):**
1. Cek dulu apakah benar tidak ada ONVIF (`CAMERAS.md` prosedur ONVIF di atas) — kalau
   masih ada opsi ONVIF di app kameranya, pakai pola `bardi`, jauh lebih sederhana.
2. Cek model code di [go2rtc Known Xiaomi cameras](https://github.com/AlexxIT/go2rtc/issues/1982)
   apakah didukung (protokol `cs2` jauh lebih stabil dari `tutk`).
3. Buka dashboard go2rtc lewat SSH tunnel (`http://localhost:1984/add.html`), login akun Mi Home
   (coba region `sg` dulu untuk akun Indonesia/Asia Tenggara), cari device, catat
   `account_id`, `region`, `did`.
4. Tambahkan stream dari dashboard itu (format sumber:
   `xiaomi://<account_id>:<region>@<ip>?did=<did>&model=<model_code>`); go2rtc menyimpannya
   ke go2rtc.yaml.
5. Di NVR Lite: **Pengaturan → Kamera → Sinkronkan** (ikon ↻); kamera baru langsung
   direkam.
6. Ingat: login cloud tetap dibutuhkan tiap kali konek (bukan air-gapped sepenuhnya) —
   video streaming-nya sendiri lokal, tapi ambil kunci enkripsi tetap butuh internet ke
   server Xiaomi setiap sesi baru.

---

## Kamera berikutnya

Sebelum menulis config untuk kamera baru, cek satu per satu (berhenti di langkah yang
berhasil):
1. **Pola `cam1`** — coba RTSP unicast langsung, port 8554, tanpa auth. Paling sederhana
   kalau berhasil.
2. **Pola `bardi`** — kalau port 8554/80/22/23 tertutup tapi ONVIF tersedia (cek di app
   kameranya atau coba WS-Discovery), ikuti prosedur "Cara dapat ulang RTSP URL bardi"
   di atas.
3. **Pola `c301`** — kalau SEMUA port tertutup dan tidak ada opsi ONVIF di app kamera
   (biasanya kamera Xiaomi Mi Home stock/non-hack), coba go2rtc + protokol P2P Xiaomi,
   ikuti prosedur "Cara setup ulang" di atas.
4. Selalu cek codec video (tombol *Tes koneksi* menampilkannya) sebelum asumsi H.264 —
   beberapa kamera di sini ternyata HEVC/H.265; tambahkan sub-stream H.264 bila ada.
5. Tambahkan kamera RTSP lewat **Pengaturan → Kamera → Tambah kamera** (ada *Tes
   koneksi*), lalu tambahkan entri baru di file ini dengan format yang sama.
