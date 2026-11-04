import { createRouter, createWebHistory } from 'vue-router'
import { useAuth } from '../stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    bare?: boolean
    title?: string
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('../pages/LoginPage.vue'), meta: { public: true, bare: true, title: 'Masuk' } },
    { path: '/', name: 'live', component: () => import('../pages/LivePage.vue'), meta: { title: 'Live' } },
    { path: '/rekaman', name: 'recordings', component: () => import('../pages/RecordingsPage.vue'), meta: { title: 'Rekaman' } },
    { path: '/camera/:slug', name: 'camera', component: () => import('../pages/CameraPage.vue'), meta: { title: 'Kamera' } },
    { path: '/timelapse', name: 'timelapse', component: () => import('../pages/TimelapsePage.vue'), meta: { title: 'Timelapse' } },
    { path: '/status', name: 'system', component: () => import('../pages/SystemPage.vue'), meta: { title: 'Status' } },
    { path: '/pengaturan', name: 'settings', component: () => import('../pages/SettingsPage.vue'), meta: { title: 'Pengaturan' } },
    { path: '/offline', name: 'offline', component: () => import('../pages/OfflinePage.vue'), meta: { public: true, bare: true, title: 'Offline' } },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('../pages/NotFoundPage.vue'), meta: { title: 'Tidak ditemukan' } },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuth()
  if (!auth.checked) {
    try {
      await auth.check()
    } catch {
      // Server unreachable (offline, or the box is rebooting): don't pretend the user is
      // signed out; show the offline page, which retries.
      if (to.name === 'offline') return
      return { name: 'offline', query: { next: to.fullPath } }
    }
  }
  if (!to.meta.public && !auth.user) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
  if (to.name === 'login' && auth.user) return { name: 'live' }
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · CCTV Rumah` : 'CCTV Rumah'
})
