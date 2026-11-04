<script setup lang="ts">
// Per-camera "Sumber & koneksi": change the stream URLs (e.g. after an IP change) and
// delete the camera. Xiaomi / custom sources are shown read-only.
import { computed, ref } from 'vue'
import { ChevronDown, LoaderCircle, Trash2 } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { Camera, CameraSources, SourceInput } from '../api/types'
import { useCameras } from '../stores/cameras'
import { useToast } from '../stores/toast'
import SourceFields from './SourceFields.vue'
import RtspHints from './RtspHints.vue'

const props = defineProps<{ camera: Camera }>()
const cams = useCameras()
const toast = useToast()

const open = ref(false)
const loading = ref(false)
const sources = ref<CameraSources | null>(null)
const main = ref<SourceInput>({ url: '', username: '', password: '' })
const sub = ref<SourceInput>({ url: '', username: '', password: '' })
const withSub = ref(false)
const saving = ref(false)
const error = ref('')
const confirmText = ref('')
const deleting = ref(false)
const showDelete = ref(false)

function fill(s: CameraSources) {
  sources.value = s
  main.value = { url: s.main?.url ?? '', username: s.main?.username ?? '', password: '' }
  sub.value = { url: s.sub?.url ?? '', username: s.sub?.username ?? '', password: '' }
  withSub.value = !!s.sub
}

async function toggle() {
  open.value = !open.value
  if (!open.value || sources.value) return
  loading.value = true
  try {
    fill(await api.cameraSources(props.camera.id))
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Gagal memuat sumber.'
  } finally {
    loading.value = false
  }
}

const dirty = computed(() => {
  const s = sources.value
  if (!s) return false
  const changed = (a: SourceInput, b: CameraSources['main']) => a.url !== (b?.url ?? '') || a.username !== (b?.username ?? '') || a.password !== ''
  return changed(main.value, s.main) || withSub.value !== !!s.sub || (withSub.value && changed(sub.value, s.sub))
})

async function save() {
  error.value = ''
  saving.value = true
  try {
    fill(await api.setCameraSources(props.camera.id, main.value, withSub.value && sub.value.url ? sub.value : null))
    cams.load()
    toast.ok(`Sumber ${props.camera.name} disimpan. Perekam tersambung ulang.`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Gagal menyimpan.'
  } finally {
    saving.value = false
  }
}

async function remove() {
  deleting.value = true
  try {
    await api.deleteCamera(props.camera.id, confirmText.value)
    cams.remove(props.camera.id)
    toast.ok(`${props.camera.name} dihapus beserta rekamannya.`)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menghapus kamera.')
  } finally {
    deleting.value = false
  }
}

const input = 'w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary'
</script>

<template>
  <div class="mt-4 border-t border-line pt-3">
    <button type="button" class="flex w-full items-center gap-2 text-sm font-semibold" :aria-expanded="open" @click="toggle">
      Sumber &amp; koneksi
      <ChevronDown class="ml-auto size-4 transition-transform" :class="open && 'rotate-180'" aria-hidden="true" />
    </button>

    <div v-if="open" class="mt-3 space-y-4">
      <div v-if="loading" class="skeleton h-24 rounded-lg" />
      <template v-else-if="sources">
        <p v-if="!sources.editable" class="rounded-lg bg-surface-2 p-3 text-xs">{{ sources.reason }}</p>
        <template v-else>
          <fieldset>
            <legend class="mb-2 text-xs font-semibold tracking-wide text-muted uppercase">Stream utama</legend>
            <SourceFields :id="`src-${camera.id}-main`" v-model="main" stream="main" :camera-id="camera.id" :has-password="sources.main?.has_password" required />
          </fieldset>
          <fieldset>
            <legend class="mb-2 flex items-center gap-2 text-xs font-semibold tracking-wide text-muted uppercase">
              <input :id="`src-${camera.id}-ws`" v-model="withSub" type="checkbox" class="size-4 accent-[var(--primary)]" />
              <label :for="`src-${camera.id}-ws`">Sub-stream</label>
            </legend>
            <SourceFields v-if="withSub" :id="`src-${camera.id}-sub`" v-model="sub" stream="sub" :camera-id="camera.id" :has-password="sources.sub?.has_password" />
          </fieldset>
          <RtspHints />
          <p v-if="error" class="text-sm text-danger" role="alert">{{ error }}</p>
          <div class="flex justify-end">
            <button
              type="button"
              class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary disabled:opacity-50"
              :disabled="!dirty || saving || !main.url"
              @click="save"
            >
              <LoaderCircle v-if="saving" class="size-4 animate-spin" aria-hidden="true" /> Simpan sumber
            </button>
          </div>
        </template>

        <div v-if="!sources.xiaomi" class="rounded-lg border border-danger/40 p-3">
          <button v-if="!showDelete" type="button" class="flex items-center gap-1.5 text-xs font-semibold text-danger" @click="showDelete = true">
            <Trash2 class="size-3.5" aria-hidden="true" /> Hapus kamera…
          </button>
          <form v-else @submit.prevent="remove">
            <p class="mb-2 text-xs">
              Kamera, <strong>semua rekamannya</strong>, snapshot, dan timelapse akan dihapus permanen. Untuk berhenti merekam saja, matikan
              <em>Rekam 24 jam</em>. Ketik <code class="font-semibold">{{ camera.slug }}</code> untuk konfirmasi.
            </p>
            <div class="flex gap-2">
              <input v-model="confirmText" :class="input" class="font-mono" autocapitalize="none" spellcheck="false" :aria-label="`Ketik ${camera.slug}`" />
              <button
                type="submit"
                class="flex shrink-0 items-center gap-1.5 rounded-lg bg-danger px-3 text-sm font-semibold text-white disabled:opacity-40"
                :disabled="confirmText !== camera.slug || deleting"
              >
                <LoaderCircle v-if="deleting" class="size-4 animate-spin" aria-hidden="true" /> Hapus
              </button>
              <button type="button" class="shrink-0 rounded-lg px-2 text-sm hover:bg-surface-2" @click="(showDelete = false), (confirmText = '')">Batal</button>
            </div>
          </form>
        </div>
      </template>
      <p v-else-if="error" class="text-sm text-danger">{{ error }}</p>
    </div>
  </div>
</template>
