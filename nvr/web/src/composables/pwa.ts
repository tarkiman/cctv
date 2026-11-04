// PWA support: service worker registration and the install prompt.
import { computed, ref } from 'vue'

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

const deferred = ref<BeforeInstallPromptEvent | null>(null)
const installed = ref(false)

const standalone = () =>
  window.matchMedia('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true

/** Call once at startup, before the browser fires beforeinstallprompt. */
export function setupPwa() {
  installed.value = standalone()
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault() // show our own button instead of the mini-infobar
    deferred.value = e as BeforeInstallPromptEvent
  })
  window.addEventListener('appinstalled', () => {
    installed.value = true
    deferred.value = null
  })
  if ('serviceWorker' in navigator && window.isSecureContext) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js', { updateViaCache: 'none' }).catch(() => {})
    })
  }
}

export function usePwaInstall() {
  const ua = navigator.userAgent
  const ios = /iPhone|iPad|iPod/.test(ua) || (ua.includes('Mac') && navigator.maxTouchPoints > 1)
  return {
    installed: computed(() => installed.value),
    canPrompt: computed(() => !!deferred.value),
    /** iOS has no install prompt; the user adds it from Safari's Share menu. */
    iosManual: computed(() => ios && !installed.value),
    secure: window.isSecureContext,
    async install() {
      const e = deferred.value
      if (!e) return false
      await e.prompt()
      const { outcome } = await e.userChoice
      deferred.value = null
      return outcome === 'accepted'
    },
  }
}
