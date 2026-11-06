import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../api/client'
import type { Camera, CameraStatus, Location } from '../api/types'

export const useCameras = defineStore('cameras', () => {
  const list = ref<Camera[]>([])
  const locations = ref<Location[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  // Bumped on each new snapshot so <img> URLs change and the browser refetches.
  const snapshotAt = ref<Record<number, string>>({})
  const connected = ref(true)
  const alert = ref<string | null>(null)
  let source: EventSource | null = null

  const byId = computed(() => new Map(list.value.map((c) => [c.id, c])))
  const bySlug = computed(() => new Map(list.value.map((c) => [c.slug, c])))
  // Location label for a camera, only when there is more than one location.
  const locationLabel = (c: Camera) => (locations.value.length > 1 ? (locations.value.find((l) => l.id === c.location_id)?.name ?? '') : '')
  const online = computed(() => list.value.filter((c) => c.status.state === 'running').length)

  async function load() {
    loading.value = true
    error.value = null
    try {
      ;[list.value, locations.value] = await Promise.all([api.cameras(), api.locations()])
      loaded.value = true
      for (const c of list.value) if (c.status.last_snapshot_at) snapshotAt.value[c.id] = c.status.last_snapshot_at
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  function applyStatus(st: CameraStatus) {
    const cam = list.value.find((c) => c.id === st.camera_id)
    if (cam) cam.status = st
  }

  function replace(cam: Camera) {
    const i = list.value.findIndex((c) => c.id === cam.id)
    if (i >= 0) list.value[i] = cam
  }

  function remove(id: number) {
    list.value = list.value.filter((c) => c.id !== id)
  }

  function connect() {
    if (source) return
    source = new EventSource('/api/v1/stream')
    source.onopen = () => (connected.value = true)
    source.onerror = () => (connected.value = false) // the browser retries by itself
    source.addEventListener('camera.status', (e) => applyStatus(JSON.parse((e as MessageEvent).data)))
    source.addEventListener('snapshot.updated', (e) => {
      const { camera_id, at } = JSON.parse((e as MessageEvent).data)
      snapshotAt.value[camera_id] = at
    })
    source.addEventListener('camera.removed', (e) => remove(JSON.parse((e as MessageEvent).data).camera_id))
    source.addEventListener('system.alert', (e) => (alert.value = JSON.parse((e as MessageEvent).data).message))
  }

  function disconnect() {
    source?.close()
    source = null
  }

  return { list, loading, loaded, error, snapshotAt, connected, alert, byId, bySlug, online, locations, locationLabel, load, replace, remove, connect, disconnect }
})
