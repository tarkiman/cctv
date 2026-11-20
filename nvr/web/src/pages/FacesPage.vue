<script setup lang="ts">
// Face gallery (Tahap E, admin): name the faces nvr-vision collected inside the fence.
// Recognition matches new faces against the named ones; a known person's arrival is
// announced once per visit (Pengaturan → Pengenalan wajah).
import { computed, onMounted, ref } from 'vue'
import { Bell, BellOff, Check, LoaderCircle, Pencil, RefreshCw, ScanFace, Trash2, UserRoundX, X } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { FaceCluster, PersonItem } from '../api/types'
import { useToast } from '../stores/toast'
import { fmtDateTime, fmtTime } from '../utils/format'
import StateMessage from '../components/StateMessage.vue'

const toast = useToast()
const tab = ref<'new' | 'people'>('new')
const loading = ref(true)
const clusters = ref<FaceCluster[]>([])
const people = ref<PersonItem[]>([])
const busy = ref<string>('')
// Per cluster (by its first id): faces left out of the group, and the typed name.
const excluded = ref<Record<number, Set<number>>>({})
const names = ref<Record<number, string>>({})
const renaming = ref<number | null>(null)
const renameTo = ref('')

const unnamedCount = computed(() => clusters.value.reduce((n, c) => n + c.count, 0))

async function load() {
  loading.value = true
  try {
    ;[clusters.value, people.value] = await Promise.all([api.faceClusters(), api.people()])
    excluded.value = {}
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Galeri wajah tidak bisa dimuat.')
  } finally {
    loading.value = false
  }
}

const key = (c: FaceCluster) => c.ids[0]!
const isOut = (c: FaceCluster, id: number) => excluded.value[key(c)]?.has(id) ?? false
function toggleOut(c: FaceCluster, id: number) {
  const set = new Set(excluded.value[key(c)] ?? [])
  if (set.has(id)) set.delete(id)
  else set.add(id)
  excluded.value = { ...excluded.value, [key(c)]: set }
}
// Faces in the group that will be named: all except the ones left out.
const chosen = (c: FaceCluster) => c.ids.filter((id) => !isOut(c, id))

const span = (c: FaceCluster) => {
  const a = new Date(c.first_at)
  const b = new Date(c.last_at)
  return a.toDateString() === b.toDateString() ? `${fmtDateTime(a)}–${fmtTime(b)}` : `${fmtDateTime(a)} – ${fmtDateTime(b)}`
}

async function assign(c: FaceCluster, target: { person_id?: number; name?: string }) {
  const ids = chosen(c)
  if (!ids.length) return toast.error('Semua foto di grup ini dikeluarkan.')
  if (!target.person_id && !target.name?.trim()) return toast.error('Ketik nama dulu.')
  busy.value = `assign-${key(c)}`
  try {
    people.value = await api.assignFaces(ids, target)
    const name = target.name?.trim() || people.value.find((p) => p.id === target.person_id)?.name
    toast.ok(`${ids.length} foto disimpan sebagai ${name}.`)
    clusters.value = await api.faceClusters()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menyimpan.')
  } finally {
    busy.value = ''
  }
}

async function discard(c: FaceCluster) {
  const ids = chosen(c)
  if (!ids.length || !confirm(`Hapus ${ids.length} foto ini? (mis. bukan wajah, buram, atau orang lewat)`)) return
  busy.value = `discard-${key(c)}`
  try {
    await api.deleteFaces(ids)
    clusters.value = await api.faceClusters()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menghapus.')
  } finally {
    busy.value = ''
  }
}

async function setNotify(p: PersonItem, notify: boolean) {
  try {
    people.value = await api.updatePerson(p.id, { notify })
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menyimpan.')
  }
}

async function rename(p: PersonItem) {
  if (!renameTo.value.trim()) return
  try {
    people.value = await api.updatePerson(p.id, { name: renameTo.value.trim() })
    renaming.value = null
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal mengganti nama.')
  }
}

async function unassign(p: PersonItem, id: number) {
  if (!confirm(`Lepaskan foto ini dari ${p.name}? (kalau ternyata bukan dia)`)) return
  try {
    await api.unassignFaces([id])
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal.')
  }
}

async function removePerson(p: PersonItem) {
  if (!confirm(`Hapus ${p.name}? Foto-fotonya kembali ke "Belum diberi nama" dan ia tidak akan dikenali lagi.`)) return
  try {
    await api.deletePerson(p.id)
    await load()
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : 'Gagal menghapus.')
  }
}

onMounted(load)
</script>

<template>
  <div class="max-w-6xl">
    <header class="mb-4 flex flex-wrap items-center gap-3">
      <h1 class="mr-auto text-xl font-bold">Wajah</h1>
      <button type="button" class="flex min-h-10 items-center gap-1.5 rounded-lg px-3 text-sm font-semibold hover:bg-surface-2" @click="load">
        <RefreshCw class="size-4" aria-hidden="true" /> Muat ulang
      </button>
    </header>
    <p class="mb-4 max-w-3xl text-sm text-muted">
      Wajah orang yang masuk pagar dikumpulkan otomatis dan dikelompokkan berdasarkan kemiripan. Beri nama pada kelompoknya; setelah itu orang tersebut
      dikenali dan kedatangannya diberitahukan sekali per kunjungan. Klik foto untuk mengeluarkannya dari kelompok bila bukan orang yang sama. Foto tanpa
      nama terhapus otomatis setelah 7 hari.
    </p>

    <div class="mb-4 flex w-fit overflow-hidden rounded-lg border border-line" role="tablist">
      <button
        type="button"
        role="tab"
        class="min-h-10 px-4 text-sm font-semibold"
        :class="tab === 'new' ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
        :aria-selected="tab === 'new'"
        @click="tab = 'new'"
      >
        Belum diberi nama <span class="tabular opacity-80">({{ unnamedCount }})</span>
      </button>
      <button
        type="button"
        role="tab"
        class="min-h-10 px-4 text-sm font-semibold"
        :class="tab === 'people' ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'"
        :aria-selected="tab === 'people'"
        @click="tab = 'people'"
      >
        Orang dikenal <span class="tabular opacity-80">({{ people.length }})</span>
      </button>
    </div>

    <div v-if="loading" class="flex items-center gap-2 text-sm text-muted"><LoaderCircle class="size-4 animate-spin" /> Memuat…</div>

    <!-- Unnamed groups -->
    <template v-else-if="tab === 'new'">
      <StateMessage
        v-if="!clusters.length"
        :icon="ScanFace"
        title="Belum ada wajah baru"
        text="Wajah muncul di sini setelah ada orang masuk ke zona Dalam pagar kamera yang punya zona."
      />
      <datalist id="people-names">
        <option v-for="p in people" :key="p.id" :value="p.name" />
      </datalist>
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <section v-for="c in clusters" :key="key(c)" class="rounded-[var(--radius-card)] border border-line bg-surface p-3">
          <div class="mb-2 flex items-baseline justify-between gap-2 text-xs text-muted">
            <span><b class="text-sm text-fg">{{ c.count }} foto</b> · {{ span(c) }}</span>
            <span v-if="chosen(c).length !== c.count" class="text-warn">{{ c.count - chosen(c).length }} dikeluarkan</span>
          </div>
          <div class="grid grid-cols-4 gap-1.5">
            <button
              v-for="id in c.preview"
              :key="id"
              type="button"
              class="relative aspect-square overflow-hidden rounded-md bg-surface-2"
              :aria-pressed="isOut(c, id)"
              :title="isOut(c, id) ? 'Masukkan lagi ke kelompok' : 'Keluarkan dari kelompok'"
              @click="toggleOut(c, id)"
            >
              <img :src="api.faceImageUrl(id)" alt="" loading="lazy" class="h-full w-full object-cover" :class="isOut(c, id) && 'opacity-30 grayscale'" />
              <X v-if="isOut(c, id)" class="absolute inset-0 m-auto size-6 text-danger" aria-hidden="true" />
            </button>
          </div>
          <p v-if="c.count > c.preview.length" class="mt-1 text-xs text-muted">+{{ c.count - c.preview.length }} foto lain yang mirip ikut disimpan.</p>

          <button
            v-if="c.suggestion"
            type="button"
            class="mt-2 flex min-h-9 w-full items-center justify-center gap-1.5 rounded-lg bg-surface-2 px-3 text-sm font-semibold hover:bg-primary hover:text-on-primary"
            :disabled="!!busy"
            @click="assign(c, { person_id: c.suggestion.person_id })"
          >
            <Check class="size-4" aria-hidden="true" /> Ya, ini {{ c.suggestion.name }}
            <span class="font-normal opacity-70">({{ Math.round(c.suggestion.score * 100) }}% mirip)</span>
          </button>

          <form class="mt-2 flex gap-2" @submit.prevent="assign(c, { name: names[key(c)] })">
            <input
              v-model="names[key(c)]"
              list="people-names"
              maxlength="40"
              placeholder="Nama orang ini"
              :aria-label="`Nama untuk kelompok ${c.count} foto`"
              class="min-h-9 min-w-0 flex-1 rounded-lg border border-line bg-bg px-2 text-sm outline-none focus:border-primary"
            />
            <button type="submit" class="flex min-h-9 items-center gap-1 rounded-lg bg-primary px-3 text-sm font-semibold text-on-primary disabled:opacity-50" :disabled="!!busy">
              <LoaderCircle v-if="busy === `assign-${key(c)}`" class="size-4 animate-spin" aria-hidden="true" />
              Simpan
            </button>
            <button
              type="button"
              class="grid min-h-9 place-items-center rounded-lg px-2 text-danger hover:bg-surface-2"
              title="Hapus foto-foto ini (bukan wajah, buram, atau orang lewat)"
              aria-label="Hapus foto-foto ini"
              :disabled="!!busy"
              @click="discard(c)"
            >
              <Trash2 class="size-4" aria-hidden="true" />
            </button>
          </form>
        </section>
      </div>
    </template>

    <!-- Known people -->
    <template v-else>
      <StateMessage v-if="!people.length" :icon="ScanFace" title="Belum ada orang" text="Beri nama pada kelompok wajah di tab Belum diberi nama." />
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <section v-for="p in people" :key="p.id" class="rounded-[var(--radius-card)] border border-line bg-surface p-3">
          <div class="mb-2 flex items-center gap-2">
            <form v-if="renaming === p.id" class="flex flex-1 gap-1" @submit.prevent="rename(p)">
              <input v-model="renameTo" maxlength="40" class="min-h-9 min-w-0 flex-1 rounded-lg border border-line bg-bg px-2 text-sm" aria-label="Nama baru" />
              <button type="submit" class="rounded-lg bg-primary px-3 text-sm font-semibold text-on-primary">Simpan</button>
              <button type="button" class="rounded-lg px-2 text-sm" @click="renaming = null">Batal</button>
            </form>
            <template v-else>
              <h2 class="mr-auto truncate font-semibold">{{ p.name }}</h2>
              <button
                type="button"
                class="grid size-9 place-items-center rounded-lg hover:bg-surface-2"
                :aria-label="`Ganti nama ${p.name}`"
                @click="(renaming = p.id), (renameTo = p.name)"
              >
                <Pencil class="size-4" aria-hidden="true" />
              </button>
              <button type="button" class="grid size-9 place-items-center rounded-lg text-danger hover:bg-surface-2" :aria-label="`Hapus ${p.name}`" @click="removePerson(p)">
                <UserRoundX class="size-4" aria-hidden="true" />
              </button>
            </template>
          </div>
          <p class="mb-2 text-xs text-muted">
            {{ p.samples }} foto contoh ·
            {{ p.last_seen ? `terakhir dikenali ${fmtDateTime(new Date(p.last_seen))}` : 'belum pernah dikenali' }}
          </p>
          <div class="grid grid-cols-4 gap-1.5">
            <button
              v-for="id in p.preview"
              :key="id"
              type="button"
              class="aspect-square overflow-hidden rounded-md bg-surface-2"
              title="Lepaskan foto ini dari orang ini"
              @click="unassign(p, id)"
            >
              <img :src="api.faceImageUrl(id)" alt="" loading="lazy" class="h-full w-full object-cover" />
            </button>
          </div>
          <label class="mt-3 flex items-center justify-between gap-3 text-sm">
            <span class="flex items-center gap-1.5">
              <component :is="p.notify ? Bell : BellOff" class="size-4 text-muted" aria-hidden="true" />
              Beri tahu saat datang
            </span>
            <input type="checkbox" class="size-5 accent-[var(--primary)]" :checked="p.notify" @change="setNotify(p, ($event.target as HTMLInputElement).checked)" />
          </label>
        </section>
      </div>
    </template>
  </div>
</template>
