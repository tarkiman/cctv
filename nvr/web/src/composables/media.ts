import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

/** True while the element is on screen and the tab is visible. */
export function useVisible(el: Ref<HTMLElement | null>, rootMargin = '100px') {
  const inView = ref(false)
  const tabVisible = ref(typeof document === 'undefined' ? true : !document.hidden)
  const visible = ref(false)
  let io: IntersectionObserver | null = null
  const update = () => (visible.value = inView.value && tabVisible.value)
  const onVis = () => {
    tabVisible.value = !document.hidden
    update()
  }
  onMounted(() => {
    document.addEventListener('visibilitychange', onVis)
    if (!el.value) return
    io = new IntersectionObserver(
      ([entry]) => {
        inView.value = !!entry?.isIntersecting
        update()
      },
      { rootMargin },
    )
    io.observe(el.value)
  })
  onBeforeUnmount(() => {
    io?.disconnect()
    document.removeEventListener('visibilitychange', onVis)
  })
  return visible
}

const probe = typeof document !== 'undefined' ? document.createElement('video') : null
const MSE = typeof window !== 'undefined' ? (window.ManagedMediaSource ?? window.MediaSource) : undefined

/** Whether this browser can play a codec as reported by the API ('h264' | 'hevc'). */
export function canPlayCodec(codec?: string): boolean {
  if (!codec || !probe) return true
  const type = codec === 'hevc' ? 'video/mp4; codecs="hvc1.1.6.L120.90"' : 'video/mp4; codecs="avc1.640029"'
  return probe.canPlayType(type) !== '' || !!MSE?.isTypeSupported(type)
}

export function hasMSE() {
  return !!MSE
}

/** Global keyboard shortcuts, ignored while typing in form fields. */
export function useHotkeys(map: Record<string, (e: KeyboardEvent) => void>) {
  const handler = (e: KeyboardEvent) => {
    const t = e.target as HTMLElement | null
    if (t && (t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName))) return
    if (e.ctrlKey || e.metaKey || e.altKey) return
    const key = (e.shiftKey && e.key.startsWith('Arrow') ? 'Shift+' : '') + e.key
    const fn = map[key]
    if (fn) {
      e.preventDefault()
      fn(e)
    }
  }
  onMounted(() => window.addEventListener('keydown', handler))
  onBeforeUnmount(() => window.removeEventListener('keydown', handler))
}

/** A clock that ticks every `ms` (for relative times and the timeline's "now"). */
export function useNow(ms = 30_000) {
  const now = ref(Date.now())
  let t: number | undefined
  onMounted(() => (t = window.setInterval(() => (now.value = Date.now()), ms)))
  onBeforeUnmount(() => clearInterval(t))
  return now
}
