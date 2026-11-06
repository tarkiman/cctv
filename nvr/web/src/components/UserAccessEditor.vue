<script setup lang="ts">
// Which cameras a viewer may see: everything, or whole locations (including cameras added
// later) with optional exceptions, plus single cameras from other locations.
import { computed, onMounted, ref } from 'vue'
import { LoaderCircle } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { Grants, UserItem } from '../api/types'
import { useCameras } from '../stores/cameras'
import { useToast } from '../stores/toast'

const props = defineProps<{ user: UserItem }>()
const emit = defineEmits<{ saved: [g: Grants]; close: [] }>()
const cams = useCameras()
const toast = useToast()
const g = ref<Grants | null>(null)
const saving = ref(false)

onMounted(async () => {
  try {
    g.value = await api.userAccess(props.user.id)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal memuat akses.')
  }
})

const camsIn = (locId: number) => cams.list.filter((c) => c.location_id === locId)
const hasLoc = (id: number) => !!g.value?.locations.includes(id)
const toggle = (list: number[], id: number, on: boolean) => {
  const i = list.indexOf(id)
  if (on && i < 0) list.push(id)
  if (!on && i >= 0) list.splice(i, 1)
}

function setLocation(id: number, on: boolean) {
  if (!g.value) return
  toggle(g.value.locations, id, on)
  // Switching a location resets its per-camera choices.
  for (const c of camsIn(id)) {
    toggle(g.value.cameras, c.id, false)
    toggle(g.value.deny_cameras, c.id, false)
  }
}

function camChecked(id: number, locId: number) {
  if (!g.value) return false
  return hasLoc(locId) ? !g.value.deny_cameras.includes(id) : g.value.cameras.includes(id)
}

function setCamera(id: number, locId: number, on: boolean) {
  if (!g.value) return
  if (hasLoc(locId)) toggle(g.value.deny_cameras, id, !on)
  else toggle(g.value.cameras, id, on)
}

const visibleCount = computed(() => {
  if (!g.value) return 0
  if (g.value.access_all) return cams.list.length
  return cams.list.filter((c) => camChecked(c.id, c.location_id)).length
})

async function save() {
  if (!g.value) return
  saving.value = true
  try {
    const saved = await api.setUserAccess(props.user.id, g.value)
    toast.ok(`Akses ${props.user.username} disimpan. Berlaku saat halaman mereka dimuat ulang.`)
    emit('saved', saved)
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menyimpan akses.')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="w-full rounded-lg border border-line bg-bg p-3 text-sm">
    <div v-if="!g" class="skeleton h-20 rounded-md" />
    <template v-else>
      <label class="flex items-start gap-2">
        <input v-model="g.access_all" type="checkbox" class="mt-0.5 size-4 accent-[var(--primary)]" />
        <span>
          <span class="font-semibold">Semua kamera</span>
          <span class="block text-xs text-muted">Termasuk lokasi dan kamera yang ditambahkan nanti.</span>
        </span>
      </label>

      <div v-if="!g.access_all" class="mt-3 space-y-3">
        <fieldset v-for="l in cams.locations" :key="l.id" class="rounded-md border border-line p-2.5">
          <legend class="px-1">
            <label class="flex items-center gap-2 font-semibold">
              <input type="checkbox" class="size-4 accent-[var(--primary)]" :checked="hasLoc(l.id)" @change="setLocation(l.id, ($event.target as HTMLInputElement).checked)" />
              {{ l.name }}
              <span class="text-xs font-normal text-muted">{{ hasLoc(l.id) ? 'seluruh lokasi, termasuk kamera baru' : 'pilih kamera tertentu' }}</span>
            </label>
          </legend>
          <div class="grid gap-x-4 gap-y-1.5 sm:grid-cols-2">
            <label v-for="c in camsIn(l.id)" :key="c.id" class="flex items-center gap-2">
              <input
                type="checkbox"
                class="size-4 accent-[var(--primary)]"
                :checked="camChecked(c.id, l.id)"
                @change="setCamera(c.id, l.id, ($event.target as HTMLInputElement).checked)"
              />
              {{ c.name }}
            </label>
            <p v-if="!camsIn(l.id).length" class="text-xs text-muted">Belum ada kamera.</p>
          </div>
        </fieldset>
      </div>

      <div class="mt-3 flex items-center gap-2">
        <p class="mr-auto text-xs text-muted">{{ visibleCount }} dari {{ cams.list.length }} kamera terlihat</p>
        <button type="button" class="min-h-9 rounded-md px-3 text-xs font-semibold hover:bg-surface-2" @click="emit('close')">Batal</button>
        <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-semibold text-on-primary disabled:opacity-60" :disabled="saving" @click="save">
          <LoaderCircle v-if="saving" class="size-3.5 animate-spin" aria-hidden="true" /> Simpan akses
        </button>
      </div>
    </template>
  </div>
</template>
