<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Cpu, HardDrive, MemoryStick, RefreshCw, Server } from 'lucide-vue-next'
import { api } from '../api/client'
import type { SystemStats, WorkerStatus } from '../api/types'
import { useAuth } from '../stores/auth'
import { useCameras } from '../stores/cameras'
import { fmtBytes, fmtDateTime, fmtDuration, fmtRelative } from '../utils/format'
import StatusBadge from '../components/StatusBadge.vue'

const auth = useAuth()
const cams = useCameras()
const stats = ref<SystemStats | null>(null)
const workers = ref<WorkerStatus[]>([])
const error = ref('')
const openLog = ref<number | null>(null)
let timer: number | undefined

async function refresh() {
  try {
    const [s, w] = await Promise.all([api.stats(), auth.user?.role === 'admin' ? api.workers() : Promise.resolve([])])
    stats.value = s
    workers.value = w
    error.value = ''
  } catch (e) {
    error.value = (e as Error).message
  }
}

const host = computed(() => stats.value?.host)
const memUsed = computed(() => (host.value ? 1 - host.value.mem_available / host.value.mem_total : 0))
const swapUsed = computed(() => (host.value && host.value.swap_total ? 1 - host.value.swap_free / host.value.swap_total : 0))
const diskUsed = computed(() => (host.value && host.value.disk_total ? 1 - host.value.disk_free / host.value.disk_total : 0))
const tone = (f: number) => (f > 0.9 ? 'bg-danger' : f > 0.75 ? 'bg-warn' : 'bg-ok')
const usage = (id: number) => stats.value?.cameras.find((c) => c.camera_id === id)

onMounted(() => {
  refresh()
  // Refresh only while the tab is visible.
  timer = window.setInterval(() => !document.hidden && refresh(), 10_000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div>
    <header class="mb-4 flex items-center gap-3">
      <div class="mr-auto">
        <h1 class="text-xl font-bold">Status sistem</h1>
        <p class="text-sm text-muted">Diperbarui otomatis setiap 10 detik.</p>
      </div>
      <button type="button" class="grid size-10 place-items-center rounded-lg border border-line hover:bg-surface-2" aria-label="Perbarui sekarang" @click="refresh">
        <RefreshCw class="size-4" />
      </button>
    </header>

    <p v-if="error" class="mb-4 rounded-lg bg-danger/10 p-3 text-sm text-danger" role="alert">{{ error }}</p>

    <div class="mb-6 grid grid-cols-[repeat(auto-fill,minmax(min(100%,220px),1fr))] gap-4">
      <template v-if="host">
        <div class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <p class="mb-1 flex items-center gap-2 text-sm text-muted"><Cpu class="size-4" aria-hidden="true" /> CPU</p>
          <p class="text-2xl font-bold tabular">{{ host.cpu_percent.toFixed(0) }}%</p>
          <p class="text-xs text-muted tabular">{{ host.cpu_cores }} core · load {{ host.load.map((l) => l.toFixed(2)).join(' ') }}</p>
        </div>
        <div class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <p class="mb-1 flex items-center gap-2 text-sm text-muted"><MemoryStick class="size-4" aria-hidden="true" /> Memori</p>
          <p class="text-2xl font-bold tabular">{{ fmtBytes(host.mem_available) }} <span class="text-sm font-normal text-muted">tersedia</span></p>
          <div class="mt-2 h-2 overflow-hidden rounded-full bg-surface-2" role="meter" :aria-valuenow="Math.round(memUsed * 100)" aria-valuemin="0" aria-valuemax="100" aria-label="Memori terpakai">
            <div class="h-full" :class="tone(memUsed)" :style="{ width: `${memUsed * 100}%` }" />
          </div>
          <p class="mt-1 text-xs text-muted tabular">dari {{ fmtBytes(host.mem_total) }} · swap {{ (swapUsed * 100).toFixed(0) }}% terpakai</p>
        </div>
        <div class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <p class="mb-1 flex items-center gap-2 text-sm text-muted"><HardDrive class="size-4" aria-hidden="true" /> Disk rekaman</p>
          <p class="text-2xl font-bold tabular">{{ fmtBytes(host.disk_free) }} <span class="text-sm font-normal text-muted">sisa</span></p>
          <div class="mt-2 h-2 overflow-hidden rounded-full bg-surface-2" role="meter" :aria-valuenow="Math.round(diskUsed * 100)" aria-valuemin="0" aria-valuemax="100" aria-label="Disk terpakai">
            <div class="h-full" :class="tone(diskUsed)" :style="{ width: `${diskUsed * 100}%` }" />
          </div>
          <p class="mt-1 text-xs text-muted tabular">dari {{ fmtBytes(host.disk_total) }}</p>
        </div>
        <div class="rounded-[var(--radius-card)] border border-line bg-surface p-4">
          <p class="mb-1 flex items-center gap-2 text-sm text-muted"><Server class="size-4" aria-hidden="true" /> Server</p>
          <p class="text-2xl font-bold">{{ fmtDuration(host.uptime_seconds) }}</p>
          <p class="text-xs text-muted">nvrd {{ stats?.version }} · go2rtc {{ stats?.go2rtc_version || '—' }}</p>
        </div>
      </template>
      <template v-else>
        <div v-for="i in 4" :key="i" class="skeleton h-28 rounded-[var(--radius-card)]" />
      </template>
    </div>

    <h2 class="mb-3 text-base font-semibold">Kamera</h2>
    <div class="overflow-x-auto rounded-[var(--radius-card)] border border-line bg-surface">
      <table class="w-full min-w-[640px] text-sm">
        <thead class="border-b border-line text-left text-xs text-muted">
          <tr>
            <th class="px-4 py-2.5 font-medium">Kamera</th>
            <th class="px-4 py-2.5 font-medium">Status</th>
            <th class="px-4 py-2.5 font-medium">Segmen terakhir</th>
            <th class="px-4 py-2.5 font-medium">Restart</th>
            <th class="px-4 py-2.5 font-medium">Penyimpanan</th>
            <th class="px-4 py-2.5 font-medium">24 jam terakhir</th>
            <th v-if="workers.length" class="px-4 py-2.5"><span class="sr-only">Log</span></th>
          </tr>
        </thead>
        <tbody>
          <template v-for="c in cams.list" :key="c.id">
            <tr class="border-b border-line last:border-0">
              <td class="px-4 py-3 font-medium">{{ c.name }}</td>
              <td class="px-4 py-3">
                <StatusBadge :state="c.status.state" />
                <p v-if="c.status.message" class="mt-1 max-w-xs text-xs text-muted">{{ c.status.message }}</p>
              </td>
              <td class="px-4 py-3 tabular" :title="c.status.last_segment_at ? fmtDateTime(new Date(c.status.last_segment_at)) : ''">
                {{ fmtRelative(c.status.last_segment_at) }}
              </td>
              <td class="px-4 py-3 tabular">{{ c.status.restarts }}</td>
              <td class="px-4 py-3 tabular">
                {{ fmtBytes(usage(c.id)?.bytes ?? 0) }}
                <span v-if="usage(c.id)?.oldest_at" class="block text-xs text-muted">sejak {{ fmtDateTime(new Date(usage(c.id)!.oldest_at!)) }}</span>
              </td>
              <td class="px-4 py-3 tabular">{{ fmtBytes(usage(c.id)?.last_24h_bytes ?? 0) }}</td>
              <td v-if="workers.length" class="px-4 py-3 text-right">
                <button type="button" class="rounded-md px-2 py-1 text-xs font-semibold text-primary hover:bg-surface-2" :aria-expanded="openLog === c.id" @click="openLog = openLog === c.id ? null : c.id">
                  Log
                </button>
              </td>
            </tr>
            <tr v-if="openLog === c.id">
              <td colspan="7" class="bg-bg px-4 py-3">
                <pre class="max-h-64 overflow-auto text-xs leading-relaxed whitespace-pre-wrap text-muted">{{ workers.find((w) => w.id === c.id)?.log.join('\n') || 'Belum ada log.' }}</pre>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>
