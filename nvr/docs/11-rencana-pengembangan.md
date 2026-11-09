# 11 — Rencana pengembangan

Catatan perencanaan setelah Fase 1–2 selesai dan `nvr-lite` digabung ke `main`
(2026-11-04). Dokumen ini urutan kerja berikutnya; rancangan teknis detailnya tetap di
[02 — Arsitektur](02-arsitektur.md), [04 — Backend](04-backend.md) dan
[05 — Database](05-database.md). Perbarui centang dan tanggal setiap ada kemajuan.

## Posisi saat ini

| Area | Status |
|---|---|
| Rekam 24 jam 3 kamera (copy, segmen 10 menit), retensi, snapshot, timelapse | ✅ jalan di `umma` |
| Live grid/fokus/wall (MSE), playback + timeline, export klip | ✅ |
| Status sistem, SSE, notifikasi Discord (offline, sesi Xiaomi, disk, start) | ✅ |
| Multi-user admin/viewer, perangkat login, PWA + halaman offline | ✅ |
| Kelola kamera dari web (tambah, ganti sumber, tes koneksi, hapus) | ✅ |
| CI → GHCR (`:latest`, `:<sha7>`), deploy dari `main` | ✅ |
| Beban di `umma` | nvrd ~4–9% + go2rtc ~8% CPU, ~105 MB RAM (Frigate dulu 120–215%, ~500 MB) |

## Tahap A — Stabilisasi (sebelum fitur baru)

Tujuan: membuktikan Fase 1–2 stabil tanpa disentuh. Kerjakan dulu, cepat.

- [ ] **Uji 24 jam** (pagi 2026-11-05): rekaman tanpa celah selain restart/deploy,
      timelapse 00:15 WIB terbentuk untuk 3 kamera, backup `nvr-<tanggal>.db` ada,
      retensi berjalan (log `retention`).
- [ ] **Uji reboot `umma`** (`sudo reboot`, butuh password pengguna): "🚀 NVR Lite aktif"
      masuk Discord, 3 kamera `Merekam` lagi tanpa alert offline, PWA menampilkan halaman
      offline selama reboot lalu pulih sendiri.
- [ ] **Tes dari HP**: live grid (sub-stream), live HD `c301`/`bardi` (HEVC) di Chrome
      Android, playback, export klip, install PWA.
- [ ] **Uji retensi nyata**: set retensi 1 hari di satu kamera selama 2 hari, pastikan
      segmen lama terhapus dan timeline ikut menyesuaikan, lalu kembalikan.
- [ ] **DHCP reservation di router** untuk `cam1` (`.14`), `bardi` (`.27`), `c301`
      (`.4`, MAC `50:7B:91:79:02:CA`): IP `cam1`/`c301` sudah berkali-kali berubah.
      (Sekarang bisa diperbaiki lewat web, tapi lebih baik tidak terjadi.)

### Kebersihan keamanan (tindakan pengguna)

Rahasia berikut sempat tercetak di log sesi pengembangan pada 2026-11-04:

- [ ] Rotate **token Cloudflare Tunnel** (dashboard Cloudflare → Tunnels), lalu perbarui
      di `umma`.
- [ ] **Login ulang Xiaomi** `c301` di dashboard go2rtc (sesi baru = token lama mati).
- [ ] Ganti **password `bardi`** di kamera, lalu perbarui lewat **Pengaturan → Kamera →
      bardi → Sumber & koneksi** (stream utama dan sub).
- [ ] Ganti `--webhook-secret` gowa milik channel WhatsApp Claude di Pi (opsional, hanya
      dipakai lokal).
- [ ] Hapus paket GHCR lama `cctv-panel` dan `cctv-timelapse` (GitHub → Packages; token
      CLI tidak punya izin `delete:packages`).
- [ ] Hapus branch `nvr-lite` di GitHub setelah yakin tidak diperlukan (sudah tergabung
      ke `main`).

## Tahap B — Fase 3: deteksi orang

Tujuan: tahu **kapan ada orang** di kamera prioritas (`bardi`), dapat notifikasi
bersnapshot, dan klip di sekitar kejadian tidak ikut terhapus retensi biasa.
Target beban: total < 50% CPU `umma` ([01](01-tujuan-dan-lingkup.md#kebutuhan-non-fungsional)).

### B0. Spike dulu (ukur sebelum membangun)

| # | Pertanyaan | Cara | Lolos bila |
|---|---|---|---|
| S7 ✅ | Berapa ms satu inferensi model orang di A53? | `onnxruntime` arm64 + YOLO nano (input 320) di container uji, 200 frame `bardi_sub` | median < 600 ms, RAM < 150 MB |
| S8 | Motion gate cukup selektif? | Go murni: selisih frame gray 320 px, 2 fps, 24 jam log | malam hari < 1 inferensi/menit tanpa orang; siang tidak melewatkan orang lewat |
| S9 | CGO di image utama vs layanan terpisah? | Bandingkan ukuran image, waktu build CI arm64, dampak crash | lihat keputusan di bawah |
| S10 | Kualitas model di kamera ini | 50 snapshot berlabel manual (orang / bukan: kucing, bayangan, motor) | recall orang ≥ 90%, false positive ≤ 1/hari |

#### Hasil S7 (2026-11-05)

244 frame asli `bardi` (200 sub-stream siang 640×360 + 44 frame malam dari rekaman),
onnxruntime 1.20.1 CPU di `umma`, container dibatasi 500 MB:

| Model (lisensi) | Input | 1 thread | 2 thread | RAM | Catatan |
|---|---|---|---|---|---|
| **YOLOX-nano** (Apache-2.0) | 416 | **562 ms** | **355 ms** | 284 MB | dipilih; muat 0,3 dtk |
| SSD-MobileNet v1 (Apache-2.0) | asli | 874 ms | 568 ms | 360 MB | skor orang asli lebih tinggi (0,97), FP lebih sedikit, tapi muat model 45 dtk |
| YOLOX-tiny (Apache-2.0) | 416 | 1993 ms | 1110 ms | 303 MB | terlalu lambat |
| YOLOX-nano int8 dinamis | 416 | 597 ms | 394 ms | 292 MB | tidak lebih cepat, deteksi hilang total → buang |

Temuan:
- Orang asli (malam, di pagar dan di pintu) terdeteksi YOLOX-nano dengan skor 0,51–0,65.
- False positive: **kucing di atas tembok** (skor sampai 0,70 di YOLOX-tiny, 0,51 di SSD) dan
  benda gelap di bawah tiang (0,4–0,5). Butuh ambang ~0,6, konfirmasi 2 frame berturut-turut,
  dan zona.
- **Sudut pandang `bardi` berubah dalam hitungan detik** (PTZ/patroli?) — perlu dipastikan;
  motion gate berbasis selisih frame tidak bekerja saat kamera bergerak.
- Anggaran CPU: dengan motion gate dan maks 1 inferensi/dtk (1 thread) ≈ 15% CPU total
  `umma` selama ada gerak, ≈ 0 saat sepi. RAM +~300 MB (tersedia 1,2 GB) — muat.
- Kesimpulan S7: latensi **lolos** (562 ms < 600 ms) dengan YOLOX-nano fp32, 1 thread; RAM
  **melebihi target** (284 MB vs 150 MB, angka ini termasuk Python + numpy + Pillow di
  container uji) tetapi masih muat di RAM tersedia. Ukur ulang di implementasi akhir.

**Keputusan yang perlu diambil setelah S7/S9** (catat sebagai D12):
- *Opsi 1 — CGO di `nvrd`* (rencana awal [04](04-backend.md#detector-fase-3)): satu
  binary, build tag `onnx`, image varian `-detect`. Risiko: crash native menjatuhkan
  perekam; build CI arm64 lebih lama.
- *Opsi 2 — layanan `nvr-detect` terpisah* (HTTP, menerima JPEG, mengembalikan kotak).
  `nvrd` tetap tanpa CGO; detektor bisa dipindah ke Pi bila `umma` kurang kuat; bisa
  di-restart sendiri. Sedikit overhead encode JPEG (hanya saat ada gerak).
- **Rekomendasi awal: Opsi 2**, karena isolasi kegagalan lebih penting daripada satu
  binary, dan sejalan dengan rencana "pindahkan inferensi lewat HTTP".

### B1. Motion gate & pipeline (tanpa AI) — dibangun 2026-11-05

Keputusan arsitektur: [D12](10-keputusan.md#d12-deteksi-bisa-diatur-per-kamera-model-orang-di-layanan-terpisah).

- [x] Worker `detector` per kamera dengan `detect_enabled`, saklar di Pengaturan → Kamera,
      dijalankan supervisor yang sama dengan perekam (`internal/detect`).
- [x] ffmpeg: sub-stream (atau stream utama bila tidak ada) → `fps=N,scale=320:180,format=gray`
      → rawvideo ke stdout.
- [x] Motion gate di Go: sel 4×4, selisih > ambang, luas > X‰, konfirmasi 2 frame, selesai
      setelah 10 dtk tenang; parameter global di Pengaturan → Deteksi (berlaku langsung).
- [x] Event `motion` (migrasi `0002_events.sql`), penanda kuning di timeline, daftar
      "Aktivitas gerak", chip "Gerak" di tile live; tanpa notifikasi.
- [x] Ukur CPU (2026-11-05, 2 fps): ffmpeg detektor `bardi_sub` **11% satu core / 48 MB**
      (≈ 2,8% total `umma`) — target < 5% tercapai. Tanpa sub-stream jauh lebih mahal:
      `c301` (HEVC utama 848×480) 23% / 68 MB, `cam1` (H.264 utama 720p25) 30% / 61 MB.
      Konfigurasi uji S8: deteksi hanya `bardi`; `c301` & `cam1` off; global 2 fps, ambang 25,
      luas 15‰.
- [x] Mengubah saklar deteksi kini hanya me-restart worker detektor (grup worker terpisah);
      ganti nama/retensi/timelapse tidak me-restart apa pun.
- [x] **S8** (5 Nov 13:26 → 6 Nov 17:36 WIB, `bardi`, 2 fps / ambang 25 / luas 15‰):
      **1.020 event**; siang 30–70/jam (aktif 1–7 menit/jam), malam 23:00–05:00 **nol event**
      (tidak ada masalah serangga IR). 91% event berupa perubahan gambar total (median luas
      0,68) karena PTZ mengikuti gerakan; durasi median 1 dtk, p90 10 dtk. Sampel rekaman:
      orang beraktivitas di halaman, motor lewat di jalan (PTZ ikut berputar), dan PTZ kembali
      ke posisi awal tanpa objek. Detektor restart berkelompok saat kamera `bardi` sendiri
      putus (RTSP 404), pulih otomatis.
      **Kesimpulan:** motion gate bekerja dan murah; ambang default dipertahankan. Untuk B2:
      (1) orang/motor **di jalan** akan terdeteksi sebagai orang → perlu filter ukuran kotak
      minimum (zona tidak bisa di PTZ) + cooldown notifikasi; (2) beban AI di jam tersibuk
      ≈ 430 dtk aktif × 1,5 frame/dtk ≈ 7% satu core rata-rata per jam — aman.

### B2. Deteksi orang

**Layanan `nvr-detect` sudah dibuat (2026-11-05)** — `nvr/detect/` (Python + onnxruntime +
YOLOX-nano, model di-pin checksum), `POST /detect` (JPEG → kotak `person`/`cat`/`dog`/`car`/
`motorcycle`/`bicycle` + skor), `GET /healthz`. Di compose sebagai profile opsional `detect`
(1 core, 300 MB). Diukur di `umma`: **583 ms/inferensi, 674 ms end-to-end, RAM 53 MB**, siap
3 dtk setelah start (angka RAM 284 MB di S7 ternyata karena skrip uji memuat semua gambar).
Pada frame S7: orang 0,66 dan 0,51, kucing tidak lagi terbaca orang, false positive tersisa
≤ 0,47 → ambang awal **0,5** + konfirmasi 2 frame. Belum tersambung ke `nvrd`.

- [ ] Detector (sesuai D12): input frame warna 320 saat gerak, maks 2 inferensi/detik.
- [ ] Event `person`: mulai saat skor ≥ ambang dua frame berturut-turut, selesai setelah
      30 dtk tanpa orang; simpan snapshot terbaik + kotak.
- [ ] Migrasi `0003_events.sql` (skema sudah dirancang di [05](05-database.md)).
- [ ] Proteksi: segmen yang overlap event ±1 menit → `protected = 1`; retensi event
      memakai `event_retention_days` (kolom sudah ada, default 30).

### B3. UI & notifikasi

- [ ] Penanda event di timeline (titik/strip berwarna) + lompat ke event.
- [ ] Halaman **Event**: daftar per hari dengan thumbnail, filter kamera, tandai sudah
      dilihat, putar klip (pakai player & export yang ada).
- [ ] Notifikasi Discord "🚶 Orang terdeteksi di Teras Rumah" dengan snapshot + kotak,
      cooldown per kamera (default 5 menit), jam tenang opsional.
- [ ] Pengaturan per kamera: deteksi on/off, sensitivitas gerak, ambang skor, cooldown.

### B4. Selesai bila

- [ ] 7 hari berjalan: recall orang ≥ 90% pada sampel, false positive ≤ 1/hari.
- [ ] CPU total `umma` < 50% rata-rata, RAM tersedia > 600 MB, tidak ada swap habis.
- [ ] Rekaman tetap tanpa celah saat detektor crash/restart.

## Tahap D — Multi-lokasi (diputuskan 2026-11-06)

Pengguna punya CCTV di lokasi lain. Keputusan: **semua direkam terpusat di `umma`**; tiap
lokasi punya STB gerbang (go2rtc relay lewat ZeroTier) — lihat
[08 — Gerbang lokasi](08-deployment.md#gerbang-lokasi-go2rtc-relay-mis-pipit).

- [x] Gerbang `pipit` terpasang; `cam1` disimulasikan lewat `pipit` (masih satu jaringan).
- [x] **Lokasi** sebagai grup kamera (1 kamera = 1 lokasi); kamera yang ada → "Rumah" (migrasi `0003_locations`).
- [x] Akses user per lokasi (otomatis mencakup kamera baru) + pengecualian per kamera;
      admin melihat semua ([D13](10-keputusan.md#d13-lokasi-sebagai-grup-kamera-dan-dasar-hak-akses)). Ditegakkan di semua jalur: daftar, live, snapshot, rekaman, export,
      timelapse, event, SSE, Status.
- [x] Tab per lokasi di Live; label lokasi di Rekaman/Status.
- [x] Notifikasi tingkat lokasi: "📡 Lokasi X tidak terjangkau" bila semua (≥ 2) kamera lokasi
      mati ≥ 5 menit, menggantikan alert per kamera; pesan pulih menyebut kamera yang masih
      bermasalah.
- [ ] Webhook notifikasi per lokasi (opsional).
- [ ] Saat `pipit` dipindah: cek ZeroTier *direct*, upload internet, lalu tambah kamera lokasi itu.
- [ ] Nanti (opsional): buffer lokal di gerbang untuk mengisi celah saat internet lokasi putus.

### Cari kamera (2026-11-06)

- [x] Tombol **Cari kamera** (Pengaturan → Kamera): ONVIF WS-Discovery + sapuan port RTSP di LAN
      `umma`, digabung per MAC; kategori baru / terdaftar / IP berubah / gerbang / diabaikan.
- [x] Tambah dengan isian otomatis: ONVIF `GetStreamUri` (uji `bardi`: utama 2304×1296 +
      sub 640×360 langsung terisi) atau tebak path RTSP 13 pola merek (DESCRIBE + Digest).
- [x] MAC kamera dipelajari; IP berubah → **Perbarui sumber** satu klik (password lama dipakai).
- [ ] Pindai lokasi lain lewat agen di gerbang (pipit).
- [ ] Notifikasi offline menyarankan IP baru bila kamera ditemukan di alamat lain.

## Tahap C — Backlog (belum dijadwalkan)

Urut kira-kira dari manfaat terbesar:

1. **Zona deteksi** (poligon di atas snapshot) untuk mengabaikan jalan/pohon.
2. **Deteksi `c301`/`cam1`**: `cam1` (H.264 720p) bisa langsung; `c301` HEVC tanpa
   sub-stream → mahal, pertimbangkan deteksi gerak bawaan kamera atau lewati.
3. **Live dari luar rumah lebih lancar**: WebRTC lewat TURN/Cloudflare Calls, atau
   tetap MSE (sekarang 1–3 dtk delay, cukup).
4. ~~**Audio** di rekaman & live~~ — selesai 2026-11-09 (D14). Lanjutan: **bicara balik**
   (two-way audio) ke `c301` lewat WebRTC + mikrofon browser.
5. **Integrasi Home Assistant / MQTT** (event & status kamera).
6. **2FA (TOTP)** untuk akun admin.
7. **Urutkan kamera** (drag & drop di Pengaturan; kolom `sort_order` sudah ada).
8. **Export lintas hari / beberapa kamera sekaligus**.
9. **Pengingat sesi Xiaomi proaktif**: sesi `c301` cenderung basi ~16 hari; kirim
   pengingat di hari ke-14 alih-alih menunggu mati.

### Utang teknis kecil

- [ ] Tes koneksi: saran "tambahkan sub-stream" tetap muncul untuk `bardi` walau
      sub-stream sudah ada — sembunyikan bila kamera punya sub-stream.
- [ ] Naikkan versi GitHub Actions (peringatan Node 20 deprecated) dan siapkan migrasi
      `ubuntu-latest` → Ubuntu 26.
- [ ] `go test -race` tidak bisa di kernel Pi 5 (VMA 47-bit) — hanya di CI; biarkan.
- [ ] Tes frontend (vitest) belum ada; minimal untuk `utils/format.ts` dan store kamera.
- [ ] `09-roadmap.md` masih memuat target Fase 0–1 lama; ringkas setelah Fase 3 jalan.

## Cara kerja yang disepakati

- Pengembangan langsung di `main` (commit kecil, pesan konvensional `feat(nvr): …`);
  fitur besar boleh di branch + PR.
- Setiap push ke `main` → CI → image `:latest`; deploy manual di `umma` (perintah di
  [08](08-deployment.md#update)). Runner ARM GitHub kadang antre lama → jalur cadangan
  build di Pi.
- Setelah deploy: cek halaman Status dan notifikasi "NVR Lite aktif".
- Jangan pernah mencetak `go2rtc.yaml` / `/api/config` / `.env` ke log atau chat — berisi
  sesi Xiaomi, password kamera, webhook.
- Fitur baru wajib: dokumen (`06-api`, `09-roadmap`, file ini) diperbarui di commit yang
  sama.
