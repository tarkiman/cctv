<script setup lang="ts">
// Live video over go2rtc's MSE websocket protocol, proxied by nvrd:
//   client -> {"type":"mse","value":"<supported codecs>"}
//   server -> {"type":"mse","value":"<mime>"} then binary fMP4 fragments.
// Decoding happens in the browser; the server only remuxes.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api/client'

const props = withDefaults(defineProps<{ cameraId: number; quality?: 'main' | 'sub'; active?: boolean; fit?: 'contain' | 'cover' }>(), {
  quality: 'main',
  active: true,
  fit: 'contain',
})
const emit = defineEmits<{ state: [state: 'connecting' | 'playing' | 'error'] }>()

const CODECS = ['avc1.640029', 'avc1.64002A', 'avc1.640033', 'hvc1.1.6.L153.B0', 'mp4a.40.2', 'mp4a.40.5', 'flac', 'opus']
const video = ref<HTMLVideoElement | null>(null)
const state = ref<'idle' | 'connecting' | 'playing' | 'error'>('idle')
const error = ref('')

let ws: WebSocket | null = null
let ms: MediaSource | null = null
let sb: SourceBuffer | null = null
let objectUrl = ''
let pending: Uint8Array[] = []
let retry: number | undefined
let retries = 0
let stopped = true

function setState(s: typeof state.value, msg = '') {
  state.value = s
  error.value = msg
  if (s !== 'idle') emit('state', s)
}

function start() {
  stop()
  stopped = false
  const v = video.value
  const MS = window.ManagedMediaSource ?? window.MediaSource
  if (!v || !MS) {
    setState('error', 'Browser ini tidak mendukung pemutaran live (MSE).')
    return
  }
  setState('connecting')
  ms = new MS()
  ms.addEventListener('sourceopen', () => openSocket(MS), { once: true })
  if (window.ManagedMediaSource && MS === window.ManagedMediaSource) {
    // Safari's ManagedMediaSource must be attached via srcObject with remote playback off.
    v.disableRemotePlayback = true
    ;(v as HTMLVideoElement & { srcObject: unknown }).srcObject = ms
  } else {
    objectUrl = URL.createObjectURL(ms)
    v.src = objectUrl
  }
}

function openSocket(MS: typeof MediaSource) {
  if (stopped) return
  ws = new WebSocket(api.liveWsUrl(props.cameraId, props.quality))
  ws.binaryType = 'arraybuffer'
  ws.onopen = () => {
    const codecs = CODECS.filter((c) => MS.isTypeSupported(`video/mp4; codecs="${c}"`)).join()
    ws?.send(JSON.stringify({ type: 'mse', value: codecs }))
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      const msg = JSON.parse(ev.data)
      if (msg.type === 'mse' && ms) {
        try {
          sb = ms.addSourceBuffer(msg.value)
          sb.mode = 'segments'
          sb.addEventListener('updateend', flush)
        } catch {
          setState('error', 'Codec kamera ini tidak didukung browser. Coba kualitas SD atau browser lain.')
          stop(false)
        }
      } else if (msg.type === 'error') {
        setState('error', 'Kamera tidak bisa ditayangkan saat ini.')
      }
      return
    }
    pending.push(new Uint8Array(ev.data as ArrayBuffer))
    flush()
  }
  ws.onclose = () => {
    if (stopped) return
    // Reconnect with backoff; the camera or go2rtc may be restarting.
    if (state.value !== 'error') setState('connecting')
    retries++
    retry = window.setTimeout(start, Math.min(30_000, 1000 * 2 ** Math.min(retries, 5)))
  }
}

function flush() {
  if (!sb || sb.updating || pending.length === 0) return
  const total = pending.reduce((n, b) => n + b.byteLength, 0)
  const buf = new Uint8Array(total)
  let off = 0
  for (const b of pending) {
    buf.set(b, off)
    off += b.byteLength
  }
  pending = []
  try {
    sb.appendBuffer(buf)
  } catch {
    // QuotaExceeded: drop old media and let the next fragment retry.
    trim(true)
    return
  }
  const v = video.value
  if (!v || !sb.buffered.length) return
  const end = sb.buffered.end(sb.buffered.length - 1)
  // Stay near the live edge; jump forward if we fall behind (e.g. after a stall).
  if (end - v.currentTime > 3) v.currentTime = end - 0.5
  if (v.paused) v.play().catch(() => {})
  if (state.value !== 'playing') {
    retries = 0
    setState('playing')
  }
  trim(false)
}

function trim(force: boolean) {
  if (!sb || sb.updating || !sb.buffered.length) return
  const start = sb.buffered.start(0)
  const end = sb.buffered.end(sb.buffered.length - 1)
  if (force || end - start > 20) {
    try {
      sb.remove(start, end - 5)
    } catch {
      /* ignore */
    }
  }
}

function stop(resetState = true) {
  stopped = true
  clearTimeout(retry)
  if (ws) {
    ws.onclose = null
    ws.close()
  }
  ws = null
  sb = null
  pending = []
  if (ms && ms.readyState === 'open') {
    try {
      ms.endOfStream()
    } catch {
      /* ignore */
    }
  }
  ms = null
  const v = video.value
  if (v) {
    v.removeAttribute('src')
    ;(v as HTMLVideoElement & { srcObject: unknown }).srcObject = null
    v.load()
  }
  if (objectUrl) URL.revokeObjectURL(objectUrl)
  objectUrl = ''
  if (resetState) setState('idle')
}

// Start only once the <video> element exists: an immediate watcher would run during
// setup, before mount, find no element and give up.
onMounted(() => props.active && start())
watch(
  () => [props.active, props.cameraId, props.quality] as const,
  ([active]) => (active ? start() : stop()),
  { flush: 'post' },
)
onBeforeUnmount(() => stop(false))

defineExpose({ video, state, retry: start })
</script>

<template>
  <!-- Transparent until frames arrive, so whatever is underneath (e.g. the latest
       snapshot) stays visible while connecting instead of a black box. -->
  <div class="relative h-full w-full">
    <video
      ref="video"
      class="h-full w-full transition-opacity duration-300"
      :class="[fit === 'cover' ? 'object-cover' : 'object-contain', state === 'playing' ? 'opacity-100' : 'opacity-0']"
      muted
      playsinline
      autoplay
      aria-label="Video live"
    />
    <slot :state="state" :error="error" />
  </div>
</template>
