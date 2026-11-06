# 06 — API

Basis: `/api/v1`. JSON untuk semua request/response kecuali file media. Semua rute
kecuali `POST /auth/login` dan `GET /healthz` butuh sesi.

## Konvensi

- **Autentikasi:** cookie `nvr_session` (`HttpOnly`, `SameSite=Lax`, `Secure` bila HTTPS).
  Token acak 32 byte; yang disimpan di DB hanya hash SHA-256-nya. Masa berlaku 30 hari,
  diperpanjang otomatis saat aktif.
- **CSRF:** request yang mengubah data (`POST/PATCH/PUT/DELETE`) wajib membawa header
  `X-Requested-With: nvr`. Bersama `SameSite=Lax` ini cukup untuk SPA satu origin.
- **Waktu:** dikirim sebagai string RFC 3339 UTC (`2026-11-04T10:00:00Z`). Query
  `from`/`to` menerima format yang sama.
- **Paginasi:** cursor — `?limit=50&before=<id>`; respons berisi `next_before` bila masih
  ada data.
- **Error:**
  ```json
  { "error": { "code": "camera_not_found", "message": "Kamera tidak ditemukan" } }
  ```
  Kode HTTP: `400` validasi, `401` belum login, `403` tidak berhak, `404`, `409` konflik,
  `429` rate limit, `500`, `503` dependensi (go2rtc) tidak tersedia.

## Auth

| Method | Path | Keterangan |
|---|---|---|
| `POST` | `/auth/login` | `{username, password}` → set cookie. Rate limit 5 percobaan/menit per IP. |
| `POST` | `/auth/logout` | Hapus sesi saat ini |
| `GET` | `/auth/me` | `{id, username, role}` |
| `PUT` | `/auth/password` | `{current_password, new_password}` |
| `GET` | `/auth/sessions` | Perangkat (sesi) milik pengguna ini; `current` menandai perangkat yang dipakai |
| `DELETE` | `/auth/sessions/{id}` | Keluarkan satu perangkat sendiri (bukan perangkat ini) |
| `POST` | `/auth/sessions/logout-others` | Keluarkan semua perangkat lain |
| `GET` | `/locations` | Lokasi yang punya kamera terlihat oleh pemanggil (admin: semua) `{id, name, cameras}` |
| `POST` | `/locations` | `{name}` (admin) |
| `PATCH` | `/locations/{id}` | `{name}` (admin) |
| `DELETE` | `/locations/{id}` | Hanya lokasi kosong → `409 location_in_use` (admin) |
| `GET` | `/users/{id}/access` | `{access_all, locations[], cameras[], deny_cameras[]}` (admin) |
| `PUT` | `/users/{id}/access` | Ganti akses viewer; admin selalu `access_all` (admin) |
| `GET` | `/users` | Daftar pengguna + jumlah perangkat aktif & terakhir aktif (admin) |
| `POST` | `/users` | `{username, password, role}`; username 3–32 `[a-z0-9._-]`, password ≥ 10 (admin) |
| `PATCH` | `/users/{id}` | `{role?, password?}`; reset password mengeluarkan semua perangkat pengguna itu. Tidak bisa mengubah akun sendiri atau menurunkan admin terakhir (admin) |
| `DELETE` | `/users/{id}` | Hapus pengguna (bukan diri sendiri, bukan admin terakhir) (admin) |

## Kamera

| Method | Path | Keterangan |
|---|---|---|
| `GET` | `/cameras` | Daftar kamera + status singkat |
| `GET` | `/cameras/{id}` | Detail (tanpa URL sumber/rahasia) |
| `POST` | `/cameras` | `{slug, name, main, sub?}` (sumber = `{url, username, password}`); stream ditulis ke go2rtc (`PUT`, persisten), lalu ditemukan & mulai direkam (admin) |
| `GET` | `/cameras/{id}/sources` | Sumber tanpa password: `{editable, reason, xiaomi, main:{url, username, has_password}, sub}` (admin) |
| `PUT` | `/cameras/{id}/sources` | `{main, sub?}`; password kosong = pakai password lama (username sama); `sub: null` menghapus sub-stream. Xiaomi/sumber khusus → `409` (admin) |
| `DELETE` | `/cameras/{id}` | `{confirm: <slug>}`; hapus stream go2rtc, kamera, rekaman, snapshot, timelapse. Xiaomi → `409` (admin) |
| `PATCH` | `/cameras/{id}` | Ubah sebagian field (nama, rekaman, retensi, deteksi, urutan) |
| `DELETE` | `/cameras/{id}` | Fase 2. Hapus kamera; `?delete_recordings=true` untuk ikut hapus file |
| `POST` | `/cameras/probe` | `{source, stream, camera_id?}` → `{ok, info:{codec,width,height,fps,audio}, advice[]}` atau `{ok:false, error}` (pesan Indonesia: login salah, path 404, port ditolak, kamera tidak menjawab). ffprobe langsung ke kamera, timeout 20 dtk (admin) |
| `GET` | `/cameras/{id}/snapshot.jpg` | Snapshot terbaru (`Cache-Control: no-cache`, `ETag`) |

Contoh `GET /cameras`:
```json
[
  {
    "id": 2,
    "slug": "bardi",
    "name": "Teras",
    "has_sub_stream": true,
    "codec": "hevc",
    "record_enabled": true,
    "detect_enabled": true,
    "status": {
      "state": "running",
      "last_segment_at": "2026-11-04T10:40:00Z",
      "last_snapshot_at": "2026-11-04T10:49:12Z",
      "message": null
    }
  }
]
```

`state`: `starting | running | degraded | offline | disabled`. `message` berisi kalimat
yang bisa ditindaklanjuti, mis. "Sesi Xiaomi kemungkinan kedaluwarsa — login ulang di
go2rtc".

## Rekaman & playback

| Method | Path | Keterangan |
|---|---|---|
| `GET` | `/cameras/{id}/timeline?date=2026-11-04` | Rentang rekaman + event untuk satu hari WIB |
| `GET` | `/cameras/{id}/recordings?from=&to=` | Daftar segmen mentah |
| `GET` | `/cameras/{id}/recordings/at?t=` | Segmen yang memuat waktu `t` + offset detik |
| `GET` | `/recordings/{id}/file` | File MP4, mendukung `Range` |
| `GET` | `/cameras/{id}/events?from=&to=` | Event deteksi (`motion`/`person`) yang overlap rentang (maks 7 hari): `{id, label, score, start, end, reviewed}`; `score` motion = luas gerak puncak 0..1 |
| `GET` | `/cameras/{id}/export/estimate?from=&to=` | Perkiraan klip: `seconds` terekam, `bytes`, `segments`, `gap_seconds` |
| `GET` | `/cameras/{id}/export?from=&to=` | Unduh klip MP4 (stream copy, maks 3 jam, satu export sekaligus → `429` bila sibuk, `404` bila tanpa rekaman). Nama file `<slug>_<tanggal>_<HHMMSS>-<HHMMSS>.mp4` (WIB) |

Contoh `GET /cameras/2/timeline?date=2026-11-04`:
```json
{
  "date": "2026-11-04",
  "timezone": "Asia/Jakarta",
  "ranges": [
    { "start": "2026-11-03T17:00:00Z", "end": "2026-11-04T02:20:00Z" },
    { "start": "2026-11-04T02:31:00Z", "end": "2026-11-04T10:40:00Z" }
  ],
  "events": [
    { "id": 91, "label": "person", "start": "2026-11-04T06:12:03Z", "end": "2026-11-04T06:12:40Z" }
  ]
}
```

`ranges` sudah digabung server (segmen bersambung → satu rentang) agar frontend cukup
menggambar bar.

## Export (fase 2)

| Method | Path | Keterangan |
|---|---|---|
| `POST` | `/exports` | `{camera_id, from, to}` (maks 2 jam) → `202` + objek export |
| `GET` | `/exports` | Daftar export |
| `GET` | `/exports/{id}` | Status |
| `GET` | `/exports/{id}/file` | Unduh MP4 |
| `DELETE` | `/exports/{id}` | Hapus |

Export memakai ffmpeg `concat` + `-c copy` (tanpa transcode), dipotong di keyframe
terdekat.

## Event (fase 3)

| Method | Path | Keterangan |
|---|---|---|
| `GET` | `/events?camera_id=&label=&from=&to=&reviewed=&limit=&before=` | Daftar event |
| `GET` | `/events/{id}` | Detail |
| `GET` | `/events/{id}/snapshot.jpg` | Snapshot event |
| `PATCH` | `/events/{id}` | `{reviewed: true}` |
| `POST` | `/events/review` | `{ids: [...]}` tandai banyak sekaligus |
| `DELETE` | `/events/{id}` | Hapus event (segmen kembali ke retensi biasa) |

## Timelapse

| Method | Path | Keterangan |
|---|---|---|
| `GET` | `/timelapse?camera_id=` | Daftar tanggal yang tersedia |
| `GET` | `/timelapse/{camera_id}/{date}.mp4` | Video timelapse, mendukung `Range` |

## Sistem & pengaturan

| Method | Path | Keterangan |
|---|---|---|
| `GET` | `/healthz` | Tanpa auth. `200 ok` bila DB dan go2rtc bisa dihubungi |
| `GET` | `/system/status` | Status worker per kamera, 50 baris log ffmpeg terakhir (admin) |
| `GET` | `/system/stats` | CPU, RAM, load, disk `/data`, ukuran rekaman per kamera, versi, jumlah tujuan notifikasi |
| `POST` | `/system/notify-test` | Kirim pesan tes ke semua webhook notifikasi; `204`, `409` bila belum diatur, `502` bila gagal (admin) |
| `GET` | `/settings` | Pengaturan global |
| `PUT` | `/settings` | Ubah pengaturan global (admin) |
| `GET/POST/PATCH/DELETE` | `/users[/{id}]` | Fase 2. Kelola pengguna (admin) |

## Push realtime — SSE

`GET /api/v1/stream` (`text/event-stream`), satu koneksi per tab.

| Event | Data |
|---|---|
| `camera.status` | `{camera_id, state, message, last_segment_at, last_snapshot_at}` |
| `snapshot.updated` | `{camera_id, at}` — frontend me-refresh gambar tile |
| `event.started` / `event.ended` | Objek event (fase 3) |
| `export.updated` | Objek export (fase 2) |
| `system.alert` | `{level, message}` mis. disk < 10% |

SSE dipilih dibanding WebSocket karena arahnya satu (server → browser), otomatis
reconnect di browser, dan mudah di-proxy.

## Live view — proxy ke go2rtc

| Path | Diteruskan ke |
|---|---|
| `GET /api/v1/live/ws?camera={id}&quality=main\|sub` (WebSocket) | `ws://127.0.0.1:1984/api/ws?src=<stream>` |
| `POST /api/v1/live/webrtc?camera={id}&quality=` | `POST http://127.0.0.1:1984/api/webrtc?src=<stream>` (pertukaran SDP) |

`nvrd` memetakan `camera` + `quality` ke nama stream; klien tidak pernah menyebut nama
stream go2rtc langsung, dan API go2rtc (port 1984) **tidak** diekspos ke jaringan.
Untuk WebRTC dari luar LAN (ZeroTier), go2rtc perlu tahu kandidat IP ZeroTier; diatur di
`go2rtc.yaml` (`webrtc.candidates`).
