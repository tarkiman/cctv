# 07 — Frontend & UI/UX

## Stack

| Bagian | Pilihan | Alasan |
|---|---|---|
| Framework | Vue 3 (Composition API, `<script setup>`) + TypeScript | Sesuai permintaan, type-safe |
| Build | Vite | Cepat, output kecil, mudah di-embed ke binary Go |
| Routing | Vue Router (lazy-loaded per halaman) | Bundle awal kecil |
| State | Pinia | Store per domain: `auth`, `cameras`, `system`, `events` |
| Styling | Tailwind CSS v4 + design token CSS variable | Ringan, konsisten, tema gelap/terang mudah |
| Komponen dasar | [Reka UI](https://reka-ui.com) (headless, aksesibel) | Dialog, menu, popover, tooltip, slider dengan keyboard & ARIA yang benar, tanpa gaya bawaan berat |
| Ikon | `lucide-vue-next` (tree-shaken) | Konsisten, hanya ikon yang dipakai ikut di bundle |
| Tanggal | `Intl.DateTimeFormat` dengan `timeZone: 'Asia/Jakarta'` | Tanpa library tanggal besar |
| Live player | `video-rtc.js` dari go2rtc (MSE/WebRTC), dibungkus komponen Vue | Protokol sudah ditangani go2rtc |
| PWA | `vite-plugin-pwa` | Bisa di-install di HP, ikon & splash |
| Kualitas | ESLint, Prettier, `vue-tsc`, Vitest, Playwright (smoke test) | |

Tidak memakai UI kit besar (Vuetify, Element Plus, Quasar): ukuran bundle dan gaya bawaan
yang sulit diubah tidak sebanding untuk aplikasi kecil ini.

**Target ukuran:** JS awal < 150 KB gzip; halaman lain dimuat saat dibuka.

## Prinsip UX

1. **Status selalu terlihat.** Setiap kamera menampilkan state (Live / Merekam /
   Bermasalah / Offline) dengan warna **dan** ikon **dan** teks — tidak mengandalkan
   warna saja. Waktu "terakhir diperbarui" ditampilkan relatif ("2 menit lalu") dengan
   waktu absolut di tooltip.
2. **Pesan yang bisa ditindaklanjuti.** Bukan "Error", tapi "c301 offline sejak 10:42 —
   sesi Xiaomi kemungkinan kedaluwarsa. [Cara login ulang]".
3. **Mobile-first.** Sebagian besar pemakaian dari HP. Target sentuh minimal 44×44 px,
   navigasi bawah di layar kecil, sidebar di layar lebar.
4. **Hemat bandwidth & CPU perangkat.** Tile live di grid memakai sub-stream bila ada, video
   hanya berjalan saat tile terlihat (`IntersectionObserver`) dan berhenti saat tab
   disembunyikan (`visibilitychange`). Mode "Hemat data" menampilkan snapshot per menit
   saja.
5. **Cepat terasa.** Skeleton saat memuat (bukan spinner layar penuh), tidak ada layout
   shift (tile rasio 16:9 tetap), aksi lambat (export) berjalan di latar dengan toast
   progres.
6. **Aman dari salah klik.** Aksi destruktif (hapus kamera, hapus rekaman) butuh dialog
   konfirmasi yang menyebutkan akibatnya; bila memungkinkan, sediakan "Urungkan".
7. **Konsisten waktu.** Semua waktu ditampilkan dalam WIB, dengan label "WIB" di header
   timeline dan pemilih tanggal.
8. **Bahasa Indonesia** yang ringkas dan sehari-hari. Istilah teknis (codec, bitrate) hanya
   di halaman pengaturan/status.

## Peta halaman

```
/login
/                    Live (beranda)
/camera/:slug        Satu kamera: live layar penuh + playback
/events              Event (fase 3)
/timelapse           Timelapse
/exports             Export (fase 2)
/settings            Pengaturan
  ├─ /settings/cameras      Kamera (daftar, tambah, ubah, tes koneksi)
  ├─ /settings/recording    Rekaman & retensi
  ├─ /settings/notifications  Notifikasi (fase 2)
  ├─ /settings/users        Pengguna (fase 2)
  └─ /settings/account      Akun saya (ganti password)
/system              Status sistem
```

Navigasi utama (5 item, muat di bottom bar HP): **Live · Rekaman · Event · Timelapse ·
Lainnya** (Lainnya membuka Pengaturan, Status, Keluar).

## Halaman

### Login
- Satu kolom: username, password (tombol tampilkan/sembunyikan), tombol "Masuk".
- Pesan gagal generik ("Username atau password salah") untuk tidak membocorkan username.
- Setelah rate limit: tampilkan berapa detik harus menunggu.
- `autocomplete="username"` / `"current-password"` agar password manager bekerja.

### Live (beranda)
```
┌──────────────────────────────────────────────────────────────┐
│ CCTV Rumah            ● 3/3 online   Disk 23%      [⊞ 2×2 ▾] │
├──────────────────────────────┬───────────────────────────────┤
│ ┌──────────────────────────┐ │ ┌───────────────────────────┐ │
│ │                          │ │ │                           │ │
│ │        video live        │ │ │        video live         │ │
│ │                          │ │ │                           │ │
│ └──────────────────────────┘ │ └───────────────────────────┘ │
│ Teras · bardi   ● Merekam    │ Ruang depan · cam1  ● Merekam │
├──────────────────────────────┼───────────────────────────────┤
│ ┌──────────────────────────┐ │                               │
│ │  ⚠ Offline sejak 10:42   │ │                               │
│ │  Sesi Xiaomi kedaluwarsa │ │                               │
│ │  [Cara memperbaiki]      │ │                               │
│ └──────────────────────────┘ │                               │
│ Kamar · c301    ● Offline    │                               │
└──────────────────────────────┴───────────────────────────────┘
```
- **Grid yang memaksimalkan lebar layar** (masukan pengguna setelah uji S4: daftar video
  berjejer ke bawah memboroskan layar lebar):
  - Mode **Otomatis** (default): CSS Grid
    `grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr))`, sehingga
    jumlah kolom mengikuti lebar layar — 1 di HP, 2 di tablet, 3–4 di desktop/monitor
    lebar — tanpa breakpoint manual. Tile rasio 16:9 tetap, jadi tidak ada layout shift.
  - Mode manual: **1 / 2×2 / 3×3**, dan **Fokus** (satu kamera besar + strip kamera lain
    di samping pada layar lebar, atau di bawah pada HP). Pilihan disimpan per perangkat
    (`localStorage`).
  - Mode **Dinding** (tombol layar penuh): grid memenuhi seluruh layar tanpa header dan
    navigasi, label kamera tampil sebagai overlay tipis yang memudar setelah 3 detik tanpa
    gerakan mouse — untuk monitor/TV yang memantau terus.
  - Jumlah kolom maksimal dibatasi agar tile tidak terlalu kecil untuk dikenali; bila
    kamera lebih banyak dari slot, grid menggulir vertikal (bukan mengecilkan tile).
  - Halaman lain yang menampilkan banyak media (Event, Timelapse, Export) memakai pola
    grid kartu yang sama.
- Ketuk tile → `/camera/:slug`. Tekan lama/menu → "Putar rekaman 5 menit lalu".
- Urutan tile bisa diatur (drag) di Pengaturan → Kamera.
- Header ringkas: jumlah kamera online, peringatan disk, status SSE (bila putus: banner
  "Koneksi terputus, mencoba lagi…").

### Satu kamera: live + playback
```
┌──────────────────────────────────────────────────────────────┐
│ ← Teras (bardi)        [Live] [Rekaman]        Kualitas: HD ▾ │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│                     video (16:9, zoom/pinch)                 │
│                                                              │
├──────────────────────────────────────────────────────────────┤
│ ◀ Sel, 4 Nov 2026 ▶  [📅]        ⏪10s  ▶  10s⏩   1x ▾   ⤓   │
│ 00    03    06    09    12    15    18    21    24  WIB      │
│ ████████████████▌ ██████████████████████████░░░░░░░░░        │
│        ▲ person          ▲ person      │ sekarang            │
└──────────────────────────────────────────────────────────────┘
```
- **Tata letak layar lebar (≥ 1200 px):** video di kiri (sebesar mungkin, rasio 16:9),
  panel kanan selebar ~320 px berisi pemilih tanggal, daftar event hari itu, dan info
  kamera; timeline tetap selebar penuh di bawah video. Di HP semuanya bertumpuk vertikal
  dengan video di atas.
- **Mode Live:** layar penuh, pinch/scroll untuk zoom digital, tombol snapshot (unduh JPG),
  pilihan kualitas (HD = stream utama, SD = sub-stream).
- **Mode Rekaman:**
  - Timeline 24 jam: bar berisi = ada rekaman, celah = tidak ada; penanda event berwarna.
  - Klik/seret di timeline untuk lompat; zoom timeline (1 jam / 6 jam / 24 jam) dengan
    pinch atau tombol.
  - Pemutaran mulus antar-segmen: dua elemen `<video>`, segmen berikutnya dimuat diam-diam
    lalu ditukar saat segmen saat ini habis.
  - Kecepatan 0,5×/1×/2×/4×/8×; lompat ±10 detik; "Ke event berikutnya".
  - Tombol unduh: segmen saat ini, atau pilih rentang → export (fase 2).
  - URL menyimpan posisi (`?t=2026-11-04T06:12:03Z`) sehingga bisa dibagikan/di-bookmark.
- **Shortcut keyboard** (desktop): `Space` putar/jeda, `←/→` ±10 s, `Shift+←/→` ±1 menit,
  `,`/`.` frame sebelum/sesudah saat jeda, `[`/`]` kecepatan, `L` ke live, `F` layar penuh,
  `?` tampilkan daftar shortcut.
- Bila browser tidak mendukung HEVC: pesan jelas + tombol "Pakai kualitas SD" (bila ada
  sub-stream) atau "Unduh rekaman".

### Event (fase 3)
- Daftar kartu dengan thumbnail, kamera, label, waktu, durasi; dikelompokkan per hari.
- Filter: kamera, label, tanggal, "belum ditinjau". Filter tercermin di URL.
- Klik kartu → putar klip mulai 5 detik sebelum event.
- Tandai sudah ditinjau (satu per satu atau "Tandai semua di hari ini").

### Timelapse
- Pilih kamera & tanggal; video diputar dengan kecepatan bisa diubah.
- Grid thumbnail tanggal (minggu ini) agar cepat memilih.

### Pengaturan → Kamera
- Daftar kamera dengan toggle cepat: Rekam, Deteksi.
- Form tambah/ubah dengan langkah: 1) jenis sumber (RTSP / ONVIF / Xiaomi via go2rtc),
  2) URL, 3) **Tes koneksi** → tampilkan codec, resolusi, fps, dan peringatan bila HEVC
  ("Tidak bisa diputar di Firefox"), 4) nama & opsi rekaman.
- Untuk Xiaomi: tautan ke dashboard go2rtc beserta panduan singkat (region `sg`).
- Validasi inline, tombol Simpan nonaktif sampai valid, pesan error di bawah field.

### Pengaturan → Rekaman & retensi
- Per kamera: retensi kontinu (hari), retensi event (hari), stream yang direkam.
- Global: batas minimal sisa disk (%).
- Perkiraan dihitung langsung: "Dengan pengaturan ini, perkiraan pemakaian 150 GB dari
  320 GB tersedia" (berdasarkan rata-rata ukuran segmen 24 jam terakhir).

### Status sistem
- Kartu: CPU, RAM, load, disk (dengan bar), uptime, versi `nvrd`/go2rtc.
- Tabel worker per kamera: state, uptime, restart terakhir, segmen terakhir, tombol
  "Lihat log" (50 baris stderr ffmpeg) dan "Restart".

## Design system

### Token warna (CSS variable)
```css
:root {
  --bg: #f6f7f9;  --surface: #ffffff;  --surface-2: #eef0f3;
  --text: #111827; --text-muted: #5b6472; --border: #d9dde3;
  --primary: #2563eb; --primary-contrast: #ffffff;
  --ok: #15803d; --warn: #b45309; --danger: #b91c1c; --info: #0369a1;
  --event: #7c3aed;               /* penanda event di timeline */
  --radius: 10px;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {
    --bg: #0b0d10; --surface: #14171c; --surface-2: #1c2027;
    --text: #e6e8eb; --text-muted: #9aa3ae; --border: #2a2f37;
    --primary: #60a5fa; --primary-contrast: #0b0d10;
    --ok: #4ade80; --warn: #fbbf24; --danger: #f87171; --info: #38bdf8;
    --event: #a78bfa;
  }
}
:root[data-theme="dark"] { /* nilai sama dengan blok gelap di atas */ }
```
- Tema gelap adalah default yang direkomendasikan (sering dipakai malam hari, video lebih
  menonjol), tetap mengikuti preferensi sistem dan bisa diubah manual.
- Semua pasangan teks/latar memenuhi kontras WCAG AA (4,5:1 teks biasa, 3:1 komponen UI).

### Tipografi & spasi
- Font sistem (`system-ui`) — tanpa unduhan font.
- Skala: 12 / 14 / 16 (dasar) / 20 / 24 px. Angka waktu memakai `font-variant-numeric:
  tabular-nums` agar tidak "goyang".
- Spasi kelipatan 4 px.

### Komponen inti
`AppShell`, `NavBar`/`BottomNav`, `CameraTile`, `LivePlayer`, `RecordingPlayer`,
`Timeline`, `StatusBadge`, `EmptyState`, `ErrorState`, `Skeleton`, `ConfirmDialog`,
`Toast`, `DatePicker`, `FormField`.

Setiap komponen yang memuat data punya empat keadaan yang dirancang: **memuat, kosong,
error, berhasil**.

## Aksesibilitas

- Semua kontrol bisa dipakai dengan keyboard; urutan fokus logis; fokus terlihat jelas
  (`:focus-visible` ring 2 px).
- Tombol ikon wajib punya `aria-label`; status penting diumumkan lewat region
  `aria-live="polite"` (mis. "Kamera c301 offline").
- Timeline adalah `role="slider"` dengan `aria-valuetext` berupa waktu ("06:12:03 WIB").
- Hormati `prefers-reduced-motion` (tanpa animasi transisi).
- Ukuran teks mengikuti setelan browser (pakai `rem`).

## Struktur folder

```
web/
├── index.html
├── vite.config.ts
├── src/
│   ├── main.ts
│   ├── App.vue
│   ├── router/index.ts
│   ├── api/            # klien fetch bertipe + SSE client
│   ├── stores/         # pinia: auth, cameras, system, events
│   ├── composables/    # useVisibility, useSse, useTimeline, useHotkeys, useCodecSupport
│   ├── components/     # komponen inti di atas
│   ├── pages/          # satu file per rute
│   ├── styles/         # tokens.css, base.css
│   └── utils/          # format waktu WIB, ukuran file
└── public/             # ikon PWA, manifest
```

- Klien API memakai `fetch` tipis (tanpa axios), menambahkan header `X-Requested-With`,
  dan mengalihkan ke `/login` saat `401`.
- Tipe respons API didefinisikan di `src/api/types.ts` dan dijaga sinkron dengan
  [06 — API](06-api.md).
