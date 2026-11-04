<script setup lang="ts">
// Export a clip: pick a start (defaults to the playhead) and a duration; the server
// stream-copies the matching segments into one MP4, so it is fast and lossless.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Download, LoaderCircle, Scissors } from 'lucide-vue-next'
import { api } from '../api/client'
import type { ExportEstimate } from '../api/types'
import { fmtBytes, fmtDateLong, fmtTimeSec, wibDate } from '../utils/format'

const props = defineProps<{ cameraId: number; cameraName: string; playhead: Date | null; now: number }>()
const emit = defineEmits<{ selection: [range: { start: Date; end: Date } | null] }>()

const DURATIONS = [
  { s: 30, label: '30 dtk' },
  { s: 60, label: '1 mnt' },
  { s: 300, label: '5 mnt' },
  { s: 600, label: '10 mnt' },
  { s: 1800, label: '30 mnt' },
  { s: 3600, label: '1 jam' },
]

const open = ref(false)
const start = ref<Date>(new Date(props.now - 5 * 60_000))
const duration = ref(60)
const estimate = ref<ExportEstimate | null>(null)
const estimating = ref(false)
const preparing = ref(false)
let seq = 0
let debounce: number | undefined
let prepTimer: number | undefined

const end = computed(() => new Date(Math.min(start.value.getTime() + duration.value * 1000, props.now)))
const valid = computed(() => end.value.getTime() - start.value.getTime() >= 1000)
const iso = (d: Date) => d.toISOString().replace(/\.\d+Z$/, 'Z')
const href = computed(() => api.exportUrl(props.cameraId, iso(start.value), iso(end.value)))
const crossesDay = computed(() => wibDate(start.value) !== wibDate(end.value))

function fromPlayhead() {
  if (props.playhead) start.value = new Date(Math.floor(props.playhead.getTime() / 1000) * 1000)
}

function onTime(e: Event) {
  const v = (e.target as HTMLInputElement).value // HH:MM or HH:MM:SS
  if (!v) return
  const t = new Date(`${wibDate(start.value)}T${v.length === 5 ? `${v}:00` : v}+07:00`)
  if (!Number.isNaN(t.getTime())) start.value = new Date(Math.min(t.getTime(), props.now - 1000))
}

function toggle() {
  open.value = !open.value
  if (open.value) fromPlayhead()
}

async function refreshEstimate() {
  if (!open.value || !valid.value) {
    estimate.value = null
    return
  }
  const my = ++seq
  estimating.value = true
  try {
    const e = await api.exportEstimate(props.cameraId, iso(start.value), iso(end.value))
    if (my === seq) estimate.value = e
  } catch {
    if (my === seq) estimate.value = null
  } finally {
    if (my === seq) estimating.value = false
  }
}

watch(
  () => [open.value, start.value.getTime(), end.value.getTime()],
  () => {
    emit('selection', open.value && valid.value ? { start: start.value, end: end.value } : null)
    clearTimeout(debounce)
    debounce = window.setTimeout(refreshEstimate, 250)
  },
  { immediate: true },
)

function onDownload() {
  // The server needs a few seconds to assemble the clip before the download starts.
  preparing.value = true
  clearTimeout(prepTimer)
  prepTimer = window.setTimeout(() => (preparing.value = false), 8000)
}

onBeforeUnmount(() => {
  clearTimeout(debounce)
  clearTimeout(prepTimer)
  emit('selection', null)
})

const fmtSecs = (s: number) => (s >= 60 ? `${Math.floor(s / 60)} mnt${s % 60 ? ` ${Math.round(s % 60)} dtk` : ''}` : `${Math.round(s)} dtk`)
</script>

<template>
  <div class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
    <button type="button" class="flex w-full items-center gap-2 text-sm font-semibold" :aria-expanded="open" @click="toggle">
      <Scissors class="size-4" aria-hidden="true" /> Export klip
      <span class="ml-auto text-xs font-normal text-muted">{{ open ? 'Tutup' : 'Unduh potongan video' }}</span>
    </button>

    <div v-if="open" class="mt-3 space-y-3 text-sm">
      <div>
        <label class="mb-1 block text-xs text-muted" for="export-start">Mulai ({{ fmtDateLong(start) }}, WIB)</label>
        <div class="flex gap-2">
          <input
            id="export-start"
            type="time"
            step="1"
            class="min-h-10 min-w-0 flex-1 rounded-lg border border-line bg-surface px-3 tabular"
            :value="fmtTimeSec(start)"
            @change="onTime"
          />
          <button
            type="button"
            class="min-h-10 rounded-lg border border-line px-3 text-xs font-semibold hover:bg-surface-2 disabled:opacity-40"
            :disabled="!playhead"
            title="Pakai posisi pemutaran saat ini"
            @click="fromPlayhead"
          >
            Posisi putar
          </button>
        </div>
      </div>

      <div>
        <p class="mb-1 text-xs text-muted">Durasi</p>
        <div class="grid grid-cols-3 gap-1.5" role="radiogroup" aria-label="Durasi klip">
          <button
            v-for="d in DURATIONS"
            :key="d.s"
            type="button"
            role="radio"
            class="min-h-9 rounded-md border text-xs font-semibold"
            :class="duration === d.s ? 'border-primary bg-primary text-on-primary' : 'border-line hover:bg-surface-2'"
            :aria-checked="duration === d.s"
            @click="duration = d.s"
          >
            {{ d.label }}
          </button>
        </div>
      </div>

      <p class="rounded-lg bg-surface-2 px-3 py-2 text-xs tabular">
        {{ fmtTimeSec(start) }} – {{ fmtTimeSec(end) }} WIB<span v-if="crossesDay"> (lewat tengah malam)</span>
        <template v-if="end.getTime() < start.getTime() + duration * 1000"><br />Dipotong sampai saat ini.</template>
      </p>

      <div class="min-h-10 text-xs" aria-live="polite">
        <p v-if="estimating" class="text-muted">Menghitung…</p>
        <template v-else-if="estimate">
          <p v-if="!estimate.segments" class="text-warn">Tidak ada rekaman pada rentang ini.</p>
          <template v-else>
            <p>
              <span class="font-semibold">≈ {{ fmtBytes(estimate.bytes) }}</span>
              · {{ fmtSecs(estimate.seconds) }} rekaman · kualitas asli
            </p>
            <p v-if="estimate.gap_seconds >= 5" class="mt-0.5 text-warn">{{ fmtSecs(estimate.gap_seconds) }} tanpa rekaman akan dilewati.</p>
          </template>
        </template>
      </div>

      <a
        v-if="valid && estimate?.segments"
        :href="href"
        download
        class="flex min-h-11 items-center justify-center gap-2 rounded-lg bg-primary px-4 font-semibold text-on-primary"
        @click="onDownload"
      >
        <LoaderCircle v-if="preparing" class="size-4 animate-spin" aria-hidden="true" />
        <Download v-else class="size-4" aria-hidden="true" />
        {{ preparing ? 'Menyiapkan klip…' : 'Unduh klip' }}
      </a>
      <button v-else type="button" disabled class="flex min-h-11 w-full items-center justify-center gap-2 rounded-lg bg-surface-2 px-4 font-semibold text-muted">
        <Download class="size-4" aria-hidden="true" /> Unduh klip
      </button>
      <p class="text-[11px] leading-snug text-muted">
        Klip dimulai dari keyframe terdekat sebelum waktu mulai (selisih ≤ 2 detik). Unduhan dimulai otomatis setelah klip siap.
      </p>
    </div>
  </div>
</template>
