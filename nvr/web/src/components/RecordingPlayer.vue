<script setup lang="ts">
// Plays recorded segments back to back. Two <video> elements alternate: the next
// segment loads in the hidden one and is swapped in when the current one ends, so
// playback continues across 10-minute segment boundaries without a visible reload.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api, ApiError } from '../api/client'
import type { Recording } from '../api/types'
import { canPlayCodec } from '../composables/media'

const props = withDefaults(defineProps<{ cameraId: number; rate: number; muted?: boolean }>(), { muted: true })
const emit = defineEmits<{ time: [t: Date | null]; state: [playing: boolean]; segment: [rec: Recording | null] }>()

type Slot = { el: HTMLVideoElement | null; rec: Recording | null }
const slots = ref<[Slot, Slot]>([
  { el: null, rec: null },
  { el: null, rec: null },
])
const active = ref<0 | 1>(0)
const message = ref('')
const loading = ref(false)
const playing = ref(false)
let raf = 0

const cur = computed(() => slots.value[active.value])
const other = computed(() => slots.value[active.value === 0 ? 1 : 0])
const unsupported = computed(() => !!cur.value.rec && !canPlayCodec(cur.value.rec.codec))

function setSrc(slot: Slot, rec: Recording | null) {
  slot.rec = rec
  if (!slot.el) return
  if (rec) {
    slot.el.src = rec.url
    slot.el.load()
  } else {
    slot.el.removeAttribute('src')
    slot.el.load()
  }
}

/** Start playback at wall-clock time t (snaps forward to the next footage after a gap). */
async function playAt(t: Date) {
  message.value = ''
  const r = cur.value.rec
  const el = cur.value.el
  // Seek within the loaded segment without refetching.
  if (r && el && t >= new Date(r.start) && t < new Date(r.end)) {
    el.currentTime = (t.getTime() - new Date(r.start).getTime()) / 1000
    el.play().catch(() => {})
    return
  }
  loading.value = true
  try {
    const res = await api.recordingAt(props.cameraId, t)
    setSrc(cur.value, res.recording)
    setSrc(other.value, null)
    const el2 = cur.value.el!
    el2.addEventListener(
      'loadedmetadata',
      () => {
        el2.currentTime = res.offset
        el2.playbackRate = props.rate
        el2.play().catch(() => {})
      },
      { once: true },
    )
    emit('segment', res.recording)
  } catch (e) {
    message.value = e instanceof ApiError && e.status === 404 ? 'Tidak ada rekaman pada atau setelah waktu ini.' : (e as Error).message
    setSrc(cur.value, null)
    emit('segment', null)
    emit('time', null)
  } finally {
    loading.value = false
  }
}

let preloading = false
let noNextUntil = 0
async function preloadNext() {
  const r = cur.value.rec
  if (!r || other.value.rec || preloading || Date.now() < noNextUntil) return
  preloading = true
  try {
    setSrc(other.value, await api.recordingNext(r.id))
  } catch {
    // No next segment yet (we are near the live edge); check again in a while.
    noNextUntil = Date.now() + 15_000
  } finally {
    preloading = false
  }
}

async function onEnded() {
  const next = other.value.rec
  if (!next) {
    noNextUntil = 0
    await preloadNext()
    if (!other.value.rec) {
      message.value = 'Akhir rekaman yang tersedia.'
      playing.value = false
      emit('state', false)
      return
    }
  }
  active.value = active.value === 0 ? 1 : 0
  const el = cur.value.el!
  el.currentTime = 0
  el.playbackRate = props.rate
  el.play().catch(() => {})
  setSrc(other.value, null)
  emit('segment', cur.value.rec)
}

function tick() {
  const r = cur.value.rec
  const el = cur.value.el
  if (r && el && !el.seeking) {
    emit('time', new Date(new Date(r.start).getTime() + el.currentTime * 1000))
    if (el.duration && el.duration - el.currentTime < 30) preloadNext()
  }
  raf = requestAnimationFrame(tick)
}
raf = requestAnimationFrame(tick)
onBeforeUnmount(() => cancelAnimationFrame(raf))

watch(
  () => props.rate,
  (r) => {
    if (cur.value.el) cur.value.el.playbackRate = r
  },
)

function onPlayState(i: 0 | 1, isPlaying: boolean) {
  if (i !== active.value) return
  playing.value = isPlaying
  emit('state', isPlaying)
}

function toggle() {
  const el = cur.value.el
  if (!el || !cur.value.rec) return
  if (el.paused) el.play().catch(() => {})
  else el.pause()
}

function skip(seconds: number) {
  const r = cur.value.rec
  const el = cur.value.el
  if (!r || !el) return
  playAt(new Date(new Date(r.start).getTime() + (el.currentTime + seconds) * 1000))
}

function step(frames: number) {
  const el = cur.value.el
  if (!el) return
  el.pause()
  el.currentTime = Math.max(0, el.currentTime + frames / 15)
}

defineExpose({ playAt, toggle, skip, step, playing, current: cur })
</script>

<template>
  <div class="relative h-full w-full bg-[var(--video-bg)]">
    <video
      v-for="(s, i) in slots"
      :key="i"
      :ref="(el) => (s.el = el as HTMLVideoElement | null)"
      class="absolute inset-0 h-full w-full object-contain"
      :class="i === active ? 'visible' : 'invisible'"
      :muted="muted || i !== active"
      playsinline
      preload="auto"
      :aria-hidden="i !== active"
      @ended="i === active && onEnded()"
      @play="onPlayState(i as 0 | 1, true)"
      @pause="onPlayState(i as 0 | 1, false)"
    />
    <div v-if="loading" class="absolute inset-0 grid place-items-center">
      <div class="size-10 animate-spin rounded-full border-4 border-white/20 border-t-white" role="status" aria-label="Memuat rekaman" />
    </div>
    <div v-if="message || unsupported" class="absolute inset-0 grid place-items-center p-6 text-center text-sm text-white">
      <div class="max-w-sm rounded-lg bg-black/70 p-4">
        <p v-if="unsupported">
          Browser ini tidak bisa memutar video HEVC dari kamera ini. Gunakan Chrome/Edge (dengan dukungan hardware) atau Safari, atau
          <a :href="`${cur.rec!.url}?download=1`" class="font-semibold underline">unduh segmen ini</a>.
        </p>
        <p v-else>{{ message }}</p>
      </div>
    </div>
  </div>
</template>
