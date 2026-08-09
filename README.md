# Portal CCTV Rumah

Fondasi portal CCTV di atas [Frigate](https://frigate.video) untuk kamera-kamera di
rumah — campuran kamera Xiaomi Dafang dengan custom firmware
[Xiaomi-Dafang-Hacks](https://github.com/EliasKotlyar/Xiaomi-Dafang-Hacks) dan kamera
Xiaomi Mi Home stock (tanpa hack) yang diakses lewat protokol cloud P2P. Lihat
[CAMERAS.md](CAMERAS.md) untuk detail tiap kamera.

Stack: **Frigate** (live view, recording, AI object detection) + **Mosquitto** (MQTT,
bus event) + **go2rtc standalone** (jembatan untuk kamera Xiaomi Mi Home stock yang
tidak bisa RTSP/ONVIF langsung) + **timelapse** (snapshot berkala + compile video
harian).

## Prasyarat di tiap kamera

Kamera-kamera di rumah ini beda merek/tipe firmware/pola akses — **lihat
[CAMERAS.md](CAMERAS.md)** untuk detail akses lengkap tiap kamera yang sudah terdaftar
(IP, kredensial, path RTSP, codec) dan prosedur menambah kamera baru. Tiga pola yang
sudah ditemui: RTSP unicast langsung (`cam1`), ONVIF (`bardi`), dan protokol P2P
Xiaomi lewat go2rtc untuk kamera stock tanpa hack (`c301`, lihat juga bagian "Kamera
Xiaomi Mi Home stock" di bawah). Jangan asumsikan satu pola berlaku untuk semua kamera.

## Setup

1. Salin `.env.example` ke `.env` dan isi IP tiap kamera + daftar nama kamera:
   ```
   cp .env.example .env
   ```
2. Sesuaikan `frigate/config.template.yml` (ini **template**, bukan config final yang
   dibaca Frigate — lihat "Memilih kamera mana yang pakai AI detection" di bawah).
   Sudah ada 3 kamera nyata terkonfigurasi sebagai contoh (lihat [CAMERAS.md](CAMERAS.md)
   untuk detail masing-masing):
   - `cam1` dan `bardi`: kamera **prioritas-capable** (punya token
     `enabled: __DETECT_<NAMA>__` di blok `detect`, settingnya hasil benchmark —
     lihat "Catatan performa" di bawah).
   - `c301`: kamera **non-prioritas** (live view + record + timelapse saja, tanpa role
     `detect`), diakses lewat go2rtc karena firmware stock tanpa RTSP/ONVIF (lihat
     "Kamera Xiaomi Mi Home stock" di bawah).
   - Kamera `cam2`..`cam5` ada di komentar sebagai template non-prioritas tambahan —
     uncomment sesuai kebutuhan. Kalau kamera baru itu mungkin butuh AI juga, salin
     blok `cam1`/`bardi` (termasuk token `__DETECT_<NAMA>__`-nya), bukan template
     non-prioritas.
   - Opsional: ganti key `cam1`, `cam2`, dst. jadi nama yang lebih jelas (mis. `teras`,
     `garasi`). Kalau diganti, pastikan `TIMELAPSE_CAMERAS` dan `DETECT_CAMERAS` di
     `.env` ikut disesuaikan supaya sama persis dengan key kamera di sini.
3. Jalankan:
   ```
   docker compose up -d --build
   ```
4. Buka Frigate web UI di `http://<ip-raspberry-pi>:5000` — ini jadi tampilan live view
   utama untuk sekarang.

## Memilih kamera mana yang pakai AI detection

`frigate/config.template.yml` **bukan** file yang langsung dibaca Frigate. Setiap kali
container `frigate` start, ia menjalankan `render_config.py` yang membaca template ini,
mengganti tiap token `enabled: __DETECT_<NAMA>__` jadi `true`/`false` sesuai daftar
kamera di variabel `DETECT_CAMERAS` (`.env`), lalu menulis hasilnya ke
`storage/frigate-config/config.yml` (file inilah yang sebenarnya dipakai Frigate).

Untuk ganti kamera mana yang aktif AI detection-nya, **cukup edit satu baris** di `.env`:

```
DETECT_CAMERAS=bardi          # hanya bardi
DETECT_CAMERAS=bardi,cam1     # bardi dan cam1 dua-duanya
```

lalu terapkan:

```
docker compose up -d --force-recreate frigate
```

Tidak perlu edit YAML manual. Ini hanya berlaku untuk kamera yang di
`frigate/config.template.yml` sudah punya token `__DETECT_<NAMA>__` di blok
`detect.enabled` (kamera "prioritas-capable") — kamera non-prioritas permanen (tanpa
role `detect` sama sekali) tidak bisa diaktifkan lewat cara ini, harus edit YAML
langsung untuk menambah role `detect`-nya dulu.

Frigate juga punya toggle AI detection lewat UI/MQTT sendiri, tapi **tidak persisten**
— balik lagi ke nilai `DETECT_CAMERAS` setiap kali container di-restart (keterbatasan
Frigate: [diskusi terkait](https://github.com/blakeblackshear/frigate/discussions/21656)).
Jadi `DETECT_CAMERAS` di `.env` adalah satu-satunya sumber kebenaran yang persisten.

## Kamera Xiaomi Mi Home stock (lewat go2rtc)

Kamera seperti `c301` tidak punya RTSP/ONVIF sama sekali (firmware stock, bukan hasil
custom firmware) — satu-satunya jalan masuk adalah protokol P2P proprietary Xiaomi
(`cs2`), dijembatani lewat service `go2rtc` standalone di `docker-compose.yml`
(**bukan** go2rtc bawaan Frigate — versinya 1.9.10, belum support Xiaomi; dukungan
`cs2` baru ada di go2rtc ≥1.9.13).

Setup kamera baru jenis ini:
1. Buka `http://<ip-raspberry-pi>:1984/add.html` (dashboard go2rtc standalone).
2. Login akun Mi Home. Region yang tersedia: `de`, `i2` (India), `ru`, `sg`
   (Singapura), `us`, atau default/China — **tidak ada "Indonesia"**, Xiaomi tidak
   punya server per negara. Untuk akun Indonesia, coba region **`sg`** dulu (server
   yang melayani Asia Tenggara).
3. Cari kamera di daftar device, catat `account_id`, `region`, `did`.
4. Tambahkan stream-nya ke `storage/go2rtc/go2rtc.yaml` (lihat
   `go2rtc/go2rtc.yaml.example` untuk formatnya), lalu
   `docker compose up -d --force-recreate go2rtc`.
5. Tambahkan kamera di `frigate/config.template.yml` dengan path
   `rtsp://host.docker.internal:8556/<nama-stream>` (lihat blok `c301` sebagai
   contoh) — bukan lewat variabel `FRIGATE_*_HOST` di `.env` seperti kamera lain.

Catatan teknis penting kalau mengubah setup ini:
- Service `go2rtc` **wajib** `network_mode: host` — protokol P2P butuh UDP
  hole-punching yang gagal (`read punch: i/o timeout`) di belakang NAT bridge Docker
  biasa.
- Karena host networking, port listen go2rtc digeser manual ke `8556` (RTSP) dan
  `8557` (WebRTC) di `go2rtc.yaml` supaya tidak bentrok dengan port Frigate
  (8554/8555). Dashboard API tetap di `1984`.
- Frigate (masih di bridge network Docker biasa) menjangkau go2rtc yang host-networked
  lewat `host.docker.internal` — perlu `extra_hosts: host-gateway` di service
  `frigate` (sudah ada di `docker-compose.yml`).
- Video tetap streaming lokal sepenuhnya, tapi **setiap sesi baru butuh koneksi
  internet sesaat ke server Xiaomi** untuk fetch kunci enkripsi — bukan air-gapped
  murni. Kalau internet rumah mati, stream kamera jenis ini ikut terputus meski
  kameranya sendiri di LAN yang sama.

## Verifikasi

- `docker compose ps` — semua service (`mosquitto`, `frigate`, `go2rtc`, `timelapse`)
  harus `running`.
- Frigate UI menampilkan live feed semua kamera (bukan "camera is offline").
- Untuk kamera lewat go2rtc (mis. `c301`): `curl http://localhost:1984/api/streams`
  harus menunjukkan `producers` terisi (bukan kosong/error) untuk stream itu.
- `docker compose logs frigate` — tidak ada error decode RTSP berulang.
- Setelah beberapa menit, cek `storage/timelapse/<nama-kamera>/<tanggal>/` mulai terisi
  file JPEG sesuai `TIMELAPSE_INTERVAL_SECONDS`.
- Compile timelapse jalan otomatis tiap hari jam 00:05 (cron di dalam container
  `timelapse`), atau trigger manual untuk tes:
  ```
  docker compose exec timelapse python3 compile_timelapse.py
  ```
  lalu cek `storage/timelapse/<nama-kamera>/videos/<tanggal>.mp4` bisa diputar.
- Pantau storage NVMe (`df -h`) beberapa hari pertama — kalau cepat penuh, kecilkan
  retensi di `frigate/config.template.yml` (`record.alerts.retain.days` /
  `record.detections.retain.days`) atau resolusi `detect`.

## Catatan performa (Raspberry Pi 5, tanpa Coral TPU, host shared)

**Penting: Pi ini tidak didedikasikan untuk CCTV** — host yang sama juga menjalankan
banyak service Docker lain (ticketing, platform lain, AI platform, meet/LiveKit, dll).
Load average host sudah ~4-6 di CPU 4-core dan RAM ketat (swap hampir penuh) bahkan
sebelum CCTV ditambah. Ini bukan cuma soal performa kamera — detection yang terlalu
berat juga bisa memperlambat layanan lain di Pi ini.

Hasil benchmark di `cam1` (1 kamera, CPU detector `type: cpu`):

| Setting `detect`         | `skipped_fps` (dari `camera_fps` ~5.1) | CPU detector |
|---------------------------|------------------------------------------|--------------|
| 1280x720, fps 5 (awal)     | 2.4 (~47% frame di-skip)                 | ~220-230%    |
| 640x360, fps 5 (final)      | 1.2 (~24% frame di-skip)                 | ~220%        |

Turun resolusi `detect` cukup membantu (frame yang di-skip berkurang signifikan) tapi
CPU detector tetap tinggi untuk 1 kamera saja — karena itu strategi yang dipakai:
**AI detection hanya diaktifkan di kamera yang terdaftar di `DETECT_CAMERAS`** (lihat
"Memilih kamera mana yang pakai AI detection" di atas), kamera non-prioritas permanen
tidak punya role `detect` di ffmpeg sama sekali supaya tidak ada beban decode+inference
tambahan. Cek `docker stats` dan `docker compose logs frigate` / layanan lain di Pi ini
setiap kali menambah kamera ke `DETECT_CAMERAS`, dan turunkan `detect.fps` (mis. ke 3)
kalau mulai terasa berat.

Kamera `bardi` (HEVC/H.265, 2304x1296) ternyata performanya lebih baik dari `cam1`
(H.264, 1280x720) meski resolusi sumbernya jauh lebih besar — `skipped_fps` 0.0 vs 1.2
dengan setting `detect` yang sama (640x360/5fps). Jangan asumsikan resolusi source
menentukan beban CPU; selalu benchmark tiap kamera baru lewat `curl
http://localhost:5000/api/stats` sebelum menganggap suatu setting "aman".

- Hardware video decode (hwaccel) **belum diaktifkan by default**: Raspberry Pi 5 hanya
  punya hardware decoder untuk HEVC/H.265, bukan H.264 seperti Pi 4, dan dukungan
  `preset-rpi-64-h264` di Frigate masih belum stabil per Agustus 2026
  ([diskusi terkait](https://github.com/blakeblackshear/frigate/discussions/16411)).
  Beban decode di setup ini hanya kena di stream `detect` (resolusi rendah) karena
  stream `record` cuma di-remux tanpa decode, jadi software decode saja seharusnya
  cukup ringan. Kalau nanti ingin eksperimen hwaccel, tambahkan `hwaccel_args:
  preset-rpi-64-h264` di ffmpeg tiap kamera, aktifkan mount `/dev/dri` di
  `docker-compose.yml`, dan cek log untuk error.
- Storage NVMe terbatas — recording default motion-triggered (bukan continuous 24/7)
  dengan retensi 7 hari, sesuaikan lagi kalau perlu.
- **Insiden 2026-08-10:** Pi ini sempat reboot 5x beruntun dalam ~12 menit (02:08-02:20),
  root cause belum pasti (tidak ada log kernel panic/OOM tersimpan dari sebelum crash —
  konsisten dengan crash keras/power issue, bukan graceful shutdown). Sempat juga CPU
  saturation 96.7% tak lama setelah stabil. Ada `rpi-watchdog` (systemd service,
  `/etc/rpi-watchdog/`) yang mengirim alert ke Discord untuk kejadian seperti ini —
  cek log-nya (`journalctl -u rpi-watchdog`) kalau CCTV atau layanan lain di Pi ini
  tiba-tiba terasa tidak stabil. Belum jelas apakah penambahan service `go2rtc`
  (host networking, berjalan >1 jam sebelum insiden) berkontribusi atau cuma kebetulan
  bersamaan dengan beban dari service lain di Pi ini.

## Belum dikerjakan (fase berikutnya)

- Halaman portal web custom yang menyatukan live view Frigate + galeri timelapse dalam
  satu UI.
- Worker AI/OpenCV yang subscribe ke event MQTT Frigate atau memproses snapshot
  timelapse.
- Review reverse proxy HTTPS + autentikasi untuk akses dari luar rumah — **catatan:**
  domain `cctv.tarkiman.com` sepertinya sudah di-routing (kemungkinan lewat Traefik
  yang juga jalan di Pi ini untuk stack lain) ke Frigate UI di sini, ketahuan dari log
  akses saat development. Perlu dicek eksplisit apakah itu memang setup yang disengaja,
  request/response-nya sudah lewat auth yang benar, dan tidak membocorkan live feed ke
  publik tanpa proteksi.

## Keamanan

- MQTT broker saat ini `allow_anonymous true` — cukup untuk LAN rumah, tapi wajib
  diperketat (username/password atau TLS) sebelum expose ke luar jaringan lokal.
- RTSP kamera Dafang-hacks umumnya tanpa autentikasi di LAN (kecuali kamera ONVIF
  seperti `bardi` yang butuh auth) — jaga agar jaringan kamera tidak bisa diakses dari
  luar rumah.
- Kredensial kamera (password ONVIF/RTSP, dll) hanya disimpan di `.env` (gitignored) —
  `CAMERAS.md` sengaja tidak menyimpan password asli meski dokumentasi cara aksesnya
  lengkap di sana.
- Sesi login akun Mi Home (untuk kamera lewat go2rtc, mis. `c301`) tersimpan terenkripsi
  di `storage/go2rtc/go2rtc.yaml` (gitignored) — bukan password plaintext, tapi tetap
  jangan commit atau share file itu.
- Dashboard go2rtc (port 1984) tidak ada autentikasi bawaan — siapa pun di LAN yang
  sama bisa akses/re-konfigurasi stream kamera lewat situ. Cukup aman untuk LAN rumah,
  tapi jangan expose port ini ke luar rumah.
