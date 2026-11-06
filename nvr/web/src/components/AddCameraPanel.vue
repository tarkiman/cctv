<script setup lang="ts">
// Add a camera: name, ID, main stream and optional sub-stream, each with a connection test.
import { computed, reactive, ref, watch } from 'vue'
import { LoaderCircle } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { Camera, SourceInput } from '../api/types'
import { useCameras } from '../stores/cameras'
import SourceFields from './SourceFields.vue'
import RtspHints from './RtspHints.vue'

const emit = defineEmits<{ added: [cam: Camera]; cancel: [] }>()

const cams = useCameras()
const form = reactive({ name: '', slug: '', slugTouched: false, withSub: false, location: cams.locations[0]?.id ?? 0 })
const main = ref<SourceInput>({ url: '', username: '', password: '' })
const sub = ref<SourceInput>({ url: '', username: '', password: '' })
const busy = ref(false)
const error = ref('')

const autoSlug = (name: string) =>
  name
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
watch(
  () => form.name,
  (n) => {
    if (!form.slugTouched) form.slug = autoSlug(n)
  },
)
const slugOk = computed(() => /^[a-z0-9][a-z0-9_-]{0,31}$/.test(form.slug) && !form.slug.endsWith('_sub'))

// Same credentials for the sub-stream unless the user typed others.
watch(
  () => form.withSub,
  (on) => {
    if (on && !sub.value.url && main.value.url) {
      sub.value = { url: main.value.url, username: main.value.username, password: main.value.password }
    }
  },
)

async function submit() {
  error.value = ''
  if (!slugOk.value) return (error.value = 'ID kamera tidak valid.')
  busy.value = true
  try {
    const cam = await api.createCamera(form.slug, form.name, main.value, form.withSub && sub.value.url ? sub.value : null, form.location)
    emit('added', cam)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Gagal menambah kamera.'
  } finally {
    busy.value = false
  }
}

const input = 'w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary'
</script>

<template>
  <form class="mb-4 rounded-[var(--radius-card)] border border-primary/40 bg-surface p-4" @submit.prevent="submit">
    <h3 class="mb-3 text-sm font-semibold">Tambah kamera</h3>
    <div class="grid gap-3 sm:grid-cols-2">
      <div>
        <label class="mb-1 block text-sm font-medium" for="nc-name">Nama tampilan</label>
        <input id="nc-name" v-model="form.name" :class="input" maxlength="60" placeholder="mis. Garasi" required />
      </div>
      <div>
        <label class="mb-1 block text-sm font-medium" for="nc-slug">ID kamera</label>
        <input
          id="nc-slug"
          v-model.trim="form.slug"
          :class="[input, form.slug && !slugOk && 'border-danger']"
          class="font-mono"
          autocapitalize="none"
          spellcheck="false"
          maxlength="32"
          required
          aria-describedby="nc-slug-hint"
          @input="form.slugTouched = true"
        />
        <p id="nc-slug-hint" class="mt-1 text-xs text-muted">Dipakai di go2rtc dan nama folder rekaman; tidak bisa diubah nanti.</p>
      </div>
      <div v-if="cams.locations.length > 1">
        <label class="mb-1 block text-sm font-medium" for="nc-loc">Lokasi</label>
        <select id="nc-loc" v-model.number="form.location" :class="input">
          <option v-for="l in cams.locations" :key="l.id" :value="l.id">{{ l.name }}</option>
        </select>
      </div>
    </div>

    <div class="mt-4 grid gap-4 lg:grid-cols-2">
      <fieldset>
        <legend class="mb-2 text-sm font-semibold">Stream utama <span class="font-normal text-muted">(direkam)</span></legend>
        <SourceFields id="nc-main" v-model="main" stream="main" required />
      </fieldset>
      <fieldset>
        <legend class="mb-2 flex items-center gap-2 text-sm font-semibold">
          <input id="nc-withsub" v-model="form.withSub" type="checkbox" class="size-4 accent-[var(--primary)]" />
          <label for="nc-withsub">Sub-stream <span class="font-normal text-muted">(live ringan, disarankan)</span></label>
        </legend>
        <SourceFields v-if="form.withSub" id="nc-sub" v-model="sub" stream="sub" />
        <p v-else class="text-xs text-muted">Resolusi rendah (mis. 640×360 H.264) membuat grid live lebih ringan dan bisa diputar di semua browser.</p>
      </fieldset>
    </div>

    <RtspHints class="mt-4" />

    <p v-if="error" class="mt-3 text-sm text-danger" role="alert">{{ error }}</p>
    <div class="mt-4 flex justify-end gap-2">
      <button type="button" class="min-h-10 rounded-lg px-3 text-sm font-semibold hover:bg-surface-2" @click="emit('cancel')">Batal</button>
      <button type="submit" class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary disabled:opacity-60" :disabled="busy || !main.url">
        <LoaderCircle v-if="busy" class="size-4 animate-spin" aria-hidden="true" /> Tambah kamera
      </button>
    </div>
  </form>
</template>
