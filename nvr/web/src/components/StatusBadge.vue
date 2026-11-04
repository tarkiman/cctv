<script setup lang="ts">
import { computed } from 'vue'
import { CircleAlert, CircleDot, CircleOff, CirclePause, LoaderCircle } from 'lucide-vue-next'
import type { CameraState } from '../api/types'

const props = defineProps<{ state: CameraState; compact?: boolean }>()

// Color + icon + text, so state never relies on color alone.
const meta = computed(() => {
  switch (props.state) {
    case 'running':
      return { label: 'Merekam', icon: CircleDot, cls: 'text-ok' }
    case 'starting':
      return { label: 'Menghubungkan', icon: LoaderCircle, cls: 'text-info', spin: true }
    case 'degraded':
      return { label: 'Bermasalah', icon: CircleAlert, cls: 'text-warn' }
    case 'offline':
      return { label: 'Offline', icon: CircleOff, cls: 'text-danger' }
    default:
      return { label: 'Tidak merekam', icon: CirclePause, cls: 'text-muted' }
  }
})
</script>

<template>
  <span class="inline-flex items-center gap-1 text-xs font-semibold" :class="meta.cls">
    <component :is="meta.icon" class="size-3.5 shrink-0" :class="meta.spin && 'animate-spin'" aria-hidden="true" />
    <span :class="compact && 'sr-only'">{{ meta.label }}</span>
  </span>
</template>
