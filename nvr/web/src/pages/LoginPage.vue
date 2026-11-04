<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Eye, EyeOff, LoaderCircle } from 'lucide-vue-next'
import { ApiError } from '../api/client'
import { useAuth } from '../stores/auth'

const auth = useAuth()
const router = useRouter()
const route = useRoute()
const username = ref('')
const password = ref('')
const show = ref(false)
const busy = ref(false)
const error = ref('')

async function submit() {
  if (!username.value || !password.value) {
    error.value = 'Isi username dan password.'
    return
  }
  busy.value = true
  error.value = ''
  try {
    await auth.login(username.value.trim(), password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Gagal masuk.'
    password.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="grid min-h-dvh place-items-center px-4">
    <form class="w-full max-w-sm rounded-[var(--radius-card)] border border-line bg-surface p-6 shadow-sm" novalidate @submit.prevent="submit">
      <div class="mb-6 flex items-center gap-3">
        <img src="/favicon.svg" alt="" class="size-10" />
        <div>
          <h1 class="text-lg font-bold">CCTV Rumah</h1>
          <p class="text-sm text-muted">Masuk untuk melihat kamera</p>
        </div>
      </div>

      <label class="mb-1 block text-sm font-medium" for="username">Username</label>
      <input
        id="username"
        v-model="username"
        class="mb-4 w-full rounded-lg border border-line bg-bg px-3 py-2.5 text-base outline-none focus:border-primary"
        autocomplete="username"
        autocapitalize="none"
        spellcheck="false"
        required
      />

      <label class="mb-1 block text-sm font-medium" for="password">Password</label>
      <div class="relative mb-4">
        <input
          id="password"
          v-model="password"
          :type="show ? 'text' : 'password'"
          class="w-full rounded-lg border border-line bg-bg py-2.5 pr-11 pl-3 text-base outline-none focus:border-primary"
          autocomplete="current-password"
          required
        />
        <button
          type="button"
          class="absolute inset-y-0 right-0 grid w-11 place-items-center text-muted hover:text-fg"
          :aria-label="show ? 'Sembunyikan password' : 'Tampilkan password'"
          :aria-pressed="show"
          @click="show = !show"
        >
          <EyeOff v-if="show" class="size-5" />
          <Eye v-else class="size-5" />
        </button>
      </div>

      <p v-if="error" class="mb-4 rounded-lg bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">{{ error }}</p>

      <button
        type="submit"
        class="flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-4 py-2.5 font-semibold text-on-primary disabled:opacity-60"
        :disabled="busy"
      >
        <LoaderCircle v-if="busy" class="size-4 animate-spin" aria-hidden="true" />
        Masuk
      </button>
    </form>
  </div>
</template>
