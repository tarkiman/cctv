# 03 — Kamera & stream

Detail cara akses tiap kamera (IP, pola koneksi, cara mendapatkan ulang URL) tetap ada di
[`CAMERAS.md`](../../CAMERAS.md) di root repo. Dokumen ini hanya mencatat **stream mana
dipakai untuk apa** di NVR Lite. Kredensial tidak pernah ditulis di repo.

## Inventaris (per 2026-11-04)

| Kamera | Sumber | Stream utama | Sub-stream | Catatan |
|---|---|---|---|---|
| `cam1` | RTSP `:8554/unicast`, tanpa auth | H.264 1280x720 | — (belum dicek) | IP sering berubah (DHCP) |
| `bardi` | RTSP `:554/V_ENC_000`, butuh auth | HEVC 2304x1296 @15 fps | **H.264 640x360 @15 fps** (`V_ENC_001`) | Kamera prioritas deteksi |
| `c301` | P2P Xiaomi via go2rtc (`xiaomi://...`) | HEVC 848x480 | — | Sesi cloud kedaluwarsa ~16 hari |

Sub-stream `bardi` ditemukan 2026-11-04 dengan `ffprobe` ke `V_ENC_001` (`V_ENC_002`
mengembalikan 404). Ini yang membuat deteksi murah: decode H.264 640x360 jauh lebih
ringan daripada HEVC 2304x1296.

## Pemetaan stream di go2rtc

Setiap kamera punya stream bernama `<kamera>` (utama) dan, bila ada, `<kamera>_sub`.

```yaml
# deploy/go2rtc.example.yaml (rahasia diganti placeholder)
streams:
  cam1:      rtsp://192.168.1.14:8554/unicast
  bardi:     rtsp://admin:${BARDI_PASS}@192.168.1.27:554/V_ENC_000
  bardi_sub: rtsp://admin:${BARDI_PASS}@192.168.1.27:554/V_ENC_001
  c301:      xiaomi://<account_id>:sg@192.168.1.2?did=<did>&model=mxiang.camera.c301
```

Di fase 2, daftar stream dikelola dari UI dan disinkronkan `nvrd` ke go2rtc lewat API.
Sumber dengan skema `xiaomi://` tetap harus dibuat lewat dashboard go2rtc (`/add.html`)
karena butuh login akun Mi Home.

## Penggunaan stream

| Kegunaan | `cam1` | `bardi` | `c301` |
|---|---|---|---|
| Rekaman 24 jam | utama | utama | utama |
| Live grid (tile kecil) | utama | **sub** | utama |
| Live layar penuh | utama | utama (bisa pilih sub) | utama |
| Snapshot / timelapse | utama, keyframe | utama, keyframe | utama, keyframe |
| Deteksi (fase 3) | — | **sub** | — |

## Codec dan browser

| Codec | Chrome/Edge | Safari (iOS/macOS) | Firefox |
|---|---|---|---|
| H.264 | ya | ya | ya |
| HEVC/H.265 | ya, bila perangkat punya hardware decode | ya | tidak |

`bardi` (utama) dan `c301` adalah HEVC. Konsekuensinya:
- **Live:** go2rtc memilih WebRTC/MSE sesuai kemampuan browser. Bila browser tidak bisa
  HEVC, UI menampilkan pesan yang jelas dan menawarkan sub-stream (untuk `bardi`) atau
  snapshot yang diperbarui berkala (untuk `c301`).
- **Playback rekaman:** sama. UI mendeteksi dukungan lewat
  `MediaSource.isTypeSupported('video/mp4; codecs="hvc1"')` dan menampilkan opsi unduh
  bila tidak didukung.
- Server **tidak** men-transcode HEVC ke H.264 — itu justru beban yang ingin dihilangkan.

Rekomendasi pemakaian: Chrome di Android/desktop atau Safari di iPhone.

## Menambah kamera baru

Ikuti urutan pola di `CAMERAS.md` ("Kamera berikutnya"). Setelah URL ketemu, tambahkan
lewat UI (fase 2) atau `go2rtc.yaml` (fase 1), lalu tes koneksi untuk melihat codec dan
resolusi sebelum mengaktifkan rekaman.
