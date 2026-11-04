import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { setUnauthorizedHandler } from './api/client'
import { useAuth } from './stores/auth'
import { loadPref } from './utils/prefs'
import './styles/main.css'

const theme = loadPref<'system' | 'dark' | 'light'>('theme', 'system')
if (theme !== 'system') document.documentElement.dataset.theme = theme

const app = createApp(App)
app.use(createPinia())
app.use(router)

setUnauthorizedHandler(() => {
  useAuth().user = null
  const here = router.currentRoute.value
  if (!here.meta.public) router.replace({ name: 'login', query: { next: here.fullPath } })
})

app.mount('#app')
