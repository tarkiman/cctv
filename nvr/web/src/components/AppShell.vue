<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Activity, Film, History, Settings, Video, WifiOff, X } from 'lucide-vue-next'
import { useCameras } from '../stores/cameras'
import ToastHost from './ToastHost.vue'

const route = useRoute()
const cams = useCameras()

const nav = [
  { to: '/', name: 'live', label: 'Live', icon: Video },
  { to: '/rekaman', name: 'recordings', label: 'Rekaman', icon: History },
  { to: '/timelapse', name: 'timelapse', label: 'Timelapse', icon: Film },
  { to: '/status', name: 'system', label: 'Status', icon: Activity },
  { to: '/pengaturan', name: 'settings', label: 'Pengaturan', icon: Settings },
]

const isActive = (name: string) => route.name === name || (name === 'recordings' && route.name === 'camera')
const bare = computed(() => route.meta.bare === true)
</script>

<template>
  <div class="min-h-dvh md:flex">
    <a href="#main" class="sr-only z-50 rounded bg-primary px-3 py-2 text-on-primary focus:not-sr-only focus:fixed focus:top-2 focus:left-2">Lewati ke konten</a>

    <!-- Sidebar (tablet/desktop) -->
    <nav v-if="!bare" class="sticky top-0 hidden h-dvh w-56 shrink-0 flex-col border-r border-line bg-surface px-3 py-4 md:flex" aria-label="Navigasi utama">
      <div class="mb-6 flex items-center gap-2 px-2">
        <img src="/favicon.svg" alt="" class="size-7" />
        <span class="text-base font-bold">CCTV Rumah</span>
      </div>
      <RouterLink
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        class="mb-1 flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium"
        :class="isActive(item.name) ? 'bg-primary/15 text-primary' : 'text-muted hover:bg-surface-2 hover:text-fg'"
        :aria-current="isActive(item.name) ? 'page' : undefined"
      >
        <component :is="item.icon" class="size-5" aria-hidden="true" />
        {{ item.label }}
      </RouterLink>
      <div class="mt-auto px-2 text-xs text-muted">
        <span class="tabular">{{ cams.online }}/{{ cams.list.length }}</span> kamera merekam
      </div>
    </nav>

    <div class="min-w-0 flex-1">
      <div
        v-if="!cams.connected"
        class="sticky top-0 z-30 flex items-center gap-2 bg-warn px-4 py-2 text-sm font-medium text-black"
        role="status"
        aria-live="polite"
      >
        <WifiOff class="size-4" aria-hidden="true" />
        Koneksi ke server terputus, mencoba lagi…
      </div>
      <div v-if="cams.alert" class="flex items-center gap-2 bg-danger px-4 py-2 text-sm font-medium text-white" role="alert">
        <span class="flex-1">{{ cams.alert }}</span>
        <button type="button" class="rounded p-1 hover:bg-white/20" aria-label="Tutup peringatan" @click="cams.alert = null">
          <X class="size-4" />
        </button>
      </div>

      <main id="main" :class="bare ? '' : 'px-4 pt-4 pb-24 md:px-6 md:pb-8'">
        <slot />
      </main>
    </div>

    <!-- Bottom navigation (phone) -->
    <nav
      v-if="!bare"
      class="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-line bg-surface/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
      aria-label="Navigasi utama"
    >
      <RouterLink
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        class="flex min-h-14 flex-col items-center justify-center gap-0.5 text-[11px] font-medium"
        :class="isActive(item.name) ? 'text-primary' : 'text-muted'"
        :aria-current="isActive(item.name) ? 'page' : undefined"
      >
        <component :is="item.icon" class="size-5" aria-hidden="true" />
        {{ item.label }}
      </RouterLink>
    </nav>

    <ToastHost />
  </div>
</template>
