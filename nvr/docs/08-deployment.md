# 08 — Deployment

Target: STB `umma` (`tarkiman@192.168.1.20`, ZeroTier `172.22.244.135`), Armbian
bookworm, arm64. Deploy **selalu diambil dari branch `nvr-lite`** sampai branch ini
digabung ke `main`.

## Batasan host `umma`

- RAM 1,8 GB, swap zram ~900 MB. Saat Frigate berjalan swap habis — alasan utama proyek
  ini. Ukur RAM setelah deploy.
- Root eMMC hanya 6,5 GB. **Semua data di `/DATA`** (HDD 447 GB). Docker `data-root`
  sudah di `/DATA/docker`.
- Zona waktu host CST (UTC+8). Container memakai `TZ=Asia/Jakarta`; aplikasi tidak
  bergantung pada zona waktu host.
- Ada layanan lain di host ini (`aiplatform`). Jangan menjalankan Frigate dan NVR Lite
  bersamaan.
- Building Go + Node di STB lambat; image dibangun di CI (GitHub Actions → GHCR).

## Image

`nvr/Dockerfile` (multi-stage):

```dockerfile
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build                      # -> /web/dist

FROM golang:1.25-alpine AS server
WORKDIR /src
COPY server/go.* ./
RUN go mod download
COPY server/ ./
COPY --from=web /web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/nvrd ./cmd/nvrd

FROM alpine:3.22
RUN apk add --no-cache ffmpeg tzdata ca-certificates && adduser -D -u 1001 nvr   # samakan dengan uid pemilik /DATA/nvr di host
COPY --from=server /out/nvrd /usr/local/bin/nvrd
USER nvr
ENV TZ=Asia/Jakarta NVR_DATA_DIR=/data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8090/api/v1/healthz || exit 1
ENTRYPOINT ["nvrd"]
```

Versi base image dikunci saat implementasi; di atas hanya ilustrasi. Fase 3 (ONNX/CGO)
akan memakai varian image terpisah (`-detect`).

## CI

Workflow baru `.github/workflows/nvr.yml`:
- Trigger: `push` ke `nvr-lite` dengan perubahan di `nvr/**`, dan `workflow_dispatch`.
- Job: `go vet` + `go test` + `npm run lint && npm run test` → build multi-arch
  (`linux/arm64`, `linux/amd64`) → push
  `ghcr.io/tarkiman/cctv-nvr:nvr-lite` dan `:<sha>`.
- Notifikasi Discord memakai pola yang sama dengan `deploy.yml` (secret
  `DISCORD_WEBHOOK`).

## Compose

`nvr/deploy/docker-compose.yml`:

```yaml
services:
  go2rtc:
    image: alexxit/go2rtc:1.9.14
    container_name: nvr-go2rtc
    restart: unless-stopped
    network_mode: host            # wajib untuk P2P Xiaomi (c301)
    dns: [192.168.1.1]            # DNS publik via UDP di-drop di jaringan ini
    volumes:
      - /DATA/nvr/go2rtc:/config

  nvrd:
    image: ghcr.io/tarkiman/cctv-nvr:${NVR_TAG:-nvr-lite}
    container_name: nvr-nvrd
    restart: unless-stopped
    network_mode: host            # akses go2rtc di 127.0.0.1 tanpa bridge
    env_file: .env
    volumes:
      - /DATA/nvr:/data
    depends_on: [go2rtc]
```

Keduanya `network_mode: host` supaya `nvrd` menjangkau go2rtc di `127.0.0.1` dan port
API go2rtc tidak perlu dibuka ke jaringan.

`go2rtc.yaml` mengikat API ke loopback:

```yaml
api:
  listen: "127.0.0.1:1984"   # dashboard go2rtc hanya lewat SSH tunnel
rtsp:
  listen: "127.0.0.1:8556"
webrtc:
  listen: ":8557"            # perlu terbuka untuk WebRTC dari browser
  candidates:
    - 192.168.1.20:8557
    - 172.22.244.135:8557    # ZeroTier
```

Dashboard go2rtc (untuk login ulang Xiaomi `c301`) dibuka lewat tunnel:
`ssh -L 1984:127.0.0.1:1984 tarkiman@192.168.1.20` lalu `http://localhost:1984/add.html`.

## Deploy pertama

```bash
# di umma
sudo mkdir -p /DATA/nvr/go2rtc && sudo chown -R 1001:1001 /DATA/nvr   # uid tarkiman di umma = 1001
git clone -b nvr-lite git@github.com:tarkiman/cctv.git ~/cctv-nvr
cd ~/cctv-nvr/nvr/deploy
cp .env.example .env                       # isi NVR_ADMIN_USER / NVR_ADMIN_PASSWORD
cp go2rtc.example.yaml /DATA/nvr/go2rtc/go2rtc.yaml
# salin sesi Xiaomi + stream c301 dari /DATA/cctv/storage/go2rtc/go2rtc.yaml
docker compose pull
```

`~/cctv` (deploy Frigate lama, bukan git checkout) dibiarkan apa adanya sebagai jalur
rollback.

## Update

```bash
cd ~/cctv-nvr && git pull --ff-only
cd nvr/deploy && docker compose pull && docker compose up -d
```

Image `:nvr-lite` selalu menunjuk build terakhir branch ini. Untuk rollback ke build
tertentu: `NVR_TAG=<sha> docker compose up -d`.

## Cutover dari Frigate

1. Pastikan image `nvr-lite` sudah lolos CI dan berjalan baik di mesin uji (Pi) dengan
   satu kamera.
2. Catat baseline: `docker stats --no-stream`, `free -m` di `umma`.
3. Hentikan stack lama: `cd ~/cctv && docker compose stop`.
   (Rekaman Frigate lama di `/DATA/cctv/storage` **tidak dihapus**.)
4. Jalankan NVR Lite: `cd ~/cctv-nvr/nvr/deploy && docker compose up -d`.
5. Verifikasi (checklist di bawah) selama minimal 30 menit, lalu cek lagi besok pagi
   (retensi, timelapse tengah malam).
6. Setelah 7 hari stabil: hapus container Frigate lama (`docker compose down` di `~/cctv`),
   arsipkan/hapus `/DATA/cctv/storage/frigate` sesuai keputusan.

### Checklist verifikasi
- [ ] Semua kamera `running` di halaman Status.
- [ ] Segmen baru muncul tiap 10 menit di `/DATA/nvr/recordings/<kamera>/`.
- [ ] Playback segmen terakhir di Chrome Android dan desktop.
- [ ] Live view dari LAN dan dari ZeroTier.
- [ ] `latest.jpg` tiap kamera berumur < 2 menit.
- [ ] CPU total `nvrd`+`go2rtc`+ffmpeg sesuai target di
      [01 — Tujuan](01-tujuan-dan-lingkup.md#kebutuhan-non-fungsional); swap tidak habis.
- [ ] Container `aiplatform` tidak terganggu.

## Rollback

```bash
cd ~/cctv-nvr/nvr/deploy && docker compose stop
cd ~/cctv && docker compose start
```

Port tidak bentrok (NVR Lite 8090; Frigate 5000/8554/8555, panel 8091), tetapi
**go2rtc lama dan baru sama-sama memakai port 8556/8557 di host network** dan sama-sama
membuka sesi P2P ke `c301`, jadi keduanya tidak boleh hidup bersamaan.

## Backup

- Harian 03:00 WIB (worker internal): `VACUUM INTO /DATA/nvr/backup/nvr-<tanggal>.db`,
  simpan 7 terakhir.
- Rekaman tidak di-backup (volume besar, umur pendek). Export penting diunduh manual.
- `go2rtc.yaml` berisi sesi Xiaomi — ikut di-backup bersama `nvr.db`, dengan izin file
  `600`.
