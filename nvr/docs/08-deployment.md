# 08 — Deployment

NVR Lite berjalan di STB `umma` sejak 2026-11-04, di port **:8091**, dan diakses lewat
**https://cctv.tarkiman.com** (Cloudflare Tunnel → `localhost:8091`). Stack Frigate lama
sudah dihapus seluruhnya (lihat [D11](10-keputusan.md#d11-nvr-lite-digabung-ke-main-frigate-dihapus)).

## Host `umma`

| | |
|---|---|
| Perangkat | ZTE B860H, Armbian bookworm, arm64, 4× Cortex-A53, RAM 1,8 GB (+ zram) |
| Alamat | `tarkiman@192.168.1.20` (LAN), `172.22.244.135` (ZeroTier) |
| User | `tarkiman` = uid **1001** (bukan 1000); `sudo` butuh password |
| Disk | root eMMC 6,5 GB; **semua data di `/DATA`** (HDD 447 GB), Docker `data-root` di `/DATA/docker` |
| Zona waktu host | CST (UTC+8). Container memakai `TZ=Asia/Jakarta`; aplikasi menyimpan UTC |
| Layanan lain | `aiplatform-*` (saat ini berhenti); jangan dihapus |

| Path | Isi |
|---|---|
| `~/cctv-nvr` | clone HTTPS repo ini, branch `main` |
| `~/cctv-nvr/nvr/deploy/.env` | konfigurasi (izin `600`; berisi password admin & webhook) |
| `/DATA/cctv/nvr/` | data: `nvr.db`, `recordings/`, `snapshots/`, `timelapse/`, `backup/`, `exports/` (sementara) |
| `/DATA/cctv/nvr/go2rtc/go2rtc.yaml` | sumber stream + sesi Xiaomi `c301` — **rahasia, jangan dicetak/di-commit** |

## Image & CI

`nvr/Dockerfile` (multi-stage): `node:22-alpine` membangun web → `golang:1.26-alpine`
membangun `nvrd` (web di-embed) → runtime `alpine:3.22` + `ffmpeg`, user uid 1001.
Healthcheck mengikuti port `NVR_LISTEN`.

Workflow `.github/workflows/nvr.yml`:
- Trigger: `push` ke `main` yang mengubah `nvr/**`, pull request, `workflow_dispatch`.
- Job `test`: `npm run build` (termasuk `vue-tsc`), `gofmt`, `go vet`, `go test -race`.
- Job `image`: build **arm64** di runner native `ubuntu-24.04-arm`, push
  `ghcr.io/tarkiman/cctv-nvr:latest` dan `:<sha7>`, serta `cctv-nvr-detect` dengan tag yang sama (paket publik, `umma` menarik tanpa
  login). Versi di halaman Status = sha7.
- Job `notify`: hasil build ke Discord bila secret `DISCORD_WEBHOOK` ada.

Runner ARM GitHub kadang mengantre > 30 menit. Jalur cadangan dari Pi (sama-sama arm64):

```bash
cd ~/repository/cctv/nvr
docker build --build-arg VERSION=$(git rev-parse --short HEAD) -t ghcr.io/tarkiman/cctv-nvr:latest .
docker save ghcr.io/tarkiman/cctv-nvr:latest | gzip -1 | ssh umma 'gunzip | docker load'
ssh umma 'cd ~/cctv-nvr/nvr/deploy && docker compose up -d nvrd'
```

## Compose

`nvr/deploy/docker-compose.yml` — dua service, keduanya `network_mode: host`:

| Service | Container | Catatan |
|---|---|---|
| `go2rtc` | `nvr-go2rtc` | `alexxit/go2rtc:1.9.14`; host network wajib untuk P2P Xiaomi; `dns: 192.168.1.1` (DNS publik via UDP di-drop di jaringan ini) |
| `nvrd` | `nvr-nvrd` | `ghcr.io/tarkiman/cctv-nvr:${NVR_TAG:-latest}`, `env_file: .env`, data di `${NVR_DATA}` |
| `detect` | `nvr-detect` | **opsional** (profile `detect`): model deteksi orang di `127.0.0.1:8092`, maks 1 core (`NVR_DETECT_CPUS`) dan 300 MB. Nyalakan dengan `COMPOSE_PROFILES=detect` di `.env` |

`go2rtc.yaml` mengikat API (`127.0.0.1:1984`) dan RTSP (`127.0.0.1:8556`) ke loopback;
hanya WebRTC (`:8557`) yang terbuka. Dashboard go2rtc (login ulang Xiaomi) lewat tunnel:
`ssh -L 1984:127.0.0.1:1984 tarkiman@192.168.1.20` → `http://localhost:1984/add.html`.

## Gerbang lokasi (go2rtc relay, mis. `pipit`)

Kamera di lokasi lain tetap **direkam terpusat di `umma`**. Di lokasi itu dipasang satu STB
gerbang yang menjalankan go2rtc (`nvr/deploy/gateway/`): ia menarik kamera di LAN lokasi
tersebut (RTSP, Xiaomi P2P) dan menyajikannya ulang lewat RTSP **berpassword** ke `umma`
melalui ZeroTier. Keuntungan dibanding routing subnet: tidak masalah bila subnet LAN sama
(`192.168.1.x`), kamera Xiaomi tetap jalan, dan hanya satu stream per kamera yang keluar
dari lokasi.

```bash
# di gerbang (Docker terpasang, sudah join jaringan ZeroTier)
mkdir -p ~/nvr-gateway/config && cd ~/nvr-gateway
cp <repo>/nvr/deploy/gateway/docker-compose.yml .
cp <repo>/nvr/deploy/gateway/go2rtc.example.yaml config/go2rtc.yaml
chmod 700 config && chmod 600 config/go2rtc.yaml   # isi streams + ganti password RTSP
docker compose up -d
```

Di NVR: **Pengaturan → Kamera → Tambah kamera**, URL
`rtsp://<IP-ZeroTier-gerbang>:8554/<nama-stream>`, username `nvr` + password RTSP gerbang,
lalu **Tes koneksi**.

| Gerbang | Lokasi | ZeroTier | Catatan |
|---|---|---|---|
| `pipit` (`tarkiman@192.168.1.25`, ZTE B860H, WiFi) | sementara masih di rumah (uji 2026-11-06) | `172.22.195.55` | `~/nvr-gateway`; root eMMC 6,5 GB hampir penuh (474 MB sisa) — jangan merekam di sini, bersihkan image/log yang tidak perlu |

Hasil uji 2026-11-06 (`cam1` lewat `pipit`): ZeroTier langsung (RTT min 4 ms; WiFi
membuat rata-rata ~40 ms), upload **~400 kbps** untuk H.264 720p, go2rtc gerbang 2,5% CPU /
6,5 MB. Tanpa password ditolak `401`.

Hal yang dicek saat gerbang pindah lokasi: jalur ZeroTier tetap *direct* (bukan relay),
upload internet lokasi ≥ 1,5× total bitrate kamera, dan notifikasi bila gerbang tidak
terjangkau. Rekaman lokasi itu terputus selama internetnya putus (kelemahan pola terpusat).

## Deploy pertama (host baru)

```bash
mkdir -p /DATA/cctv/nvr/go2rtc            # milik uid 1001
git clone https://github.com/tarkiman/cctv.git ~/cctv-nvr
cd ~/cctv-nvr/nvr/deploy
cp .env.example .env && chmod 600 .env    # isi NVR_ADMIN_*, NVR_DATA, NVR_LISTEN, webhook
cp go2rtc.example.yaml /DATA/cctv/nvr/go2rtc/go2rtc.yaml
docker compose pull && docker compose up -d
```

Kamera RTSP ditambahkan dari web (**Pengaturan → Kamera → Tambah kamera**). Kamera
Xiaomi ditambahkan dari dashboard go2rtc, lalu **Sinkronkan** di Pengaturan.

## Update

```bash
# setelah CI hijau untuk commit terbaru di main
ssh umma 'cd ~/cctv-nvr && git pull --ff-only && cd nvr/deploy && docker compose pull nvrd && docker compose up -d nvrd'
```

Notifikasi "🚀 NVR Lite aktif" di Discord menandai nvrd menyala lagi.

## Rollback

```bash
cd ~/cctv-nvr/nvr/deploy && NVR_TAG=<sha7-sebelumnya> docker compose up -d nvrd
```

Migrasi database hanya maju; sebelum rollback melewati migrasi baru, pulihkan
`backup/nvr-<tanggal>.db` (dibuat harian, 7 terakhir disimpan).

## Konfigurasi (`.env`)

| Variabel | Isi di `umma` / default |
|---|---|
| `NVR_LISTEN` | `:8091` (default `:8090`) |
| `NVR_DATA` | `/DATA/cctv/nvr` (dipakai compose) |
| `NVR_TAG` | `latest` (dipakai compose) |
| `NVR_ADMIN_USER` / `NVR_ADMIN_PASSWORD` | admin pertama, hanya dipakai bila belum ada user |
| `NVR_GO2RTC_URL` / `NVR_GO2RTC_RTSP` | `http://127.0.0.1:1984` / `rtsp://127.0.0.1:8556` |
| `NVR_TZ` | `Asia/Jakarta` |
| `NVR_SEGMENT_SECONDS` / `NVR_SNAPSHOT_SECONDS` | `600` / `60` |
| `NVR_ALLOWED_ORIGINS` | `cctv.tarkiman.com` (origin WebSocket lewat proxy) |
| `NVR_SECURE_COOKIE` | `false` (otomatis `Secure` lewat HTTPS) |
| `NVR_NOTIFY_WEBHOOK` | URL webhook Discord (lihat di bawah) |
| `NVR_NOTIFY_OFFLINE_MINUTES` | `5` |
| `NVR_PUBLIC_URL` | `https://cctv.tarkiman.com` (tautan di notifikasi) |

## Notifikasi

`NVR_NOTIFY_WEBHOOK` menerima satu atau lebih URL (pisahkan koma): Discord webhook →
embed; gowa `…/send/message?phone=62…` → pesan WhatsApp (user:password di URL jadi basic
auth); URL lain → JSON generik.

Yang dikirim:
- Kamera tidak merekam ≥ 5 menit — satu alert per kejadian dengan penyebab, kolom status,
  dan snapshot terakhir; bila penyebabnya sesi Xiaomi, judulnya "🔑 … sesi Xiaomi perlu
  login ulang" plus langkah perbaikan. Satu pesan lagi saat pulih.
- Disk bebas < setengah batas retensi (default 10% → alert < 5%); pulih saat ≥ batas.
- "🚀 NVR Lite aktif" setiap nvrd start.

Tombol **Kirim pesan tes** ada di halaman Status (admin). Pratinjau format ke webhook asli:
`NVR_PREVIEW_WEBHOOK=<url> go test -run TestPreview ./internal/notify`.

## Akses lewat Cloudflare Tunnel

- `cloudflared` di `umma` meneruskan `cctv.tarkiman.com` ke `localhost:8091`.
- Cookie sesi otomatis `Secure` bila `X-Forwarded-Proto: https`, jadi login juga bisa lewat
  HTTP di LAN (`http://192.168.1.20:8091`).
- Rate limit login memakai `CF-Connecting-IP`, hanya bila peer-nya loopback (cloudflared
  lokal).
- Live view (WebSocket MSE) melewati tunnel; WebRTC dari luar LAN belum dikonfigurasi.
- Cloudflare meng-cache `sw.js` di edge dan menimpa `Cache-Control` browser menjadi 4 jam;
  aman karena service worker didaftarkan dengan `updateViaCache: 'none'`.
- Script Cloudflare Web Analytics diblokir CSP (tidak berpengaruh); matikan di dashboard
  Cloudflare bila ingin konsol bersih.

## Backup

- Harian (worker internal): `VACUUM INTO /DATA/cctv/nvr/backup/nvr-<tanggal>.db`, 7 terakhir.
- Rekaman tidak di-backup (volume besar, umur pendek). Klip penting diunduh lewat
  **Export klip**.
- `go2rtc.yaml` (sumber + sesi Xiaomi) tidak ikut backup otomatis; salin manual bila
  memindahkan host.

## Checklist verifikasi setelah perubahan besar

- [ ] Semua kamera `Merekam` di halaman Status; segmen terakhir < 10 menit.
- [ ] Live grid & satu kamera jalan dari HP (Chrome Android) dan desktop.
- [ ] Playback & export klip hari ini.
- [ ] `docker stats --no-stream`: nvrd + go2rtc < 20% CPU, < 200 MB RAM.
- [ ] Notifikasi "NVR Lite aktif" diterima.
