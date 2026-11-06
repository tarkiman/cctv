<script setup lang="ts">
// Admin: locations group cameras (e.g. "Rumah", "Rumah Ibu") and are what user access is
// granted on.
import { ref } from 'vue'
import { Check, LoaderCircle, MapPin, Pencil, Plus, Trash2, X } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import { useCameras } from '../stores/cameras'
import { useToast } from '../stores/toast'

const cams = useCameras()
const toast = useToast()
const newName = ref('')
const editing = ref<number | null>(null)
const editName = ref('')
const busy = ref<number | 'new' | null>(null)

const err = (e: unknown, fallback: string) => toast.error(e instanceof ApiError ? e.message : fallback)

async function add() {
  if (!newName.value.trim()) return
  busy.value = 'new'
  try {
    cams.locations = await api.createLocation(newName.value.trim())
    toast.ok(`Lokasi ${newName.value.trim()} ditambahkan.`)
    newName.value = ''
  } catch (e) {
    err(e, 'Gagal menambah lokasi.')
  } finally {
    busy.value = null
  }
}

function startEdit(id: number, name: string) {
  editing.value = id
  editName.value = name
}

async function saveEdit(id: number) {
  busy.value = id
  try {
    cams.locations = await api.renameLocation(id, editName.value.trim())
    editing.value = null
  } catch (e) {
    err(e, 'Gagal mengganti nama.')
  } finally {
    busy.value = null
  }
}

async function remove(id: number, name: string) {
  busy.value = id
  try {
    cams.locations = await api.deleteLocation(id)
    toast.ok(`Lokasi ${name} dihapus.`)
  } catch (e) {
    err(e, 'Gagal menghapus lokasi.')
  } finally {
    busy.value = null
  }
}

const input = 'w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary'
</script>

<template>
  <section class="mb-8" aria-labelledby="loc-h">
    <h2 id="loc-h" class="text-base font-semibold">Lokasi</h2>
    <p class="mb-3 text-xs text-muted">
      Kelompokkan kamera per tempat (mis. rumah lain lewat gerbang pipit). Akses pengguna diberikan per lokasi; kamera yang nanti ditambahkan ke
      lokasi itu ikut terlihat.
    </p>
    <div class="max-w-2xl overflow-hidden rounded-[var(--radius-card)] border border-line bg-surface">
      <ul class="divide-y divide-line">
        <li v-for="l in cams.locations" :key="l.id" class="flex items-center gap-3 px-4 py-2.5">
          <MapPin class="size-4 shrink-0 text-muted" aria-hidden="true" />
          <template v-if="editing === l.id">
            <input v-model="editName" :class="input" maxlength="40" :aria-label="`Nama baru untuk ${l.name}`" @keydown.enter.prevent="saveEdit(l.id)" />
            <button type="button" class="grid size-9 shrink-0 place-items-center rounded-md hover:bg-surface-2" aria-label="Simpan" @click="saveEdit(l.id)">
              <LoaderCircle v-if="busy === l.id" class="size-4 animate-spin" aria-hidden="true" />
              <Check v-else class="size-4" />
            </button>
            <button type="button" class="grid size-9 shrink-0 place-items-center rounded-md hover:bg-surface-2" aria-label="Batal" @click="editing = null">
              <X class="size-4" />
            </button>
          </template>
          <template v-else>
            <span class="mr-auto min-w-0 truncate text-sm font-medium">{{ l.name }}</span>
            <span class="shrink-0 text-xs text-muted tabular">{{ l.cameras }} kamera</span>
            <button type="button" class="grid size-9 shrink-0 place-items-center rounded-md hover:bg-surface-2" :aria-label="`Ganti nama ${l.name}`" @click="startEdit(l.id, l.name)">
              <Pencil class="size-4" />
            </button>
            <button
              type="button"
              class="grid size-9 shrink-0 place-items-center rounded-md text-danger hover:bg-danger/10 disabled:opacity-30"
              :disabled="l.cameras > 0 || cams.locations.length === 1 || busy === l.id"
              :title="l.cameras > 0 ? 'Pindahkan dulu kameranya ke lokasi lain' : 'Hapus lokasi'"
              :aria-label="`Hapus ${l.name}`"
              @click="remove(l.id, l.name)"
            >
              <Trash2 class="size-4" />
            </button>
          </template>
        </li>
      </ul>
      <form class="flex gap-2 border-t border-line p-3" @submit.prevent="add">
        <input v-model="newName" :class="input" maxlength="40" placeholder="Nama lokasi baru, mis. Rumah Ibu" aria-label="Nama lokasi baru" />
        <button type="submit" class="flex shrink-0 items-center gap-1.5 rounded-lg border border-line px-3 text-sm font-semibold hover:bg-surface-2 disabled:opacity-50" :disabled="!newName.trim() || busy === 'new'">
          <Plus class="size-4" aria-hidden="true" /> Tambah
        </button>
      </form>
    </div>
  </section>
</template>
