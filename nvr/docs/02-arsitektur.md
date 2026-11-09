# 02 — Arsitektur

## Gambaran besar

```
                 ┌──────────────────────── host: umma ────────────────────────────┐
 Kamera          │                                                                │
 cam1  ──RTSP──┐ │  ┌──────────┐  RTSP restream (127.0.0.1:8556)  ┌─────────────┐  │
 bardi ──RTSP──┼─┼─▶│  go2rtc  │─────────────────────────────────▶│    nvrd     │  │
 c301  ──P2P───┘ │  │ (hub)    │◀──── API :1984 (sinkron stream) ─│  (Go, chi)  │  │
                 │  └────┬─────┘                                  │             │  │
                 │       │ WebRTC / MSE (via proxy nvrd)          │ supervisor: │  │
                 │       │                                        │  recorder   │──┼─▶ /DATA/nvr/recordings
                 │       │                                        │  snapshot   │──┼─▶ /DATA/nvr/snapshots, timelapse
                 │       │                                        │  detector   │  │
                 │       │                                        │  retention  │  │
                 │       │                                        │  health     │  │
                 │       │                                        │ SQLite (WAL)│──┼─▶ /DATA/nvr/nvr.db
                 │       │                                        └──────┬──────┘  │
                 └───────┼───────────────────────────────────────────────┼─────────┘
                         │                                               │ HTTP :8090
                         └───────────────▶ Browser / PWA (Vue) ◀─────────┘
```

Hanya ada **dua proses jangka panjang**: `go2rtc` dan `nvrd`. Semua pekerjaan video
dilakukan oleh subprocess `ffmpeg` yang di-spawn dan diawasi `nvrd`.

## Komponen

### go2rtc — hub stream
- Satu-satunya yang membuka koneksi ke kamera. Kamera murah (Dafang, Xiaomi) sering tidak
  kuat melayani banyak koneksi sekaligus; dengan hub, kamera cukup dilayani satu koneksi
  dan go2rtc yang membagikannya (live, rekaman, snapshot, deteksi).
- Wajib untuk `c301` (protokol P2P Xiaomi `cs2`), jadi tidak menambah dependensi baru.
- `network_mode: host` (P2P butuh UDP hole-punching) dan DNS dipaksa ke router
  (`192.168.1.1`) — pelajaran dari insiden `c301` 2026-09-27.
- `nvrd` menyinkronkan daftar stream ke go2rtc lewat API `PUT /api/streams` saat start
  dan saat kamera diubah. Rahasia login Xiaomi tetap di `go2rtc.yaml` (bagian `xiaomi:`),
  tidak disalin ke database.

### nvrd — server Go
Satu binary berisi: HTTP API (chi), file frontend yang di-embed, dan supervisor untuk
pekerjaan latar. Detail di [04 — Backend](04-backend.md).

| Worker | Tugas | Biaya CPU |
|---|---|---|
| `recorder` | Satu `ffmpeg -c copy` per kamera, menulis segmen MPEG-TS lalu me-remux jadi MP4 | sangat kecil (tanpa decode) |
| `snapshot` | Tiap 60 detik ambil **satu keyframe** per kamera → `latest.jpg` + frame timelapse | kecil, sesaat |
| `timelapse` | Tiap malam gabung frame jadi MP4 (encode, `nice 19`) | sedang, sekali sehari |
| `detector` | Fase 3. Decode **sub-stream** kamera prioritas, motion gate, lalu AI | sedang, hanya kamera prioritas |
| `retention` | Hapus segmen lama berdasarkan umur & batas disk | kecil |
| `health` | Pantau status stream go2rtc, umur segmen terakhir, disk/CPU/RAM | kecil |

### Browser / PWA
Vue 3 SPA yang disajikan oleh `nvrd`. Live view memakai WebRTC/MSE dari go2rtc lewat
proxy `nvrd` (satu origin, satu mekanisme login). Detail di
[07 — Frontend & UI/UX](07-frontend-uiux.md).

## Alur data

### Rekaman
1. `recorder` menjalankan per kamera:
   `ffmpeg -rtsp_transport tcp -fflags +genpts -i rtsp://127.0.0.1:8556/<stream> -map 0:v -c:v copy -an
   -f segment -segment_time 600 -segment_atclocktime 1 -strftime 1
   -segment_list pipe:1 -segment_list_type csv <dir>/%Y%m%dT%H%M%S.ts`
2. Saat satu segmen selesai, ffmpeg menulis satu baris CSV ke stdout. `nvrd` membacanya,
   me-remux `.ts` → `.mp4` (`-c copy -tag:v hvc1|avc1 -movflags +faststart`, hanya I/O), menghapus `.ts`,
   lalu mencatat segmen ke tabel `recordings`.
3. Kalau ffmpeg mati, supervisor menyalakan ulang dengan backoff eksponensial
   (1 s → maks 60 s) dan menandai kamera `degraded`/`offline`.

Kenapa MPEG-TS dulu: segmen yang sedang ditulis tetap bisa dibaca walau proses mati
mendadak (MP4 baru valid setelah ditutup). Kenapa diubah ke MP4: browser memutar dan
melompat dalam MP4 `faststart` jauh lebih baik. Lihat [keputusan D6](10-keputusan.md).

Audio (sejak 2026-11-09, [D14](10-keputusan.md)): video tetap `-c:v copy`, audio diubah
ke AAC 16 kHz mono 32 kbps (`-map 0:a:0? -af aresample=async=1 -c:a aac`). Kamera
mengirim G.711 (`bardi`), Opus (`c301`) atau MP2 (`cam1`); hanya AAC yang aman di MP4
untuk semua browser. Sumber Xiaomi direkam dengan `-use_wallclock_as_timestamps 1`
karena timestamp audionya dari go2rtc rusak. Kalau audio kamera tidak bisa di-decode
(ffmpeg tidak menulis segmen dalam 60 dtk, atau berhenti dengan error audio), perekam
otomatis lanjut **tanpa suara**. Kalau gagalnya disertai error decode audio, audio tidak
dicoba ulang berkala (satu percobaan gagal memakan ±1 menit rekaman karena ffmpeg lama
memeriksa stream rusak), hanya saat pengaturan kamera diubah atau nvrd dimulai ulang.
Kalau hanya timeout tanpa error (mis. koneksi P2P Xiaomi lambat), audio dicoba lagi
1 jam kemudian. Live: go2rtc sudah mengirim Opus/FLAC lewat MSE; pemutar mulai
bisu dan punya tombol suara.

### Snapshot & timelapse
`ffmpeg -skip_frame nokey -i rtsp://127.0.0.1:8556/<stream> -frames:v 1 -vf scale=-2:480 <file>.jpg`
— decoder hanya memproses keyframe, jadi biayanya satu gambar per menit, bukan 15 fps.

### Live view
Browser → `wss://<nvrd>/api/v1/live/ws?camera=<id>&quality=sub|main` → `nvrd`
memeriksa sesi → proxy ke `ws://127.0.0.1:1984/api/ws?src=<stream>`. go2rtc hanya
me-remux, tidak men-decode. Decode terjadi di browser.

### Deteksi (fase 3)
`ffmpeg` decode sub-stream (`bardi`: H.264 640x360) dengan `-vf fps=2,scale=320:-2,format=gray`
→ frame mentah ke stdout → Go menghitung selisih frame (motion gate) → hanya bila ada
gerak, frame warna dikirim ke model deteksi orang (ONNX). Event disimpan, segmen di
sekitarnya ditandai `protected` agar tidak terhapus oleh retensi biasa.

## Penyimpanan di disk

```
/DATA/nvr/
├── nvr.db                         # SQLite (+ -wal, -shm)
├── recordings/<kamera>/<YYYY-MM-DD>/<HHMMSS>.mp4
├── snapshots/<kamera>/latest.jpg
├── timelapse/<kamera>/frames/<YYYY-MM-DD>/<HHMMSS>.jpg
├── timelapse/<kamera>/<YYYY-MM-DD>.mp4
├── events/<event-id>.jpg          # fase 3
├── exports/<export-id>.mp4        # fase 2
└── go2rtc/go2rtc.yaml             # rahasia Xiaomi, dikelola go2rtc
```

Nama folder tanggal memakai **WIB** supaya mudah dibaca manusia; kolom waktu di database
tetap UTC.

## Kapasitas disk

Diukur di spike S1 (malam hari, 2026-11-04); angka siang hari kemungkinan lebih besar
karena lebih banyak detail/gerak:

| Kamera | Stream rekaman | Terukur | Per hari |
|---|---|---|---|
| `cam1` | H.264 1280x720 | belum (kamera offline) | perkiraan 5–15 GB |
| `bardi` | HEVC 2304x1296 | ~2,4 MB/menit (~0,3 Mbps) | ~3,4 GB |
| `c301` | HEVC 848x480 | ~0,8 MB/menit (~0,1 Mbps) | ~1,2 GB |

Dengan sisa ~320 GB di `/DATA`, rekaman 24 jam semua kamera muat jauh lebih dari 30 hari.
Default retensi: 14 hari kontinu, 30 hari untuk segmen yang berisi event. Batas aman:
retensi berhenti menunggu umur dan langsung menghapus yang tertua bila sisa disk < 10%.
