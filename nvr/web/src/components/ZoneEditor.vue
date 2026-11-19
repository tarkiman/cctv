<script setup lang="ts">
// Per-camera "Zona deteksi": polygons drawn on the latest snapshot. "Dalam pagar"
// (inside the fence: people entering) and "Depan pagar" (right outside it: loitering).
// With zones set, motion elsewhere (e.g. the street) is ignored. Points are stored
// relative to the picture (0..1). Click the picture to add a point to the selected zone,
// drag a point to move it.
import { computed, ref } from 'vue'
import { ChevronDown, LoaderCircle, Plus, RefreshCw, Trash2, Undo2 } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { Camera, Zone, ZoneKind } from '../api/types'
import { useToast } from '../stores/toast'

const props = defineProps<{ camera: Camera }>()
const toast = useToast()

// The SVG uses a 16:9 coordinate space matching the snapshot box.
const VW = 160
const VH = 90

const open = ref(false)
const loading = ref(false)
const saving = ref(false)
const zones = ref<Zone[]>([])
const saved = ref('')
const active = ref(-1)
const bust = ref(Date.now())
const svg = ref<SVGSVGElement | null>(null)
let drag: { zone: number; point: number } | null = null

const kinds: Record<ZoneKind, { label: string; color: string; hint: string }> = {
  inside: { label: 'Dalam pagar', color: '#22c55e', hint: 'Lantai/area tempat orang berdiri setelah masuk pagar.' },
  outside: { label: 'Depan pagar', color: '#f59e0b', hint: 'Jalan tepat di depan pagar, untuk orang yang mondar-mandir.' },
}

const dirty = computed(() => JSON.stringify(zones.value) !== saved.value)
const current = computed(() => zones.value[active.value])

async function toggle() {
  open.value = !open.value
  if (!open.value || saved.value) return
  loading.value = true
  try {
    zones.value = await api.cameraZones(props.camera.id)
    saved.value = JSON.stringify(zones.value)
    active.value = zones.value.length ? 0 : -1
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Zona tidak bisa dimuat.')
  } finally {
    loading.value = false
  }
}

function addZone(kind: ZoneKind) {
  const n = zones.value.filter((z) => z.kind === kind).length
  zones.value.push({ kind, name: kinds[kind].label + (n ? ` ${n + 1}` : ''), points: [] })
  active.value = zones.value.length - 1
}

function removeZone() {
  if (active.value < 0) return
  zones.value.splice(active.value, 1)
  active.value = zones.value.length ? Math.min(active.value, zones.value.length - 1) : -1
}

function undoPoint() {
  current.value?.points.pop()
}

// Pointer position as picture-relative coordinates.
function rel(ev: PointerEvent): [number, number] {
  const r = svg.value!.getBoundingClientRect()
  const x = Math.min(1, Math.max(0, (ev.clientX - r.left) / r.width))
  const y = Math.min(1, Math.max(0, (ev.clientY - r.top) / r.height))
  return [Math.round(x * 10000) / 10000, Math.round(y * 10000) / 10000]
}

function onPictureDown(ev: PointerEvent) {
  if (drag) return
  if (!current.value) {
    toast.error('Pilih atau tambah zona dulu.')
    return
  }
  if (current.value.points.length >= 32) return
  current.value.points.push(rel(ev))
}

function onPointDown(ev: PointerEvent, zone: number, point: number) {
  ev.stopPropagation()
  active.value = zone
  drag = { zone, point }
  ;(ev.target as Element).setPointerCapture(ev.pointerId)
}

function onPointMove(ev: PointerEvent) {
  if (!drag) return
  zones.value[drag.zone]!.points[drag.point] = rel(ev)
}

function onPointUp() {
  drag = null
}

const poly = (z: Zone) => z.points.map(([x, y]) => `${x * VW},${y * VH}`).join(' ')

async function save() {
  const bad = zones.value.find((z) => z.points.length < 3 || !z.name.trim())
  if (bad) {
    toast.error(`Zona "${bad.name || 'tanpa nama'}" perlu nama dan minimal 3 titik.`)
    return
  }
  saving.value = true
  try {
    zones.value = await api.setCameraZones(props.camera.id, zones.value)
    saved.value = JSON.stringify(zones.value)
    toast.ok(`Zona ${props.camera.name} disimpan. Deteksi memakainya dalam ±30 detik.`)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menyimpan zona.')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="mt-4 border-t border-line pt-3">
    <button type="button" class="flex w-full items-center gap-2 text-sm font-semibold" :aria-expanded="open" @click="toggle">
      Zona deteksi
      <span v-if="saved && zones.length" class="text-xs font-normal text-muted">{{ zones.length }} zona</span>
      <ChevronDown class="ml-auto size-4 transition-transform" :class="open && 'rotate-180'" aria-hidden="true" />
    </button>

    <div v-if="open" class="mt-3 space-y-3">
      <div v-if="loading" class="flex items-center gap-2 text-sm text-muted"><LoaderCircle class="size-4 animate-spin" /> Memuat…</div>
      <template v-else>
        <p class="text-xs text-muted">
          Gerakan di luar semua zona diabaikan. Klik gambar untuk menambah titik pada zona terpilih, seret titik untuk memindahkannya. Untuk
          <b>Dalam pagar</b>, tandai lantai tempat orang berdiri: yang dihitung adalah posisi kakinya.
        </p>

        <div class="relative aspect-video overflow-hidden rounded-lg bg-[var(--video-bg)] select-none">
          <img :src="api.snapshotUrl(camera.id, bust)" alt="" class="absolute inset-0 h-full w-full object-fill" draggable="false" />
          <svg
            ref="svg"
            :viewBox="`0 0 ${VW} ${VH}`"
            preserveAspectRatio="none"
            class="absolute inset-0 h-full w-full cursor-crosshair touch-none"
            role="img"
            :aria-label="`Zona deteksi ${camera.name}`"
            @pointerdown="onPictureDown"
            @pointermove="onPointMove"
            @pointerup="onPointUp"
            @pointercancel="onPointUp"
          >
            <g v-for="(z, zi) in zones" :key="zi">
              <polygon
                v-if="z.points.length >= 3"
                :points="poly(z)"
                :fill="kinds[z.kind].color"
                :fill-opacity="zi === active ? 0.35 : 0.22"
                :stroke="kinds[z.kind].color"
                :stroke-width="zi === active ? 3 : 2"
                vector-effect="non-scaling-stroke"
              />
              <polyline
                v-else-if="z.points.length === 2"
                :points="poly(z)"
                fill="none"
                :stroke="kinds[z.kind].color"
                stroke-width="2"
                vector-effect="non-scaling-stroke"
              />
              <template v-if="zi === active">
                <ellipse
                  v-for="(p, pi) in z.points"
                  :key="pi"
                  :cx="p[0] * VW"
                  :cy="p[1] * VH"
                  rx="1.6"
                  ry="1.6"
                  fill="#fff"
                  :stroke="kinds[z.kind].color"
                  stroke-width="2"
                  vector-effect="non-scaling-stroke"
                  class="cursor-move"
                  @pointerdown="onPointDown($event, zi, pi)"
                />
              </template>
            </g>
          </svg>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <button
            v-for="(z, zi) in zones"
            :key="zi"
            type="button"
            class="flex min-h-9 items-center gap-1.5 rounded-full border px-3 text-xs font-semibold"
            :class="zi === active ? 'border-transparent bg-surface-2' : 'border-line'"
            :aria-pressed="zi === active"
            @click="active = zi"
          >
            <span class="size-2.5 rounded-full" :style="{ background: kinds[z.kind].color }" aria-hidden="true" />
            {{ z.name || kinds[z.kind].label }}
            <span class="font-normal text-muted">{{ z.points.length }} titik</span>
          </button>
          <button
            v-for="(k, kind) in kinds"
            :key="kind"
            type="button"
            class="flex min-h-9 items-center gap-1 rounded-full border border-dashed border-line px-3 text-xs font-semibold hover:bg-surface-2 disabled:opacity-40"
            :disabled="zones.length >= 8"
            @click="addZone(kind)"
          >
            <Plus class="size-3.5" aria-hidden="true" /> {{ k.label }}
          </button>
        </div>

        <div v-if="current" class="space-y-2 rounded-lg bg-surface-2 p-3">
          <div>
            <label class="mb-1 block text-xs font-medium" :for="`zname-${camera.id}`">Nama zona ({{ kinds[current.kind].label }})</label>
            <input
              :id="`zname-${camera.id}`"
              v-model="current.name"
              maxlength="40"
              class="min-h-9 w-full rounded-lg border border-line bg-surface px-2 text-sm"
            />
            <p class="mt-1 text-xs text-muted">{{ kinds[current.kind].hint }}</p>
          </div>
          <div class="flex flex-wrap gap-1">
            <button
              type="button"
              class="flex min-h-9 items-center gap-1 rounded-lg px-2 text-xs font-semibold hover:bg-surface disabled:opacity-40"
              :disabled="!current.points.length"
              @click="undoPoint"
            >
              <Undo2 class="size-4" aria-hidden="true" /> Hapus titik terakhir
            </button>
            <button type="button" class="flex min-h-9 items-center gap-1 rounded-lg px-2 text-xs font-semibold text-danger hover:bg-surface" @click="removeZone">
              <Trash2 class="size-4" aria-hidden="true" /> Hapus zona
            </button>
          </div>
        </div>

        <div class="flex items-center justify-between gap-2">
          <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-lg px-2 text-xs font-semibold hover:bg-surface-2" @click="bust = Date.now()">
            <RefreshCw class="size-4" aria-hidden="true" /> Muat ulang gambar
          </button>
          <button
            type="button"
            class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary disabled:opacity-50"
            :disabled="!dirty || saving"
            @click="save"
          >
            <LoaderCircle v-if="saving" class="size-4 animate-spin" aria-hidden="true" />
            Simpan zona
          </button>
        </div>
      </template>
    </div>
  </div>
</template>
