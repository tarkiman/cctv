<script setup lang="ts">
import { watch } from 'vue'
import { RouterView } from 'vue-router'
import { useAuth } from './stores/auth'
import { useCameras } from './stores/cameras'
import AppShell from './components/AppShell.vue'

const auth = useAuth()
const cams = useCameras()

// Camera list and live status follow the login state.
watch(
  () => auth.user,
  (u) => {
    if (u) {
      cams.load()
      cams.connect()
    } else {
      cams.disconnect()
    }
  },
  { immediate: true },
)
</script>

<template>
  <AppShell>
    <RouterView />
  </AppShell>
</template>
