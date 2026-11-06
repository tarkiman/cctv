<script setup lang="ts">
// "Cari kamera": scan the server's network, then add a new camera with its streams
// filled in (ONVIF or common RTSP paths), fix a camera whose IP changed, or hide a device.
import { computed, reactive, ref } from 'vue'
import { ArrowRight, CircleAlert, EyeOff, LoaderCircle, Radar, RefreshCw, Router, Search, Video } from 'lucide-vue-next'
import { api, ApiError } from '../api/client'
import type { CameraDraft, DiscoverResult, DiscoverStreams, FoundDevice } from '../api/types'
import { useCameras } from '../stores/cameras'
import { useToast } from '../stores/toast'

const emit = defineEmits<{ add: [draft: CameraDraft]; close: [] }>()
const cams = useCameras()
const toast = useToast()

const scanning = ref(false)
const result = ref<DiscoverResult | null>(null)
const showKnown = ref(false)
const err = (e: unknown, f: string) => toast.error(e instanceof ApiError ? e.message : f)

async function scan() {
  scanning.value = true
  try {
    result.value = await api.discover()
  } catch (e) {
    err(e, 'Pemindaian gagal.')
  } finally {
    scanning.value = false
  }
}

const by = (st: FoundDevice['status']) => computed(() => result.value?.devices.filter((d) => d.status === st) ?? [])
const fresh = by('new')
const changed = by('ip_changed')
const known = computed(() => result.value?.devices.filter((d) => d.status === 'registered' || d.status === 'gateway') ?? [])
const hidden = by('ignored')

const title = (d: FoundDevice) =>
  d.onvif?.name && d.onvif.name !== 'IP-Camera' ? d.onvif.name : d.onvif ? `Kamera ONVIF${d.onvif.hardware ? ` (${d.onvif.hardware})` : ''}` : d.gateway ? 'Gerbang go2rtc' : 'Perangkat RTSP'
const key = (d: FoundDevice) => d.mac || d.ips[0]!

// Per-device "add" state.
type Step = { open: boolean; username: string; password: string; busy: boolean; res: DiscoverStreams | null; main: string; sub: string }
const steps = reactive<Record<string, Step>>({})
const step = (d: FoundDevice) => (steps[key(d)] ??= { open: false, username: 'admin', password: '', busy: false, res: null, main: '', sub: '' })

async function findStreams(d: FoundDevice) {
  const s = step(d)
  s.busy = true
  s.res = null
  try {
    const ip = d.onvif ? new URL(d.onvif.xaddr).hostname : d.ips[0]!
    s.res = await api.discoverStreams({ ip, port: d.ports[0] ?? 554, onvif_xaddr: d.onvif?.xaddr, username: s.username, password: s.password })
    if (s.res.streams?.length) {
      s.main = s.res.streams[0]!.url
      s.sub = s.res.streams.length > 1 ? s.res.streams[s.res.streams.length - 1]!.url : ''
    } else if (s.res.guess) {
      s.main = s.res.guess.main
      s.sub = s.res.guess.sub ?? ''
    }
  } catch (e) {
    err(e, 'Gagal memeriksa kamera.')
  } finally {
    s.busy = false
  }
}

function proceed(d: FoundDevice) {
  const s = step(d)
  const creds = { username: s.username, password: s.password }
  emit('add', {
    name: d.onvif?.name && d.onvif.name !== 'IP-Camera' ? d.onvif.name : '',
    main: { url: s.main, ...creds },
    sub: s.sub ? { url: s.sub, ...creds } : null,
    mac: d.mac,
  })
}

const linkTo = reactive<Record<string, number>>({})
async function link(d: FoundDevice) {
  const id = linkTo[key(d)]
  if (!d.mac || !id) return
  try {
    await api.linkDevice(d.mac, id)
    toast.ok('Perangkat ditandai sebagai kamera terdaftar.')
    scan()
  } catch (e) {
    err(e, 'Gagal menandai perangkat.')
  }
}

async function ignore(d: FoundDevice, on: boolean) {
  if (!d.mac) return
  try {
    await api.ignoreDevice(d.mac, on)
    scan()
  } catch (e) {
    err(e, 'Gagal menyimpan.')
  }
}

// IP changed: swap the old host for the new one in the camera's URLs; the stored
// password is kept because the username is unchanged.
const fixing = ref<number | null>(null)
async function fixIP(d: FoundDevice) {
  if (!d.camera_id || !d.old_host) return
  fixing.value = d.camera_id
  const newHost = d.ips[0]!
  const swap = (u: string) => u.replace(`//${d.old_host}`, `//${newHost}`)
  try {
    const cur = await api.cameraSources(d.camera_id)
    if (!cur.main) throw new Error('Sumber kamera ini tidak bisa diubah dari sini.')
    await api.setCameraSources(
      d.camera_id,
      { url: swap(cur.main.url), username: cur.main.username, password: '' },
      cur.sub ? { url: swap(cur.sub.url), username: cur.sub.username, password: '' } : null,
    )
    toast.ok(`${d.camera_name}: sumber diperbarui ke ${newHost}. Perekam tersambung ulang.`)
    scan()
  } catch (e) {
    err(e, 'Gagal memperbarui sumber.')
  } finally {
    fixing.value = null
  }
}

const res = (w?: number, h?: number) => (w && h ? `${w}×${h}` : '')
const input = 'w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary'
scan()
</script>

<template>
  <div class="mb-4 rounded-[var(--radius-card)] border border-primary/40 bg-surface p-4">
    <div class="mb-3 flex items-center gap-2">
      <Radar class="size-4 text-primary" aria-hidden="true" />
      <h3 class="mr-auto text-sm font-semibold">Cari kamera di jaringan</h3>
      <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-md border border-line px-2.5 text-xs font-semibold hover:bg-surface-2 disabled:opacity-50" :disabled="scanning" @click="scan">
        <RefreshCw class="size-3.5" :class="scanning && 'animate-spin'" aria-hidden="true" /> Pindai ulang
      </button>
      <button type="button" class="min-h-9 rounded-md px-2.5 text-xs font-semibold hover:bg-surface-2" @click="emit('close')">Tutup</button>
    </div>

    <div v-if="scanning" class="flex items-center gap-2 rounded-lg bg-surface-2 p-3 text-sm" role="status">
      <LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
      Memindai {{ result?.subnets.map((s) => s.cidr).join(', ') || 'jaringan lokal' }} — ONVIF dan port RTSP (±5 detik)…
    </div>

    <template v-else-if="result">
      <p class="mb-3 text-xs text-muted">
        Jaringan: {{ result.subnets.map((s) => s.cidr).join(', ') || '—' }}. Kamera Xiaomi (P2P) tidak terlihat lewat pemindaian; tambahkan lewat dashboard go2rtc lalu
        Sinkronkan. Kamera di lokasi lain (lewat gerbang) belum bisa dipindai dari sini.
      </p>

      <!-- IP changed -->
      <div v-for="d in changed" :key="key(d)" class="mb-2 flex flex-wrap items-center gap-2 rounded-lg border border-warn/50 bg-warn/10 p-3 text-sm">
        <CircleAlert class="size-4 shrink-0 text-warn" aria-hidden="true" />
        <span class="mr-auto"><strong>{{ d.camera_name }}</strong> pindah IP: {{ d.old_host }} → <strong>{{ d.ips[0] }}</strong></span>
        <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-semibold text-on-primary disabled:opacity-60" :disabled="fixing === d.camera_id" @click="fixIP(d)">
          <LoaderCircle v-if="fixing === d.camera_id" class="size-3.5 animate-spin" aria-hidden="true" /> Perbarui sumber
        </button>
      </div>

      <!-- New devices -->
      <h4 class="mt-2 mb-2 text-xs font-semibold tracking-wide text-muted uppercase">Belum terdaftar ({{ fresh.length }})</h4>
      <p v-if="!fresh.length" class="mb-2 text-sm text-muted">Tidak ada kamera baru ditemukan.</p>
      <div v-for="d in fresh" :key="key(d)" class="mb-2 rounded-lg border border-line p-3 text-sm">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <Video class="size-4 shrink-0 text-muted" aria-hidden="true" />
          <span class="font-semibold">{{ title(d) }}</span>
          <span class="text-xs text-muted tabular">{{ d.ips.join(', ') }} · port {{ d.ports.join(', ') || '—' }}<template v-if="d.onvif"> · ONVIF</template></span>
          <span v-if="d.server" class="text-xs text-muted">· {{ d.server }}</span>
          <span v-if="d.mac" class="ml-auto font-mono text-[11px] text-muted">{{ d.mac }}</span>
        </div>

        <div class="mt-2 flex flex-wrap items-center gap-2">
          <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-semibold text-on-primary" @click="step(d).open = !step(d).open">
            <Search class="size-3.5" aria-hidden="true" /> Tambahkan
          </button>
          <template v-if="d.mac && cams.list.length">
            <select v-model.number="linkTo[key(d)]" class="min-h-9 rounded-md border border-line bg-surface px-2 text-xs" :aria-label="`Tandai ${d.ips[0]} sebagai kamera terdaftar`">
              <option :value="undefined">Ini kamera…</option>
              <option v-for="c in cams.list" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <button v-if="linkTo[key(d)]" type="button" class="min-h-9 rounded-md border border-line px-2.5 text-xs font-semibold hover:bg-surface-2" @click="link(d)">Tandai</button>
          </template>
          <button v-if="d.mac" type="button" class="ml-auto flex min-h-9 items-center gap-1 rounded-md px-2 text-xs text-muted hover:bg-surface-2" @click="ignore(d, true)">
            <EyeOff class="size-3.5" aria-hidden="true" /> Abaikan
          </button>
        </div>

        <div v-if="step(d).open" class="mt-3 space-y-2 rounded-md bg-bg p-3">
          <p class="text-xs text-muted">
            Login kamera (sama dengan aplikasi kamera).
            {{ d.onvif ? 'URL stream utama & sub-stream diambil langsung dari kamera lewat ONVIF.' : 'Path stream dicoba dari daftar merek umum.' }}
          </p>
          <div class="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
            <input v-model.trim="step(d).username" :class="input" autocomplete="off" autocapitalize="none" placeholder="username" aria-label="Username kamera" />
            <input v-model="step(d).password" :class="input" type="password" autocomplete="new-password" placeholder="password" aria-label="Password kamera" />
            <button type="button" class="flex min-h-10 items-center justify-center gap-1.5 rounded-lg border border-line px-3 text-sm font-semibold hover:bg-surface-2 disabled:opacity-50" :disabled="step(d).busy" @click="findStreams(d)">
              <LoaderCircle v-if="step(d).busy" class="size-4 animate-spin" aria-hidden="true" /> Cari stream
            </button>
          </div>

          <template v-if="step(d).res">
            <p v-if="step(d).res!.error" class="flex items-start gap-1.5 text-xs text-danger"><CircleAlert class="mt-px size-3.5 shrink-0" aria-hidden="true" />{{ step(d).res!.error }}</p>
            <div v-if="step(d).res!.streams?.length" class="space-y-1 text-xs">
              <p class="font-semibold">Stream dari ONVIF:</p>
              <label v-for="st in step(d).res!.streams" :key="st.url" class="flex items-center gap-2">
                <span class="w-24 text-muted">{{ st.profile }}</span>
                <span class="tabular">{{ res(st.width, st.height) }}</span>
                <code class="truncate text-muted">{{ st.url }}</code>
              </label>
            </div>
            <p v-else-if="step(d).res!.guess" class="text-xs">
              Ditemukan pola <strong>{{ step(d).res!.guess!.brand }}</strong> ({{ step(d).res!.guess!.codec?.toUpperCase() || '?' }}{{ step(d).res!.guess!.sub ? ' + sub-stream' : '' }}).
            </p>
            <div v-if="step(d).main" class="grid gap-2 text-xs sm:grid-cols-2">
              <label>Stream utama (direkam)<input v-model.trim="step(d).main" :class="input" class="mt-1 font-mono text-[12px]" /></label>
              <label>Sub-stream (live ringan)<input v-model.trim="step(d).sub" :class="input" class="mt-1 font-mono text-[12px]" placeholder="opsional" /></label>
            </div>
            <div v-if="step(d).main" class="flex justify-end">
              <button type="button" class="flex min-h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-semibold text-on-primary" @click="proceed(d)">
                Lanjut ke form tambah kamera <ArrowRight class="size-3.5" aria-hidden="true" />
              </button>
            </div>
          </template>
        </div>
      </div>

      <!-- Known / gateways / ignored -->
      <button type="button" class="mt-2 text-xs font-semibold text-muted hover:text-fg" @click="showKnown = !showKnown">
        {{ showKnown ? 'Sembunyikan' : 'Tampilkan' }} perangkat terdaftar &amp; lainnya ({{ known.length + hidden.length }})
      </button>
      <ul v-if="showKnown" class="mt-2 space-y-1 text-xs">
        <li v-for="d in known" :key="key(d)" class="flex items-center gap-2 text-muted">
          <component :is="d.gateway ? Router : Video" class="size-3.5" aria-hidden="true" />
          <span class="text-fg">{{ d.gateway ? 'Gerbang go2rtc' : d.camera_name }}</span>
          <span class="tabular">{{ d.ips.join(', ') }}</span>
          <span v-if="d.status === 'registered'" class="rounded bg-ok/15 px-1.5 text-ok">terdaftar</span>
        </li>
        <li v-for="d in hidden" :key="key(d)" class="flex items-center gap-2 text-muted">
          <EyeOff class="size-3.5" aria-hidden="true" />
          <span>{{ title(d) }} · {{ d.ips.join(', ') }} (diabaikan)</span>
          <button type="button" class="font-semibold text-primary" @click="ignore(d, false)">Tampilkan lagi</button>
        </li>
      </ul>
    </template>
  </div>
</template>
