<script setup lang="ts">
// Shown when the server cannot be reached (no internet, umma down). Retries on its own
// when the device comes back online.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LoaderCircle, WifiOff } from 'lucide-vue-next'
import StateMessage from '../components/StateMessage.vue'

const route = useRoute()
const router = useRouter()
const retrying = ref(false)

async function retry() {
  retrying.value = true
  const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
  // The guard re-checks the session; if still unreachable it comes back here.
  await router.replace(next).finally(() => (retrying.value = false))
}

onMounted(() => window.addEventListener('online', retry))
onBeforeUnmount(() => window.removeEventListener('online', retry))
</script>

<template>
  <StateMessage :icon="WifiOff" title="Tidak bisa terhubung ke server" text="Periksa koneksi internet perangkat ini. Bila koneksi normal, server CCTV mungkin sedang menyala ulang; tunggu sebentar.">
    <button type="button" class="flex items-center gap-2 rounded-lg bg-primary px-4 py-2 font-semibold text-on-primary disabled:opacity-60" :disabled="retrying" @click="retry">
      <LoaderCircle v-if="retrying" class="size-4 animate-spin" aria-hidden="true" /> Coba lagi
    </button>
  </StateMessage>
</template>
