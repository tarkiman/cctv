<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import {
  ArrowLeft,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Download,
  Keyboard,
  Pause,
  Play,
  Radio,
  RotateCcw,
  RotateCw,
  TriangleAlert,
  VideoOff,
  Volume2,
  VolumeX,
} from 'lucide-vue-next'
import { api } from '../api/client'
import type { CameraEvent, Recording, Timeline as TimelineData } from '../api/types'
import { useCameras } from '../stores/cameras'
import { canPlayCodec, useHotkeys, useNow } from '../composables/media'
import { addDays, fmtDateLong, fmtRelative, fmtTime, fmtTimeSec, wibDate, wibMidnight } from '../utils/format'
import LivePlayer from '../components/LivePlayer.vue'
import RecordingPlayer from '../components/RecordingPlayer.vue'
import StatusBadge from '../components/StatusBadge.vue'
import Timeline from '../components/Timeline.vue'
import ExportPanel from '../components/ExportPanel.vue'
import StateMessage from '../components/StateMessage.vue'

const route = useRoute()
const router = useRouter()
const cams = useCameras()
const now = useNow(15_000)

const cam = computed(() => cams.bySlug.get(String(route.params.slug)))
const mode = computed<'live' | 'rekaman'>(() => (route.query.mode === 'rekaman' ? 'rekaman' : 'live'))
const quality = ref<'main' | 'sub'>(route.query.q === 'sub' ? 'sub' : 'main')

// --- live ---
const liveCodecOk = computed(() => quality.value === 'sub' || canPlayCodec(cam.value?.codec))
const liveState = ref<'connecting' | 'playing' | 'error'>('connecting')
const videoBox = ref<HTMLElement | null>(null)

// --- recordings ---
const date = ref(wibDate(new Date()))
const timeline = ref<TimelineData | null>(null)
const events = ref<CameraEvent[]>([])
const tlLoading = ref(false)
const player = ref<InstanceType<typeof RecordingPlayer> | null>(null)
const playhead = ref<Date | null>(null)
const playing = ref(false)
const segment = ref<Recording | null>(null)
const rate = ref(1)
const showKeys = ref(false)
// Sound starts muted (browsers block autoplay with sound); one click turns it on for
// live and recordings alike.
const muted = ref(true)
const liveAudio = ref(false)
const exportSel = ref<{ start: Date; end: Date } | null>(null)
const today = computed(() => wibDate(new Date(now.value)))
const dayStart = computed(() => wibMidnight(date.value))

function setMode(m: 'live' | 'rekaman') {
  router.replace({ query: { ...route.query, mode: m === 'live' ? undefined : m } })
}

watch(quality, (q) => router.replace({ query: { ...route.query, q: q === 'sub' ? 'sub' : undefined } }))

async function loadTimeline() {
  if (!cam.value) return
  tlLoading.value = true
  try {
    const from = wibMidnight(date.value)
    const to = new Date(from.getTime() + 86400_000)
    const iso = (d: Date) => d.toISOString().replace(/\.\d+Z$/, 'Z')
    const [tl, ev] = await Promise.all([
      api.timeline(cam.value.id, date.value),
      api.events(cam.value.id, iso(from), iso(to)).catch(() => [] as CameraEvent[]),
    ])
    timeline.value = tl
    events.value = ev
  } catch {
    timeline.value = null
    events.value = []
  } finally {
    tlLoading.value = false
  }
}

async function seek(t: Date) {
  if (wibDate(t) !== date.value) {
    date.value = wibDate(t)
    loadTimeline()
  }
  playhead.value = t
  router.replace({ query: { ...route.query, mode: 'rekaman', t: t.toISOString().replace(/\.\d+Z$/, 'Z') } })
  await nextTick()
  player.value?.playAt(t)
}

// User picked another day: load it and jump to its first footage.
async function goToDay(d: string) {
  if (d > today.value || d === date.value) return
  date.value = d
  await loadTimeline()
  const first = timeline.value?.ranges[0]
  if (first) seek(new Date(first.start))
}

const changeDay = (n: number) => goToDay(addDays(date.value, n))

function onDateInput(e: Event) {
  const v = (e.target as HTMLInputElement).value
  if (v) goToDay(v)
}

async function enterRecordings() {
  const t = typeof route.query.t === 'string' ? new Date(route.query.t) : null
  if (t && !Number.isNaN(t.getTime())) {
    date.value = wibDate(t)
    await loadTimeline()
    seek(t)
    return
  }
  date.value = today.value
  await loadTimeline()
  // Default: five minutes ago, the most common "what just happened?" question.
  seek(new Date(Date.now() - 5 * 60_000))
}

watch(
  () => [mode.value, cam.value?.id] as const,
  ([m, id]) => {
    if (m === 'rekaman' && id) enterRecordings()
  },
  { immediate: true },
)

// Keep the timeline fresh while watching today.
watch(
  () => cams.byId.get(cam.value?.id ?? -1)?.status.last_segment_at,
  () => mode.value === 'rekaman' && date.value === today.value && loadTimeline(),
)

const rates = [0.5, 1, 2, 4, 8]
const downloadUrl = computed(() => (segment.value ? `${segment.value.url}?download=1` : null))

function fullscreen() {
  const el = videoBox.value
  if (!el) return
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
  else el.requestFullscreen().catch(() => {})
}

useHotkeys({
  ' ': () => mode.value === 'rekaman' && player.value?.toggle(),
  ArrowLeft: () => mode.value === 'rekaman' && player.value?.skip(-10),
  ArrowRight: () => mode.value === 'rekaman' && player.value?.skip(10),
  'Shift+ArrowLeft': () => mode.value === 'rekaman' && player.value?.skip(-60),
  'Shift+ArrowRight': () => mode.value === 'rekaman' && player.value?.skip(60),
  ',': () => mode.value === 'rekaman' && player.value?.step(-1),
  '.': () => mode.value === 'rekaman' && player.value?.step(1),
  '[': () => (rate.value = rates[Math.max(0, rates.indexOf(rate.value) - 1)]!),
  ']': () => (rate.value = rates[Math.min(rates.length - 1, rates.indexOf(rate.value) + 1)]!),
  l: () => setMode('live'),
  r: () => setMode('rekaman'),
  f: fullscreen,
  m: () => (muted.value = !muted.value),
  '?': () => (showKeys.value = !showKeys.value),
})

onMounted(() => {
  if (!cams.loaded) cams.load()
})
</script>

<template>
  <div v-if="!cam && cams.loaded">
    <StateMessage :icon="VideoOff" title="Kamera tidak ditemukan" text="Kamera ini mungkin sudah dihapus atau namanya berubah.">
      <RouterLink to="/" class="rounded-lg bg-primary px-4 py-2 font-semibold text-on-primary">Kembali ke Live</RouterLink>
    </StateMessage>
  </div>

  <div v-else-if="cam">
    <header class="mb-4 flex flex-wrap items-center gap-3">
      <RouterLink to="/" class="grid size-10 place-items-center rounded-lg hover:bg-surface-2" aria-label="Kembali ke Live">
        <ArrowLeft class="size-5" />
      </RouterLink>
      <div class="mr-auto min-w-0">
        <h1 class="truncate text-xl font-bold">{{ cam.name }}</h1>
        <StatusBadge :state="cam.status.state" />
      </div>

      <div class="flex overflow-hidden rounded-lg border border-line" role="tablist" aria-label="Mode tampilan">
        <button
          v-for="m in (['live', 'rekaman'] as const)"
          :key="m"
          type="button"
          role="tab"
          class="min-h-10 px-4 text-sm font-semibold"
          :class="mode === m ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
          :aria-selected="mode === m"
          @click="setMode(m)"
        >
          {{ m === 'live' ? 'Live' : 'Rekaman' }}
        </button>
      </div>

      <div v-if="mode === 'live' && cam.has_sub_stream" class="flex overflow-hidden rounded-lg border border-line" role="group" aria-label="Kualitas">
        <button
          v-for="q in (['main', 'sub'] as const)"
          :key="q"
          type="button"
          class="min-h-10 px-3 text-sm font-semibold"
          :class="quality === q ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
          :aria-pressed="quality === q"
          :title="q === 'main' ? 'Stream utama (resolusi penuh)' : 'Sub-stream (ringan)'"
          @click="quality = q"
        >
          {{ q === 'main' ? 'HD' : 'SD' }}
        </button>
      </div>
    </header>

    <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
      <section class="min-w-0">
        <div ref="videoBox" class="relative aspect-video overflow-hidden rounded-[var(--radius-card)] bg-[var(--video-bg)]" @dblclick="fullscreen">
          <template v-if="mode === 'live'">
            <LivePlayer
              v-if="liveCodecOk"
              :camera-id="cam.id"
              :quality="quality"
              :muted="muted"
              @state="liveState = $event"
              @audio="liveAudio = $event"
            >
              <template #default="{ state, error }">
                <div v-if="state === 'connecting'" class="absolute inset-0 grid place-items-center">
                  <div class="size-10 animate-spin rounded-full border-4 border-white/20 border-t-white" role="status" aria-label="Menghubungkan" />
                </div>
                <div v-else-if="state === 'error'" class="absolute inset-0 grid place-items-center p-6 text-center text-sm text-white">
                  <p class="max-w-sm rounded-lg bg-black/70 p-4">{{ error }}</p>
                </div>
              </template>
            </LivePlayer>
            <div v-else class="absolute inset-0 grid place-items-center p-6 text-center text-sm text-white">
              <div class="max-w-sm rounded-lg bg-black/70 p-4">
                <TriangleAlert class="mx-auto mb-2 size-6 text-amber-300" aria-hidden="true" />
                <p>Browser ini tidak bisa memutar video HEVC dari stream utama kamera ini.</p>
                <button v-if="cam.has_sub_stream" type="button" class="mt-3 rounded-lg bg-white px-3 py-1.5 font-semibold text-black" @click="quality = 'sub'">
                  Pakai kualitas SD
                </button>
                <p v-else class="mt-2 text-white/80">Gunakan Chrome/Edge dengan dukungan hardware, atau Safari.</p>
              </div>
            </div>
            <span class="absolute top-3 left-3 flex items-center gap-1.5 rounded bg-red-600 px-2 py-0.5 text-xs font-bold text-white">
              <Radio class="size-3.5" aria-hidden="true" /> LIVE
            </span>
            <button
              v-if="liveAudio && liveState === 'playing'"
              type="button"
              class="absolute top-2 right-2 grid size-10 place-items-center rounded-full bg-black/60 text-white hover:bg-black/80"
              :aria-label="muted ? 'Nyalakan suara' : 'Matikan suara'"
              :aria-pressed="!muted"
              :title="muted ? 'Nyalakan suara (M)' : 'Matikan suara (M)'"
              @click="muted = !muted"
            >
              <VolumeX v-if="muted" class="size-5" aria-hidden="true" />
              <Volume2 v-else class="size-5" aria-hidden="true" />
            </button>
          </template>

          <RecordingPlayer
            v-else
            ref="player"
            :camera-id="cam.id"
            :rate="rate"
            :muted="muted"
            @time="playhead = $event"
            @state="playing = $event"
            @segment="segment = $event"
          />
        </div>

        <!-- Playback controls -->
        <div v-if="mode === 'rekaman'" class="mt-3 space-y-3">
          <div class="flex flex-wrap items-center gap-2">
            <div class="flex items-center gap-1">
              <button type="button" class="grid size-10 place-items-center rounded-lg hover:bg-surface-2" aria-label="Hari sebelumnya" @click="changeDay(-1)">
                <ChevronLeft class="size-5" />
              </button>
              <label class="relative flex min-h-10 items-center gap-2 rounded-lg border border-line px-3 text-sm font-medium">
                <CalendarDays class="size-4 text-muted" aria-hidden="true" />
                <span class="tabular">{{ fmtDateLong(dayStart) }}</span>
                <input type="date" class="absolute inset-0 cursor-pointer opacity-0" :value="date" :max="today" aria-label="Pilih tanggal" @change="onDateInput" />
              </label>
              <button
                type="button"
                class="grid size-10 place-items-center rounded-lg hover:bg-surface-2 disabled:opacity-40"
                aria-label="Hari berikutnya"
                :disabled="date >= today"
                @click="changeDay(1)"
              >
                <ChevronRight class="size-5" />
              </button>
            </div>

            <div class="mx-auto flex items-center gap-1">
              <button type="button" class="grid size-10 place-items-center rounded-lg hover:bg-surface-2" aria-label="Mundur 10 detik" @click="player?.skip(-10)">
                <RotateCcw class="size-5" />
              </button>
              <button
                type="button"
                class="grid size-12 place-items-center rounded-full bg-primary text-on-primary"
                :aria-label="playing ? 'Jeda' : 'Putar'"
                @click="player?.toggle()"
              >
                <Pause v-if="playing" class="size-6" />
                <Play v-else class="size-6" />
              </button>
              <button type="button" class="grid size-10 place-items-center rounded-lg hover:bg-surface-2" aria-label="Maju 10 detik" @click="player?.skip(10)">
                <RotateCw class="size-5" />
              </button>
            </div>

            <div class="flex items-center gap-1">
              <button
                type="button"
                class="grid size-10 place-items-center rounded-lg hover:bg-surface-2"
                :aria-label="muted ? 'Nyalakan suara' : 'Matikan suara'"
                :aria-pressed="!muted"
                :title="muted ? 'Nyalakan suara (M)' : 'Matikan suara (M)'"
                @click="muted = !muted"
              >
                <VolumeX v-if="muted" class="size-5" aria-hidden="true" />
                <Volume2 v-else class="size-5" aria-hidden="true" />
              </button>
              <select v-model.number="rate" class="min-h-10 rounded-lg border border-line bg-surface px-2 text-sm font-semibold" aria-label="Kecepatan putar">
                <option v-for="r in rates" :key="r" :value="r">{{ r }}×</option>
              </select>
              <a
                v-if="downloadUrl"
                :href="downloadUrl"
                class="grid size-10 place-items-center rounded-lg hover:bg-surface-2"
                aria-label="Unduh segmen ini"
                title="Unduh segmen 10 menit ini"
              >
                <Download class="size-5" />
              </a>
              <button type="button" class="flex min-h-10 items-center gap-1.5 rounded-lg px-3 text-sm font-semibold hover:bg-surface-2" @click="setMode('live')">
                <Radio class="size-4 text-danger" aria-hidden="true" /> Live
              </button>
            </div>
          </div>

          <Timeline :day-start="dayStart" :ranges="timeline?.ranges ?? []" :playhead="playhead" :now="now" :loading="tlLoading" :selection="exportSel" :events="events" @seek="seek" />
          <p v-if="!tlLoading && timeline && !timeline.ranges.length" class="text-sm text-muted">Tidak ada rekaman pada tanggal ini.</p>
        </div>
      </section>

      <!-- Side panel -->
      <aside class="space-y-4">
        <div class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <h2 class="mb-3 text-sm font-semibold">Info kamera</h2>
          <p v-if="cam.status.message" class="mb-3 rounded-lg bg-warn/10 p-2.5 text-sm text-warn" role="status">{{ cam.status.message }}</p>
          <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt class="text-muted">Status</dt>
            <dd><StatusBadge :state="cam.status.state" /></dd>
            <dt class="text-muted">Segmen terakhir</dt>
            <dd :title="cam.status.last_segment_at ?? ''">{{ fmtRelative(cam.status.last_segment_at, now) }}</dd>
            <dt class="text-muted">Video</dt>
            <dd class="tabular">{{ cam.codec ? `${cam.codec.toUpperCase()} ${cam.width}×${cam.height}` : '—' }}</dd>
            <dt class="text-muted">Deteksi</dt>
            <dd>
              <span v-if="cam.status.detect === 'off'" class="text-muted">Mati</span>
              <span v-else-if="cam.status.detect === 'error'" class="text-danger" :title="cam.status.detect_error">Bermasalah</span>
              <span v-else-if="cam.status.motion" class="font-semibold text-amber-500">Ada gerak sekarang</span>
              <span v-else>Aktif<template v-if="cam.status.last_motion_at"> · gerak {{ fmtRelative(cam.status.last_motion_at, now) }}</template></span>
            </dd>
            <dt class="text-muted">Suara</dt>
            <dd>
              <span v-if="!cam.record_audio || cam.status.audio === 'off'" class="text-muted">Tidak direkam</span>
              <span v-else-if="cam.status.audio === 'on'">Direkam</span>
              <span v-else-if="cam.status.audio === 'none'" class="text-muted">Kamera tidak mengirim suara</span>
              <span v-else-if="cam.status.audio === 'unavailable'" class="text-warn" title="Format audio kamera tidak bisa dibaca; video tetap direkam.">
                Tidak bisa dibaca
              </span>
              <span v-else class="text-muted">Menunggu segmen pertama</span>
            </dd>
            <dt class="text-muted">Retensi</dt>
            <dd>{{ cam.record_enabled ? `${cam.retention_days} hari` : 'Tidak merekam' }}</dd>
          </dl>
        </div>

        <div v-if="mode === 'rekaman'" class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <h2 class="mb-3 text-sm font-semibold">Rentang rekaman</h2>
          <ul v-if="timeline?.ranges.length" class="max-h-64 space-y-1 overflow-y-auto text-sm">
            <li v-for="(r, i) in timeline.ranges" :key="i">
              <button type="button" class="w-full rounded-md px-2 py-1.5 text-left tabular hover:bg-surface-2" @click="seek(new Date(r.start))">
                {{ fmtTime(new Date(r.start)) }} – {{ fmtTime(new Date(r.end)) }}
              </button>
            </li>
          </ul>
          <p v-else class="text-sm text-muted">Belum ada.</p>
          <p v-if="playhead" class="mt-3 text-xs text-muted tabular">Posisi: {{ fmtTimeSec(playhead) }} WIB</p>
        </div>

        <div v-if="mode === 'rekaman' && events.length" class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <h2 class="mb-1 text-sm font-semibold">Aktivitas gerak</h2>
          <p class="mb-2 text-xs text-muted">{{ events.length }} kejadian pada tanggal ini · penanda kuning di timeline</p>
          <ul class="max-h-56 space-y-0.5 overflow-y-auto text-sm">
            <li v-for="ev in [...events].reverse()" :key="ev.id">
              <button type="button" class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left tabular hover:bg-surface-2" @click="seek(new Date(new Date(ev.start).getTime() - 3000))">
                <span class="size-2 shrink-0 rounded-full" :class="ev.label === 'person' ? 'bg-danger' : 'bg-amber-400'" aria-hidden="true" />
                {{ fmtTimeSec(new Date(ev.start)) }}
                <span class="ml-auto text-xs text-muted">{{ ev.end ? `${Math.max(1, Math.round((new Date(ev.end).getTime() - new Date(ev.start).getTime()) / 1000))} dtk` : 'berlangsung' }}</span>
              </button>
            </li>
          </ul>
        </div>

        <ExportPanel
          v-if="mode === 'rekaman'"
          :camera-id="cam.id"
          :camera-name="cam.name"
          :playhead="playhead"
          :now="now"
          @selection="exportSel = $event"
        />

        <div class="hidden rounded-[var(--radius-card)] border border-line bg-surface p-4 xl:block">
          <button type="button" class="flex w-full items-center gap-2 text-sm font-semibold" :aria-expanded="showKeys" @click="showKeys = !showKeys">
            <Keyboard class="size-4" aria-hidden="true" /> Pintasan keyboard
          </button>
          <dl v-if="showKeys" class="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-xs">
            <dt><kbd>Spasi</kbd></dt><dd>Putar / jeda</dd>
            <dt><kbd>← →</kbd></dt><dd>±10 detik</dd>
            <dt><kbd>Shift ← →</kbd></dt><dd>±1 menit</dd>
            <dt><kbd>, .</kbd></dt><dd>Frame sebelum / sesudah</dd>
            <dt><kbd>[ ]</kbd></dt><dd>Kecepatan</dd>
            <dt><kbd>L / R</kbd></dt><dd>Live / Rekaman</dd>
            <dt><kbd>F</kbd></dt><dd>Layar penuh</dd>
            <dt><kbd>M</kbd></dt><dd>Suara nyala / mati</dd>
          </dl>
        </div>
      </aside>
    </div>
  </div>

  <div v-else class="skeleton aspect-video rounded-[var(--radius-card)]" />
</template>

<style scoped>
kbd {
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 0 4px;
  font-family: inherit;
}
</style>
