<script setup lang="ts">
import { CircleAlert, CircleCheck, Info, X } from 'lucide-vue-next'
import { useToast } from '../stores/toast'

const toast = useToast()
const icons = { ok: CircleCheck, error: CircleAlert, info: Info }
</script>

<template>
  <div class="pointer-events-none fixed inset-x-0 bottom-20 z-50 flex flex-col items-center gap-2 px-4 md:right-6 md:bottom-6 md:left-auto md:items-end" aria-live="polite">
    <div
      v-for="t in toast.items"
      :key="t.id"
      class="pointer-events-auto flex w-full max-w-sm items-start gap-2 rounded-lg border border-line bg-surface p-3 text-sm shadow-lg"
      :role="t.kind === 'error' ? 'alert' : 'status'"
    >
      <component
        :is="icons[t.kind]"
        class="mt-0.5 size-4 shrink-0"
        :class="{ 'text-ok': t.kind === 'ok', 'text-danger': t.kind === 'error', 'text-info': t.kind === 'info' }"
        aria-hidden="true"
      />
      <span class="flex-1">{{ t.text }}</span>
      <button type="button" class="rounded p-0.5 text-muted hover:text-fg" aria-label="Tutup" @click="toast.dismiss(t.id)">
        <X class="size-4" />
      </button>
    </div>
  </div>
</template>
