<script setup lang="ts">
// Admin: manage who can sign in. Viewers can watch live and recordings and export clips;
// only admins change cameras, settings and users.
import { onMounted, reactive, ref } from 'vue'
import { Copy, KeyRound, LoaderCircle, Trash2, UserPlus } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { UserItem } from '../api/types'
import { useAuth } from '../stores/auth'
import { useToast } from '../stores/toast'
import { fmtRelative, randomPassword } from '../utils/format'

const auth = useAuth()
const toast = useToast()
const users = ref<UserItem[]>([])
const loading = ref(true)
const busy = ref<number | 'new' | null>(null)
const confirmDelete = ref<number | null>(null)
let confirmTimer: number | undefined

// Password shown once after create/reset, so the admin can pass it on.
const reveal = ref<{ username: string; password: string } | null>(null)

const form = reactive({ open: false, username: '', password: '', role: 'viewer' as UserItem['role'], error: '' })

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    users.value = await api.users()
  } catch (e) {
    toast.error(errText(e, 'Gagal memuat pengguna.'))
  } finally {
    loading.value = false
  }
}

function openForm() {
  Object.assign(form, { open: true, username: '', password: randomPassword(), role: 'viewer', error: '' })
}

async function create() {
  form.error = ''
  busy.value = 'new'
  try {
    users.value = await api.createUser(form.username, form.password, form.role)
    reveal.value = { username: form.username.trim().toLowerCase(), password: form.password }
    form.open = false
    toast.ok('Pengguna ditambahkan.')
  } catch (e) {
    form.error = errText(e, 'Gagal menambah pengguna.')
  } finally {
    busy.value = null
  }
}

async function setRole(u: UserItem, role: UserItem['role']) {
  if (role === u.role) return
  busy.value = u.id
  try {
    users.value = await api.updateUser(u.id, { role })
    toast.ok(`${u.username} sekarang ${role === 'admin' ? 'admin' : 'viewer'}.`)
  } catch (e) {
    toast.error(errText(e, 'Gagal mengubah peran.'))
    users.value = [...users.value] // revert the select
  } finally {
    busy.value = null
  }
}

async function resetPassword(u: UserItem) {
  const password = randomPassword()
  busy.value = u.id
  try {
    users.value = await api.updateUser(u.id, { password })
    reveal.value = { username: u.username, password }
    toast.ok(`Password ${u.username} direset; semua perangkatnya dikeluarkan.`)
  } catch (e) {
    toast.error(errText(e, 'Gagal mereset password.'))
  } finally {
    busy.value = null
  }
}

async function remove(u: UserItem) {
  if (confirmDelete.value !== u.id) {
    confirmDelete.value = u.id
    clearTimeout(confirmTimer)
    confirmTimer = window.setTimeout(() => (confirmDelete.value = null), 4000)
    return
  }
  confirmDelete.value = null
  busy.value = u.id
  try {
    users.value = await api.deleteUser(u.id)
    toast.ok(`${u.username} dihapus.`)
  } catch (e) {
    toast.error(errText(e, 'Gagal menghapus.'))
  } finally {
    busy.value = null
  }
}

async function copyReveal() {
  if (!reveal.value) return
  try {
    await navigator.clipboard.writeText(reveal.value.password)
    toast.ok('Password disalin.')
  } catch {
    toast.error('Tidak bisa menyalin; salin manual.')
  }
}

onMounted(load)

const input = 'w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary'
</script>

<template>
  <section class="mb-8" aria-labelledby="users-h">
    <div class="mb-3 flex items-center gap-3">
      <div class="mr-auto">
        <h2 id="users-h" class="text-base font-semibold">Pengguna</h2>
        <p class="text-xs text-muted">Viewer bisa menonton live, rekaman, dan export klip. Hanya admin yang mengubah kamera, pengaturan, dan pengguna.</p>
      </div>
      <button v-if="!form.open" type="button" class="flex min-h-10 shrink-0 items-center gap-2 rounded-lg border border-line px-3 text-sm font-semibold hover:bg-surface-2" @click="openForm">
        <UserPlus class="size-4" aria-hidden="true" /> Tambah
      </button>
    </div>

    <div v-if="reveal" class="mb-3 rounded-[var(--radius-card)] border border-primary/40 bg-primary/10 p-4 text-sm" role="status">
      <p class="mb-2">
        Password untuk <strong>{{ reveal.username }}</strong>. Berikan ke orangnya sekarang; password ini tidak ditampilkan lagi.
      </p>
      <div class="flex flex-wrap items-center gap-2">
        <code class="rounded-md bg-bg px-3 py-1.5 text-base font-semibold tracking-wide tabular select-all">{{ reveal.password }}</code>
        <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold hover:bg-surface-2" @click="copyReveal">
          <Copy class="size-3.5" aria-hidden="true" /> Salin
        </button>
        <button type="button" class="ml-auto min-h-9 rounded-md px-2.5 text-xs font-semibold text-muted hover:bg-surface-2" @click="reveal = null">Selesai</button>
      </div>
    </div>

    <form v-if="form.open" class="mb-3 rounded-[var(--radius-card)] border border-line bg-surface p-4" @submit.prevent="create">
      <div class="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
        <div>
          <label class="mb-1 block text-sm font-medium" for="nu-name">Username</label>
          <input id="nu-name" v-model="form.username" :class="input" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="mis. ibu" required />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium" for="nu-pass">Password awal</label>
          <input id="nu-pass" v-model="form.password" :class="input" class="tabular" autocomplete="off" spellcheck="false" minlength="10" required />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium" for="nu-role">Peran</label>
          <select id="nu-role" v-model="form.role" :class="input">
            <option value="viewer">Viewer</option>
            <option value="admin">Admin</option>
          </select>
        </div>
      </div>
      <p class="mt-1 text-xs text-muted">Password acak sudah diisi (bisa diganti, minimal 10 karakter). Pengguna bisa menggantinya sendiri setelah masuk.</p>
      <p v-if="form.error" class="mt-2 text-sm text-danger" role="alert">{{ form.error }}</p>
      <div class="mt-3 flex justify-end gap-2">
        <button type="button" class="min-h-10 rounded-lg px-3 text-sm font-semibold hover:bg-surface-2" @click="form.open = false">Batal</button>
        <button type="submit" class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary" :disabled="busy === 'new'">
          <LoaderCircle v-if="busy === 'new'" class="size-4 animate-spin" aria-hidden="true" /> Tambah pengguna
        </button>
      </div>
    </form>

    <div class="overflow-hidden rounded-[var(--radius-card)] border border-line bg-surface">
      <div v-if="loading" class="skeleton h-24" />
      <ul v-else class="divide-y divide-line">
        <li v-for="u in users" :key="u.id" class="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
          <div class="mr-auto min-w-0">
            <p class="truncate font-medium">
              {{ u.username }}
              <span v-if="u.id === auth.user?.id" class="ml-1 rounded bg-surface-2 px-1.5 py-0.5 text-[11px] font-semibold text-muted">Anda</span>
            </p>
            <p class="text-xs text-muted">
              {{ u.sessions ? `${u.sessions} perangkat · aktif ${fmtRelative(u.last_seen)}` : 'Tidak sedang masuk' }}
            </p>
          </div>
          <select
            class="min-h-9 rounded-lg border border-line bg-surface px-2 text-sm disabled:opacity-60"
            :value="u.role"
            :disabled="u.id === auth.user?.id || busy === u.id"
            :aria-label="`Peran ${u.username}`"
            :title="u.id === auth.user?.id ? 'Peran akun sendiri tidak bisa diubah' : ''"
            @change="setRole(u, ($event.target as HTMLSelectElement).value as UserItem['role'])"
          >
            <option value="admin">Admin</option>
            <option value="viewer">Viewer</option>
          </select>
          <div v-if="u.id !== auth.user?.id" class="flex items-center gap-1">
            <button
              type="button"
              class="flex min-h-9 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold hover:bg-surface-2 disabled:opacity-60"
              :disabled="busy === u.id"
              title="Buat password acak baru dan keluarkan semua perangkatnya"
              @click="resetPassword(u)"
            >
              <KeyRound class="size-3.5" aria-hidden="true" /> Reset password
            </button>
            <button
              type="button"
              class="flex min-h-9 items-center gap-1.5 rounded-md px-2.5 text-xs font-semibold text-danger hover:bg-danger/10 disabled:opacity-60"
              :class="confirmDelete === u.id && 'bg-danger text-white hover:bg-danger'"
              :disabled="busy === u.id"
              :aria-label="`Hapus ${u.username}`"
              @click="remove(u)"
            >
              <Trash2 class="size-3.5" aria-hidden="true" /> {{ confirmDelete === u.id ? 'Yakin hapus?' : 'Hapus' }}
            </button>
          </div>
        </li>
      </ul>
    </div>
  </section>
</template>
