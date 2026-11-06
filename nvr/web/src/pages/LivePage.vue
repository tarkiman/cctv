<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { CameraOff, Expand, Gauge, LayoutGrid, Minimize, RefreshCw, Square, Columns2, Columns3, PanelLeft } from 'lucide-vue-next'
import { useCameras } from '../stores/cameras'
import { loadPref, savePref } from '../utils/prefs'
import CameraTile from '../components/CameraTile.vue'
import StateMessage from '../components/StateMessage.vue'

type Layout = 'auto' | '1' | '2' | '3' | 'focus'
const cams = useCameras()
const layout = ref<Layout>(loadPref('live.layout', 'auto'))
const saver = ref<boolean>(loadPref('live.saver', false))
const focusId = ref<number | null>(loadPref('live.focus', null))
// 0 = all locations. Tabs only show when the user sees more than one location.
const locationId = ref<number>(loadPref('live.location', 0))
const locationTabs = computed(() => cams.locations.filter((l) => cams.list.some((c) => c.location_id === l.id)))
const shown = computed(() => (locationId.value && locationTabs.value.some((l) => l.id === locationId.value) ? cams.list.filter((c) => c.location_id === locationId.value) : cams.list))
const shownOnline = computed(() => shown.value.filter((c) => c.status.state === 'running').length)
const wall = ref(false)
const wallEl = ref<HTMLElement | null>(null)
const chromeVisible = ref(true)
let hideTimer: number | undefined

watch(layout, (v) => savePref('live.layout', v))
watch(saver, (v) => savePref('live.saver', v))
watch(focusId, (v) => savePref('live.focus', v))
watch(locationId, (v) => savePref('live.location', v))

const layouts: { id: Layout; label: string; icon: unknown }[] = [
  { id: 'auto', label: 'Otomatis', icon: LayoutGrid },
  { id: '1', label: '1 kolom', icon: Square },
  { id: '2', label: '2 kolom', icon: Columns2 },
  { id: '3', label: '3 kolom', icon: Columns3 },
  { id: 'focus', label: 'Fokus', icon: PanelLeft },
]

// Auto: as many 360px+ columns as fit, so wide screens get 3-4 columns and phones get 1.
const gridClass = computed(() => {
  switch (layout.value) {
    case '1':
      return 'grid-cols-1 max-w-5xl mx-auto'
    case '2':
      return 'grid-cols-1 sm:grid-cols-2'
    case '3':
      return 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-3'
    default:
      return 'grid-cols-[repeat(auto-fill,minmax(min(100%,360px),1fr))]'
  }
})

const focused = computed(() => shown.value.find((c) => c.id === focusId.value) ?? shown.value[0])
const others = computed(() => shown.value.filter((c) => c.id !== focused.value?.id))

// Wall mode fills the screen: pick a column count that keeps tiles as large as possible.
const wallCols = computed(() => Math.max(1, Math.ceil(Math.sqrt(shown.value.length))))
const wallRows = computed(() => Math.max(1, Math.ceil(shown.value.length / wallCols.value)))

async function enterWall() {
  wall.value = true
  await new Promise((r) => requestAnimationFrame(r))
  try {
    await wallEl.value?.requestFullscreen()
  } catch {
    /* fullscreen not allowed: wall still fills the viewport */
  }
  poke()
}
function exitWall() {
  wall.value = false
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
}
function onFsChange() {
  if (!document.fullscreenElement && wall.value) wall.value = false
}
// Labels and the exit button fade after 3 s without pointer movement.
function poke() {
  chromeVisible.value = true
  clearTimeout(hideTimer)
  hideTimer = window.setTimeout(() => (chromeVisible.value = false), 3000)
}
const onKey = (e: KeyboardEvent) => e.key === 'Escape' && wall.value && exitWall()

onMounted(() => {
  document.addEventListener('fullscreenchange', onFsChange)
  window.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', onFsChange)
  window.removeEventListener('keydown', onKey)
  clearTimeout(hideTimer)
})
</script>

<template>
  <div>
    <header class="mb-4 flex flex-wrap items-center gap-3">
      <div class="mr-auto w-full sm:w-auto">
        <h1 class="text-xl font-bold">Live</h1>
        <p class="text-sm text-muted">
          <span class="tabular">{{ shownOnline }}/{{ shown.length }}</span> kamera merekam
        </p>
      </div>

      <!-- Phones always get one column, so the layout switcher only shows from sm up. -->
      <div class="hidden overflow-hidden rounded-lg border border-line sm:flex" role="group" aria-label="Tata letak">
        <button
          v-for="l in layouts"
          :key="l.id"
          type="button"
          class="grid min-h-10 min-w-10 place-items-center px-2.5"
          :class="layout === l.id ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
          :aria-pressed="layout === l.id"
          :title="l.label"
          :aria-label="l.label"
          @click="layout = l.id"
        >
          <component :is="l.icon" class="size-4" />
        </button>
      </div>

      <select
        v-if="layout === 'focus' && shown.length > 1"
        v-model="focusId"
        class="min-h-10 rounded-lg border border-line bg-surface px-2 text-sm"
        aria-label="Kamera utama"
      >
        <option v-for="c in shown" :key="c.id" :value="c.id">{{ c.name }}</option>
      </select>

      <button
        type="button"
        class="flex min-h-10 items-center gap-2 rounded-lg border border-line px-3 text-sm font-medium"
        :class="saver ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
        :aria-pressed="saver"
        title="Tampilkan snapshot per menit saja, tanpa video live"
        @click="saver = !saver"
      >
        <Gauge class="size-4" aria-hidden="true" />
        Hemat data
      </button>
      <button
        type="button"
        class="flex min-h-10 items-center gap-2 rounded-lg border border-line px-3 text-sm font-medium hover:bg-surface-2"
        :disabled="!shown.length"
        @click="enterWall"
      >
        <Expand class="size-4" aria-hidden="true" />
        <span class="hidden sm:inline">Mode dinding</span>
        <span class="sr-only sm:hidden">Mode dinding</span>
      </button>
    </header>

    <nav v-if="locationTabs.length > 1" class="-mt-1 mb-4 flex gap-1 overflow-x-auto" aria-label="Lokasi">
      <button
        v-for="t in [{ id: 0, name: 'Semua lokasi', cameras: cams.list.length }, ...locationTabs]"
        :key="t.id"
        type="button"
        class="flex min-h-9 shrink-0 items-center gap-1.5 rounded-full border px-3 text-sm font-medium"
        :class="locationId === t.id ? 'border-primary bg-primary text-on-primary' : 'border-line hover:bg-surface-2'"
        :aria-pressed="locationId === t.id"
        @click="locationId = t.id"
      >
        {{ t.name }} <span class="text-xs opacity-75 tabular">{{ t.id ? cams.list.filter((c) => c.location_id === t.id).length : cams.list.length }}</span>
      </button>
    </nav>

    <!-- Loading -->
    <div v-if="!cams.loaded && cams.loading" class="grid gap-4" :class="gridClass">
      <div v-for="i in 3" :key="i" class="skeleton aspect-video rounded-[var(--radius-card)]" />
    </div>

    <StateMessage v-else-if="cams.error" :icon="CameraOff" tone="danger" title="Gagal memuat kamera" :text="cams.error">
      <button type="button" class="flex items-center gap-2 rounded-lg bg-primary px-4 py-2 font-semibold text-on-primary" @click="cams.load()">
        <RefreshCw class="size-4" aria-hidden="true" />
        Coba lagi
      </button>
    </StateMessage>

    <StateMessage
      v-else-if="cams.loaded && !cams.list.length"
      :icon="CameraOff"
      title="Belum ada kamera"
      text="Tambahkan stream kamera di go2rtc, lalu buka Pengaturan → Kamera dan tekan Sinkronkan."
    />

    <!-- Focus: one large camera with the rest beside it (below on phones). -->
    <div v-else-if="layout === 'focus' && focused" class="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,1fr)]">
      <CameraTile :camera="focused" :live="!saver" />
      <div class="grid content-start gap-4 sm:grid-cols-2 lg:grid-cols-1">
        <CameraTile v-for="c in others" :key="c.id" :camera="c" :live="!saver" />
      </div>
    </div>

    <div v-else class="grid gap-4" :class="gridClass">
      <CameraTile v-for="c in shown" :key="c.id" :camera="c" :live="!saver" />
    </div>

    <!-- Wall mode -->
    <div
      v-if="wall"
      ref="wallEl"
      class="fixed inset-0 z-[60] grid gap-1 bg-black"
      :class="!chromeVisible && 'cursor-none'"
      :style="{ gridTemplateColumns: `repeat(${wallCols}, minmax(0, 1fr))`, gridTemplateRows: `repeat(${wallRows}, minmax(0, 1fr))` }"
      @pointermove="poke"
      @click.capture.prevent="poke"
    >
      <div v-for="c in shown" :key="c.id" class="min-h-0 [&_.aspect-video]:aspect-auto [&_.aspect-video]:h-full [&>div]:h-full [&_a]:h-full [&_a]:rounded-none [&_a]:border-0">
        <CameraTile :camera="c" :live="!saver" :overlay="chromeVisible" />
      </div>
      <button
        type="button"
        class="fixed top-3 right-3 z-[61] flex items-center gap-2 rounded-lg bg-black/70 px-3 py-2 text-sm font-medium text-white transition-opacity"
        :class="chromeVisible ? 'opacity-100' : 'pointer-events-none opacity-0'"
        @click.stop="exitWall"
      >
        <Minimize class="size-4" aria-hidden="true" />
        Keluar (Esc)
      </button>
    </div>
  </div>
</template>
