<script setup lang="ts">
// The signed-in user's own sessions, so a lost phone or a shared laptop can be signed out.
import { computed, onMounted, ref } from 'vue'
import { LoaderCircle, Monitor, Smartphone } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { Device } from '../api/types'
import { useToast } from '../stores/toast'
import { describeDevice, fmtDateTime, fmtRelative } from '../utils/format'

const toast = useToast()
const devices = ref<Device[]>([])
const loading = ref(true)
const busy = ref<string | null>(null)
const others = computed(() => devices.value.filter((d) => !d.current).length)

async function load() {
  try {
    devices.value = await api.sessions()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal memuat perangkat.')
  } finally {
    loading.value = false
  }
}

async function signOut(d: Device) {
  busy.value = d.id
  try {
    await api.deleteSession(d.id)
    devices.value = devices.value.filter((x) => x.id !== d.id)
    toast.ok('Perangkat dikeluarkan.')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal mengeluarkan perangkat.')
  } finally {
    busy.value = null
  }
}

async function signOutOthers() {
  busy.value = 'all'
  try {
    await api.logoutOthers()
    devices.value = devices.value.filter((d) => d.current)
    toast.ok('Semua perangkat lain dikeluarkan.')
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal mengeluarkan perangkat.')
  } finally {
    busy.value = null
  }
}

const mobile = (ua: string) => /Android|iPhone|iPad|Mobile/.test(ua)
onMounted(load)
</script>

<template>
  <div class="mt-4 max-w-2xl rounded-[var(--radius-card)] border border-line bg-surface">
    <div class="flex items-center gap-3 border-b border-line px-4 py-3">
      <h3 class="mr-auto text-sm font-semibold">Perangkat yang sedang masuk</h3>
      <button
        v-if="others"
        type="button"
        class="flex min-h-9 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold text-danger hover:bg-danger/10 disabled:opacity-60"
        :disabled="busy === 'all'"
        @click="signOutOthers"
      >
        <LoaderCircle v-if="busy === 'all'" class="size-3.5 animate-spin" aria-hidden="true" /> Keluarkan {{ others }} perangkat lain
      </button>
    </div>
    <div v-if="loading" class="skeleton h-16" />
    <ul v-else class="divide-y divide-line">
      <li v-for="d in devices" :key="d.id" class="flex items-center gap-3 px-4 py-3">
        <component :is="mobile(d.user_agent) ? Smartphone : Monitor" class="size-5 shrink-0 text-muted" aria-hidden="true" />
        <div class="mr-auto min-w-0">
          <p class="truncate text-sm font-medium" :title="d.user_agent">
            {{ describeDevice(d.user_agent) }}
            <span v-if="d.current" class="ml-1 rounded bg-ok/15 px-1.5 py-0.5 text-[11px] font-semibold text-ok">Perangkat ini</span>
          </p>
          <p class="text-xs text-muted tabular">
            {{ d.ip || 'IP tidak diketahui' }} · {{ d.current ? 'aktif sekarang' : `aktif ${fmtRelative(d.last_seen)}` }} · masuk {{ fmtDateTime(new Date(d.created_at)) }}
          </p>
        </div>
        <button
          v-if="!d.current"
          type="button"
          class="min-h-9 shrink-0 rounded-md px-2.5 text-xs font-semibold hover:bg-surface-2 disabled:opacity-60"
          :disabled="busy === d.id"
          @click="signOut(d)"
        >
          Keluarkan
        </button>
      </li>
    </ul>
  </div>
</template>
