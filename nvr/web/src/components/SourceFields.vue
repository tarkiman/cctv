<script setup lang="ts">
// One camera source: URL plus separate credentials, and a connection test that shows
// codec / resolution / fps before anything is saved.
import { ref, watch } from 'vue'
import { CircleAlert, CircleCheck, LoaderCircle, PlugZap } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { ProbeResult, SourceInput } from '../api/types'

const model = defineModel<SourceInput>({ required: true })
const props = defineProps<{ id: string; stream: 'main' | 'sub'; cameraId?: number; hasPassword?: boolean; required?: boolean }>()

const testing = ref(false)
const result = ref<ProbeResult | null>(null)
watch(() => [model.value.url, model.value.username, model.value.password], () => (result.value = null))

async function test() {
  testing.value = true
  result.value = null
  try {
    result.value = await api.probeCamera(model.value, props.stream, props.cameraId)
  } catch (e) {
    result.value = { ok: false, error: e instanceof ApiError ? e.message : 'Tes gagal.' }
  } finally {
    testing.value = false
  }
}

const codecName = (c: string) => ({ h264: 'H.264', hevc: 'H.265 (HEVC)' })[c] ?? c.toUpperCase()
const input = 'w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary'
</script>

<template>
  <div class="space-y-2">
    <div>
      <label class="mb-1 block text-xs font-medium text-muted" :for="`${id}-url`">URL stream</label>
      <input
        :id="`${id}-url`"
        v-model.trim="model.url"
        :class="input"
        class="font-mono text-[13px]"
        type="url"
        inputmode="url"
        autocapitalize="none"
        spellcheck="false"
        placeholder="rtsp://192.168.1.30:554/stream1"
        :required="required"
      />
    </div>
    <div class="grid grid-cols-2 gap-2">
      <div>
        <label class="mb-1 block text-xs font-medium text-muted" :for="`${id}-user`">Username</label>
        <input :id="`${id}-user`" v-model.trim="model.username" :class="input" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="opsional" />
      </div>
      <div>
        <label class="mb-1 block text-xs font-medium text-muted" :for="`${id}-pass`">Password</label>
        <input
          :id="`${id}-pass`"
          v-model="model.password"
          :class="input"
          type="password"
          autocomplete="new-password"
          :placeholder="hasPassword ? '•••••• (tidak diubah)' : 'opsional'"
        />
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="flex min-h-9 items-center gap-1.5 rounded-md border border-line px-2.5 text-xs font-semibold hover:bg-surface-2 disabled:opacity-50"
        :disabled="!model.url || testing"
        @click="test"
      >
        <LoaderCircle v-if="testing" class="size-3.5 animate-spin" aria-hidden="true" />
        <PlugZap v-else class="size-3.5" aria-hidden="true" />
        {{ testing ? 'Menghubungkan… (maks 20 dtk)' : 'Tes koneksi' }}
      </button>
    </div>
    <div v-if="result" class="rounded-lg p-2.5 text-xs" :class="result.ok ? 'bg-ok/10' : 'bg-danger/10 text-danger'" role="status" aria-live="polite">
      <template v-if="result.ok && result.info">
        <p class="flex items-center gap-1.5 font-semibold text-ok">
          <CircleCheck class="size-4" aria-hidden="true" /> Tersambung
        </p>
        <p class="mt-1 tabular">
          {{ codecName(result.info.codec) }} · {{ result.info.width }}×{{ result.info.height }}
          <template v-if="result.info.fps"> · {{ result.info.fps }} fps</template>
          <template v-if="result.info.audio"> · audio {{ result.info.audio.toUpperCase() }}</template>
        </p>
        <p v-for="a in result.advice" :key="a" class="mt-1.5 text-warn">{{ a }}</p>
      </template>
      <p v-else class="flex items-start gap-1.5"><CircleAlert class="mt-px size-4 shrink-0" aria-hidden="true" /> {{ result.error }}</p>
    </div>
  </div>
</template>
