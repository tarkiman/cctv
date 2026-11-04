import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface Toast {
  id: number
  kind: 'ok' | 'error' | 'info'
  text: string
}

export const useToast = defineStore('toast', () => {
  const items = ref<Toast[]>([])
  let next = 1

  function push(kind: Toast['kind'], text: string, ms = 4000) {
    const id = next++
    items.value.push({ id, kind, text })
    setTimeout(() => dismiss(id), ms)
  }

  function dismiss(id: number) {
    items.value = items.value.filter((t) => t.id !== id)
  }

  return {
    items,
    dismiss,
    ok: (t: string) => push('ok', t),
    error: (t: string) => push('error', t, 7000),
    info: (t: string) => push('info', t),
  }
})
