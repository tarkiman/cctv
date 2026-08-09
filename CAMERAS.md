# Daftar Kamera CCTV

Referensi cara akses tiap kamera CCTV di rumah. Kamera-kamera ini beda merek/tipe/status
firmware (custom Dafang-hacks vs stock Mi Home), jadi cara aksesnya TIDAK SERAGAM —
jangan asumsikan pola kamera lama otomatis berlaku ke kamera baru. Update file ini
setiap kali ada kamera baru ditambahkan atau ada perubahan (IP, kredensial, dll).

Kredensial asli (password) sengaja TIDAK ditulis di sini karena file ini di-commit ke
git — cek nilai aslinya di `.env` (gitignored). Yang ditulis di sini cuma nama variabel
dan cara mendapatkannya kalau lupa.

---

## cam1

| | |
|---|---|
| IP | `192.168.1.12` (statis) |
| Firmware | [Xiaomi-Dafang-Hacks](https://github.com/EliasKotlyar/Xiaomi-Dafang-Hacks) |
| Akses SSH/root | `root` / `ismart12` (default firmware, lihat README proyek Dafang-hacks) |
| Web UI | `http://192.168.1.12` (port 80) |
| RTSP | `rtsp://192.168.1.12:8554/unicast` — **tanpa auth** |
| Video | H.264, 1280x720 (source), `.env`: `FRIGATE_CAM1_HOST` |
| Frigate key | `cam1` |
| Status Frigate | Priority-capable (punya token `__DETECT_CAM1__`), aktif/nonaktifnya AI diatur lewat `DETECT_CAMERAS` di `.env` |
| Catatan | Sempat dimatikan manual oleh user (2026-08-09), sudah online lagi per 2026-08-10 — Frigate otomatis reconnect sendiri tanpa perlu restart, jadi kalau kamera ini mati/nyala lagi di kemudian hari tidak perlu ada tindakan config apa pun. Cara dapat RTSP URL: aktifkan RTSP lewat web UI kamera, defaultnya di path `/unicast` port `8554`. |

## bardi

| | |
|---|---|
| IP | `192.168.1.27` (statis) |
| Firmware | Dafang-hacks juga (chip **HI3518C**, sama keluarga dengan cam1), tapi dengan **ONVIF diaktifkan** |
| Web UI / SSH / Telnet | Port 80/22/23 **tertutup/unreachable** dari Pi ini — belum ditemukan cara akses langsung, hanya bisa lewat ONVIF |
| ONVIF device service | `http://192.168.1.27:10000/onvif/device_service` |
| ONVIF media service | `http://192.168.1.27:10000/onvif/media_service` (didapat dari `GetCapabilities`) |
| ONVIF auth | WS-Security UsernameToken + PasswordDigest. User: `admin`. Password: lihat `FRIGATE_BARDI_PASS` di `.env`. |
| RTSP | `rtsp://192.168.1.27:554/V_ENC_000` — **butuh auth** (user/password sama seperti ONVIF: `FRIGATE_BARDI_USER` / `FRIGATE_BARDI_PASS`) |
| Video | **HEVC/H.265**, 2304x1296, audio `pcm_alaw` — beda codec dari cam1 (H.264)! |
| Profile / encoder token | `PROFILE_000` / `V_ENC_000` (bisa beda kalau kamera di-reset firmware) |
| Frigate key | `bardi` |
| Status Frigate | Priority-capable, saat ini jadi kamera AI utama (`DETECT_CAMERAS=bardi`) |

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
| IP | `192.168.1.7` (statis), MAC `50:7B:91:79:02:CA` |
| Model | Xiaomi Smart Camera C301 (model code go2rtc/cloud: `mxiang.camera.c301`) |
| Firmware | **Stock Mi Home** — bukan custom firmware seperti cam1/bardi. Tidak ada opsi ONVIF di app kameranya sama sekali. |
| Web UI / SSH / Telnet / RTSP / ONVIF langsung | **Semua tertutup** — port 80, 22, 23, 554, 8554, 10000 semuanya closed/filtered dari Pi ini. Satu-satunya jalan masuk: protokol P2P proprietary Xiaomi (disebut `cs2` di go2rtc). |
| Cara akses | Lewat container `go2rtc` **standalone** (`docker-compose.yml`, image `alexxit/go2rtc:1.9.13`, **BUKAN** go2rtc bawaan Frigate yang versinya 1.9.10 dan belum support Xiaomi — dukungan `cs2` baru ada di go2rtc ≥1.9.13). Wajib `network_mode: host` karena protokol P2P butuh UDP hole-punching yang gagal di belakang NAT bridge Docker biasa (error kalau salah: `read punch: i/o timeout`). |
| Login akun cloud | Mi Home account, **region `sg` (Singapura)** — bukan `cn`/China meski akun terdaftar dari Indonesia (Xiaomi tidak punya server khusus Indonesia, Asia Tenggara termasuk Indonesia dilayani server Singapura). `account_id` dan `did` (device ID) TIDAK ditulis di sini (repo publik) — cek `storage/go2rtc/go2rtc.yaml` (gitignored, berisi sesi login cloud terenkripsi + config stream asli). |
| go2rtc stream URL | `xiaomi://<account_id>:sg@192.168.1.7?did=<did>&model=mxiang.camera.c301` (nilai asli ada di `storage/go2rtc/go2rtc.yaml`, key stream `c301`) |
| go2rtc listen port | API/dashboard `1984`, RTSP restream `8556`, WebRTC `8557` (digeser dari default 8554/8555 supaya tidak bentrok dengan Frigate) |
| Path yang dikonsumsi Frigate | `rtsp://host.docker.internal:8556/c301` (Frigate ada di bridge network Docker biasa, go2rtc host-networked, jadi dijembatani lewat `host.docker.internal` -> `extra_hosts: host-gateway` di `docker-compose.yml`) |
| Video | **HEVC/H.265**, 2304x1296, audio Opus (di-transcode go2rtc dari format asli) |
| Frigate key | `c301` |
| Status Frigate | Non-prioritas (role `record` saja, tanpa `detect`) per keputusan user 2026-08-10 |

**Cara setup ulang / kamera Xiaomi Mi Home lain yang mirip (stock firmware, tanpa ONVIF):**
1. Cek dulu apakah benar tidak ada ONVIF (`CAMERAS.md` prosedur ONVIF di atas) — kalau
   masih ada opsi ONVIF di app kameranya, pakai pola `bardi`, jauh lebih sederhana.
2. Cek model code di [go2rtc Known Xiaomi cameras](https://github.com/AlexxIT/go2rtc/issues/1982)
   apakah didukung (protokol `cs2` jauh lebih stabil dari `tutk`).
3. Buka dashboard go2rtc standalone (`http://<ip-pi>:1984/add.html`), login akun Mi Home
   (coba region `sg` dulu untuk akun Indonesia/Asia Tenggara), cari device, catat
   `account_id`, `region`, `did`.
4. Tambahkan stream baru di `storage/go2rtc/go2rtc.yaml` (format:
   `xiaomi://<account_id>:<region>@<ip>?did=<did>&model=<model_code>`), restart service
   `go2rtc`.
5. Tambahkan kamera baru di `frigate/config.template.yml` dengan path
   `rtsp://host.docker.internal:8556/<nama-stream>`.
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
4. Selalu cek codec video (`ffprobe`) sebelum asumsi H.264 — beberapa kamera di sini
   ternyata HEVC/H.265.
5. Tambahkan entri baru di file ini dengan format yang sama, dan update
   `frigate/config.template.yml` + `.env.example`.
