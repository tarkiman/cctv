<script setup lang="ts">
// 24-hour recording timeline for one WIB day. Filled bars are recorded footage; gaps
// have none. Click or drag to seek; zoom to 6 h / 1 h around the playhead.
import { computed, ref } from 'vue'
import type { TimeRange } from '../api/types'
import { fmtTime, fmtTimeSec } from '../utils/format'

const props = defineProps<{
  dayStart: Date
  ranges: TimeRange[]
  playhead: Date | null
  now: number
  loading?: boolean
}>()
const emit = defineEmits<{ seek: [t: Date] }>()

const ZOOMS = [24, 6, 1] as const
const zoom = ref<(typeof ZOOMS)[number]>(24)
const track = ref<HTMLElement | null>(null)
const hover = ref<{ x: number; t: Date } | null>(null)
const dragging = ref(false)
const dragT = ref<Date | null>(null)

const dayMs = 86400_000
const dayStartMs = computed(() => props.dayStart.getTime())

// Visible window: whole day, or `zoom` hours centered on the playhead (clamped to the day).
const view = computed(() => {
  const span = zoom.value * 3600_000
  if (span >= dayMs) return { start: dayStartMs.value, span: dayMs }
  const center = (dragT.value ?? props.playhead)?.getTime() ?? Math.min(props.now, dayStartMs.value + dayMs)
  const start = Math.max(dayStartMs.value, Math.min(center - span / 2, dayStartMs.value + dayMs - span))
  return { start, span }
})

const pct = (ms: number) => ((ms - view.value.start) / view.value.span) * 100

const bars = computed(() =>
  props.ranges
    .map((r) => {
      const s = Math.max(new Date(r.start).getTime(), view.value.start)
      const e = Math.min(new Date(r.end).getTime(), view.value.start + view.value.span)
      return e > s ? { left: pct(s), width: Math.max(pct(e) - pct(s), 0.15) } : null
    })
    .filter((b): b is { left: number; width: number } => b !== null),
)

const ticks = computed(() => {
  const step = zoom.value === 24 ? 3 * 3600_000 : zoom.value === 6 ? 3600_000 : 10 * 60_000
  const out: { left: number; label: string }[] = []
  // Align ticks to WIB midnight, not to the Unix epoch (which is UTC).
  const first = dayStartMs.value + Math.ceil((view.value.start - dayStartMs.value) / step) * step
  for (let t = first; t <= view.value.start + view.value.span; t += step) {
    out.push({ left: pct(t), label: t === dayStartMs.value + dayMs ? '24:00' : fmtTime(new Date(t)) })
  }
  return out
})

const futureLeft = computed(() => {
  const n = props.now
  if (n >= view.value.start + view.value.span) return null
  return Math.max(0, pct(n))
})

const shown = computed(() => dragT.value ?? props.playhead)
const playheadLeft = computed(() => {
  const p = shown.value?.getTime()
  if (p === undefined || p < view.value.start || p > view.value.start + view.value.span) return null
  return pct(p)
})

function timeAt(clientX: number): Date {
  const rect = track.value!.getBoundingClientRect()
  const f = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
  const t = Math.min(view.value.start + f * view.value.span, props.now)
  return new Date(Math.round(t / 1000) * 1000)
}

function onPointerDown(e: PointerEvent) {
  dragging.value = true
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  dragT.value = timeAt(e.clientX)
}
function onPointerMove(e: PointerEvent) {
  const t = timeAt(e.clientX)
  if (dragging.value) dragT.value = t
  const rect = track.value!.getBoundingClientRect()
  hover.value = { x: e.clientX - rect.left, t }
}
function onPointerUp() {
  if (dragging.value && dragT.value) emit('seek', dragT.value)
  dragging.value = false
  dragT.value = null
}

function onKey(e: KeyboardEvent) {
  const base = props.playhead?.getTime() ?? props.now
  const steps: Record<string, number> = { ArrowLeft: -60_000, ArrowRight: 60_000, PageDown: -600_000, PageUp: 600_000 }
  if (e.key in steps) {
    e.preventDefault()
    e.stopPropagation()
    emit('seek', new Date(Math.min(props.now, Math.max(dayStartMs.value, base + steps[e.key]!))))
  }
}
</script>

<template>
  <div class="select-none">
    <div class="mb-1.5 flex items-center justify-between gap-2 text-xs text-muted">
      <span class="tabular">
        <template v-if="shown">{{ fmtTimeSec(shown) }} WIB</template>
        <template v-else>Pilih waktu di timeline</template>
      </span>
      <div class="flex overflow-hidden rounded-md border border-line" role="group" aria-label="Zoom timeline">
        <button
          v-for="z in ZOOMS"
          :key="z"
          type="button"
          class="px-2.5 py-1 font-semibold"
          :class="zoom === z ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
          :aria-pressed="zoom === z"
          @click="zoom = z"
        >
          {{ z }} jam
        </button>
      </div>
    </div>

    <div
      ref="track"
      class="relative h-12 cursor-pointer touch-none rounded-md border border-line bg-surface-2"
      role="slider"
      tabindex="0"
      aria-label="Timeline rekaman"
      :aria-valuemin="0"
      :aria-valuemax="86400"
      :aria-valuenow="shown ? Math.round((shown.getTime() - dayStartMs) / 1000) : 0"
      :aria-valuetext="shown ? `${fmtTimeSec(shown)} WIB` : 'Belum dipilih'"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
      @pointerleave="hover = null"
      @keydown="onKey"
    >
      <div v-if="loading" class="skeleton absolute inset-0 rounded-md" />
      <div v-for="(b, i) in bars" :key="i" class="absolute top-2 bottom-2 rounded-sm bg-range" :style="{ left: `${b.left}%`, width: `${b.width}%` }" />
      <div
        v-if="futureLeft !== null"
        class="absolute inset-y-0 right-0 rounded-r-md bg-[repeating-linear-gradient(45deg,transparent,transparent_4px,var(--border)_4px,var(--border)_5px)]"
        :style="{ left: `${futureLeft}%` }"
        aria-hidden="true"
      />
      <div v-if="playheadLeft !== null" class="pointer-events-none absolute -top-1 -bottom-1 w-0.5 bg-danger" :style="{ left: `${playheadLeft}%` }">
        <div class="absolute -top-1 -left-[5px] size-3 rounded-full bg-danger" />
      </div>
      <div
        v-if="hover && !dragging"
        class="pointer-events-none absolute -top-8 -translate-x-1/2 rounded bg-fg px-1.5 py-0.5 text-xs whitespace-nowrap text-bg tabular"
        :style="{ left: `${hover.x}px` }"
      >
        {{ fmtTimeSec(hover.t) }}
      </div>
    </div>

    <div class="relative mt-1 h-4 text-[11px] text-muted tabular" aria-hidden="true">
      <span v-for="t in ticks" :key="t.left" class="absolute -translate-x-1/2" :style="{ left: `${t.left}%` }">{{ t.label }}</span>
    </div>
  </div>
</template>
