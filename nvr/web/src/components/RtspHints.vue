<template>
  <details class="rounded-lg bg-surface-2 px-3 py-2 text-xs">
    <summary class="cursor-pointer font-semibold">Contoh URL per merek kamera</summary>
    <p class="mt-2 text-muted">Ganti <code>IP</code> dengan alamat kamera. RTSP harus diaktifkan di aplikasi kamera; username/password isi di kolom terpisah.</p>
    <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
      <dt class="font-medium">Bardi / Tuya</dt>
      <dd class="font-mono break-all">rtsp://IP:554/V_ENC_000 · sub <span class="text-muted">/V_ENC_001</span></dd>
      <dt class="font-medium">TP-Link Tapo</dt>
      <dd class="font-mono break-all">rtsp://IP:554/stream1 · sub <span class="text-muted">/stream2</span></dd>
      <dt class="font-medium">Hikvision / EZVIZ</dt>
      <dd class="font-mono break-all">rtsp://IP:554/Streaming/Channels/101 · sub <span class="text-muted">/102</span></dd>
      <dt class="font-medium">Dahua / Imou</dt>
      <dd class="font-mono break-all">rtsp://IP:554/cam/realmonitor?channel=1&amp;subtype=0 · sub <span class="text-muted">subtype=1</span></dd>
      <dt class="font-medium">Reolink</dt>
      <dd class="font-mono break-all">rtsp://IP:554/h264Preview_01_main · sub <span class="text-muted">_sub</span></dd>
      <dt class="font-medium">Xiaomi</dt>
      <dd>Tidak lewat RTSP; tambahkan lewat dashboard go2rtc (login akun Xiaomi), lalu tekan <em>Sinkronkan</em>.</dd>
    </dl>
  </details>
</template>
