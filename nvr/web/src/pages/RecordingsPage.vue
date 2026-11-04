<script setup lang="ts">
// Entry point for playback: pick a camera, land on its recording view (5 minutes ago).
import { RouterLink } from 'vue-router'
import { CameraOff, History } from 'lucide-vue-next'
import { api } from '../api/client'
import { useCameras } from '../stores/cameras'
import { useNow } from '../composables/media'
import { fmtRelative } from '../utils/format'
import StatusBadge from '../components/StatusBadge.vue'
import StateMessage from '../components/StateMessage.vue'

const cams = useCameras()
const now = useNow(30_000)
</script>

<template>
  <div>
    <header class="mb-4">
      <h1 class="text-xl font-bold">Rekaman</h1>
      <p class="text-sm text-muted">Pilih kamera untuk memutar ulang rekaman.</p>
    </header>

    <div v-if="!cams.loaded" class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] gap-4">
      <div v-for="i in 3" :key="i" class="skeleton h-56 rounded-[var(--radius-card)]" />
    </div>
    <StateMessage v-else-if="!cams.list.length" :icon="CameraOff" title="Belum ada kamera" />
    <div v-else class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,280px),1fr))] gap-4">
      <RouterLink
        v-for="c in cams.list"
        :key="c.id"
        :to="{ name: 'camera', params: { slug: c.slug }, query: { mode: 'rekaman' } }"
        class="overflow-hidden rounded-[var(--radius-card)] border border-line bg-surface hover:border-primary"
      >
        <img :src="api.snapshotUrl(c.id, cams.snapshotAt[c.id])" alt="" class="aspect-video w-full bg-[var(--video-bg)] object-cover" loading="lazy" />
        <div class="flex items-center justify-between gap-2 px-3 py-2.5">
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold">{{ c.name }}</p>
            <p class="text-xs text-muted">Segmen terakhir {{ fmtRelative(c.status.last_segment_at, now) }}</p>
          </div>
          <span class="flex items-center gap-2">
            <StatusBadge :state="c.status.state" compact />
            <History class="size-5 text-muted" aria-hidden="true" />
          </span>
        </div>
      </RouterLink>
    </div>
  </div>
</template>
