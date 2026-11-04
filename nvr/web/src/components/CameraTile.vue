<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { ImageOff, TriangleAlert } from 'lucide-vue-next'
import type { Camera } from '../api/types'
import { api } from '../api/client'
import { useCameras } from '../stores/cameras'
import { useVisible, canPlayCodec } from '../composables/media'
import { fmtTime } from '../utils/format'
import LivePlayer from './LivePlayer.vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ camera: Camera; live: boolean; overlay?: boolean }>()
const cams = useCameras()
const root = ref<HTMLElement | null>(null)
const visible = useVisible(root)
const imgFailed = ref(false)

// Grid tiles prefer the light sub-stream; HEVC-only cameras fall back to snapshots on
// browsers that cannot decode HEVC.
const quality = computed(() => (props.camera.has_sub_stream ? 'sub' : 'main'))
const liveCodecOk = computed(() => props.camera.has_sub_stream || canPlayCodec(props.camera.codec))
const showLive = computed(() => props.live && liveCodecOk.value && props.camera.status.state !== 'offline')
const snapUrl = computed(() => api.snapshotUrl(props.camera.id, cams.snapshotAt[props.camera.id]))
const offline = computed(() => props.camera.status.state === 'offline' || props.camera.status.state === 'degraded')
watch(snapUrl, () => (imgFailed.value = false))
</script>

<template>
  <div ref="root">
  <RouterLink
    :to="{ name: 'camera', params: { slug: camera.slug } }"
    class="group relative block overflow-hidden rounded-[var(--radius-card)] border border-line bg-surface focus-visible:outline-2"
    :aria-label="`Buka kamera ${camera.name}`"
  >
    <div class="relative aspect-video bg-[var(--video-bg)]">
      <!-- Snapshot underneath: shown while live connects, when offline, or in data-saver mode. -->
      <img
        v-if="!imgFailed"
        :src="snapUrl"
        alt=""
        class="absolute inset-0 h-full w-full object-cover"
        :class="offline && 'opacity-40 grayscale'"
        loading="lazy"
        @error="imgFailed = true"
        @load="imgFailed = false"
      />
      <div v-else-if="!offline" class="absolute inset-0 grid place-items-center text-muted">
        <ImageOff class="size-8" aria-hidden="true" />
      </div>
      <LivePlayer v-if="showLive && visible" :camera-id="camera.id" :quality="quality" fit="cover" class="absolute inset-0" />

      <div v-if="offline" class="absolute inset-0 grid place-items-center p-4 text-center">
        <div class="max-w-xs rounded-lg bg-black/70 p-3 text-sm text-white">
          <TriangleAlert class="mx-auto mb-1 size-5 text-amber-300" aria-hidden="true" />
          <p class="font-semibold">{{ camera.status.state === 'offline' ? 'Kamera offline' : 'Kamera bermasalah' }}</p>
          <p v-if="camera.status.message" class="mt-1 text-xs text-white/80">{{ camera.status.message }}</p>
        </div>
      </div>
      <p v-else-if="!showLive && camera.status.last_snapshot_at" class="absolute right-2 bottom-2 rounded bg-black/60 px-1.5 py-0.5 text-[11px] text-white tabular">
        Snapshot {{ fmtTime(new Date(camera.status.last_snapshot_at)) }}
      </p>
    </div>

    <div
      class="flex items-center justify-between gap-2 px-3 py-2"
      :class="overlay && 'absolute inset-x-0 top-0 bg-gradient-to-b from-black/70 to-transparent text-white'"
    >
      <span class="truncate text-sm font-semibold">{{ camera.name }}</span>
      <StatusBadge :state="camera.status.state" :compact="overlay" class="shrink-0" />
    </div>
  </RouterLink>
  </div>
</template>
