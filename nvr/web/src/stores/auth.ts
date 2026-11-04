import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, ApiError } from '../api/client'
import type { User } from '../api/types'

export const useAuth = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const checked = ref(false)

  async function check() {
    try {
      user.value = await api.me()
    } catch (e) {
      if (!(e instanceof ApiError) || e.status !== 401) throw e
      user.value = null
    } finally {
      checked.value = true
    }
  }

  async function login(username: string, password: string) {
    user.value = await api.login(username, password)
    checked.value = true
  }

  async function logout() {
    try {
      await api.logout()
    } finally {
      user.value = null
    }
  }

  return { user, checked, check, login, logout }
})
