<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Download, Film } from 'lucide-vue-next'
import { api } from '../api/client'
import type { TimelapseItem } from '../api/types'
import { useCameras } from '../stores/cameras'
import { fmtDateLong, wibMidnight } from '../utils/format'
import { loadPref, savePref } from '../utils/prefs'
import StateMessage from '../components/StateMessage.vue'

const cams = useCameras()
const items = ref<TimelapseItem[]>([])
const loading = ref(true)
const error = ref('')
const cameraId = ref<number | null>(loadPref('timelapse.camera', null))
const selectedDate = ref<string | null>(null)
const rate = ref(1)
const video = ref<HTMLVideoElement | null>(null)

const current = computed(() => items.value.find((i) => i.camera_id === cameraId.value) ?? items.value[0])
const camName = (id: number) => cams.byId.get(id)?.name ?? `Kamera ${id}`
const src = computed(() => (current.value && selectedDate.value ? api.timelapseUrl(current.value.camera_id, selectedDate.value) : null))

watch(current, (c) => {
  if (!c) return
  cameraId.value = c.camera_id
  savePref('timelapse.camera', c.camera_id)
  if (!selectedDate.value || !c.dates.includes(selectedDate.value)) selectedDate.value = c.dates[0] ?? null
})
watch(rate, (r) => video.value && (video.value.playbackRate = r))

onMounted(async () => {
  try {
    items.value = await api.timelapse()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div>
    <header class="mb-4">
      <h1 class="text-xl font-bold">Timelapse</h1>
      <p class="text-sm text-muted">Satu hari dipadatkan jadi beberapa detik. Dibuat otomatis setiap pukul 00:15 WIB.</p>
    </header>

    <div v-if="loading" class="skeleton aspect-video max-w-5xl rounded-[var(--radius-card)]" />
    <StateMessage v-else-if="error" :icon="Film" tone="danger" title="Gagal memuat timelapse" :text="error" />
    <template v-else-if="current">
      <div class="mb-4 flex flex-wrap gap-2" role="tablist" aria-label="Kamera">
        <button
          v-for="it in items"
          :key="it.camera_id"
          type="button"
          role="tab"
          class="min-h-10 rounded-full border px-4 text-sm font-semibold"
          :class="it.camera_id === current.camera_id ? 'border-primary bg-primary text-on-primary' : 'border-line hover:bg-surface-2'"
          :aria-selected="it.camera_id === current.camera_id"
          @click="cameraId = it.camera_id"
        >
          {{ camName(it.camera_id) }}
        </button>
      </div>

      <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <section>
          <div class="relative aspect-video overflow-hidden rounded-[var(--radius-card)] bg-[var(--video-bg)]">
            <video v-if="src" ref="video" :key="src" :src="src" class="h-full w-full object-contain" controls playsinline autoplay muted @loadedmetadata="video && (video.playbackRate = rate)" />
            <div v-else class="absolute inset-0 grid place-items-center p-6 text-center text-sm text-white/80">
              <p>
                Belum ada timelapse untuk kamera ini.
                <template v-if="current.enabled">Hari ini sudah terkumpul {{ current.today_frames }} frame; videonya dibuat besok pukul 00:15.</template>
                <template v-else>Timelapse dimatikan untuk kamera ini (Pengaturan → Kamera).</template>
              </p>
            </div>
          </div>
          <div v-if="src" class="mt-3 flex flex-wrap items-center gap-2">
            <span class="mr-auto text-sm font-semibold">{{ fmtDateLong(wibMidnight(selectedDate!)) }}</span>
            <select v-model.number="rate" class="min-h-10 rounded-lg border border-line bg-surface px-2 text-sm font-semibold" aria-label="Kecepatan putar">
              <option v-for="r in [0.5, 1, 2, 4]" :key="r" :value="r">{{ r }}×</option>
            </select>
            <a :href="`${src}?download=1`" class="flex min-h-10 items-center gap-2 rounded-lg border border-line px-3 text-sm font-medium hover:bg-surface-2">
              <Download class="size-4" aria-hidden="true" /> Unduh
            </a>
          </div>
        </section>

        <aside class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <h2 class="mb-3 text-sm font-semibold">Tanggal tersedia</h2>
          <div v-if="current.dates.length" class="grid grid-cols-2 gap-2 sm:grid-cols-4 xl:grid-cols-2">
            <button
              v-for="d in current.dates"
              :key="d"
              type="button"
              class="rounded-lg border px-2 py-2 text-sm tabular"
              :class="d === selectedDate ? 'border-primary bg-primary/15 font-semibold text-primary' : 'border-line hover:bg-surface-2'"
              :aria-pressed="d === selectedDate"
              @click="selectedDate = d"
            >
              {{ fmtDateLong(wibMidnight(d)) }}
            </button>
          </div>
          <p v-else class="text-sm text-muted">Belum ada.</p>
        </aside>
      </div>
    </template>
    <StateMessage v-else :icon="Film" title="Belum ada kamera" />
  </div>
</template>
