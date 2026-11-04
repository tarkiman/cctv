<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import {
  CircleCheck,
  Download,
  LoaderCircle,
  LogOut,
  Plus,
  RefreshCw,
  Share,
} from "lucide-vue-next";
import { api, ApiError } from "../api/client";
import type { Camera, Settings, SystemStats } from "../api/types";
import { useAuth } from "../stores/auth";
import { useCameras } from "../stores/cameras";
import { useToast } from "../stores/toast";
import { fmtBytes } from "../utils/format";
import { loadPref, savePref } from "../utils/prefs";
import { usePwaInstall } from "../composables/pwa";
import UsersSection from "../components/UsersSection.vue";
import AddCameraPanel from "../components/AddCameraPanel.vue";
import CameraSourcesPanel from "../components/CameraSourcesPanel.vue";
import DevicesSection from "../components/DevicesSection.vue";

const auth = useAuth();
const cams = useCameras();
const toast = useToast();
const router = useRouter();
const isAdmin = computed(() => auth.user?.role === "admin");

// --- cameras ---
type Draft = Pick<
  Camera,
  | "name"
  | "record_enabled"
  | "record_stream"
  | "retention_days"
  | "snapshot_enabled"
  | "timelapse_enabled"
>;
const drafts = reactive<Record<number, Draft>>({});
const saving = ref<number | null>(null);
const syncing = ref(false);
const adding = ref(false);

function onAdded(cam: Camera) {
  adding.value = false;
  cams.list.push(cam);
  cams.load();
  toast.ok(`${cam.name} ditambahkan. Perekaman dimulai dalam beberapa detik.`);
}

function draftFor(c: Camera): Draft {
  if (!drafts[c.id]) {
    drafts[c.id] = {
      name: c.name,
      record_enabled: c.record_enabled,
      record_stream: c.record_stream,
      retention_days: c.retention_days,
      snapshot_enabled: c.snapshot_enabled,
      timelapse_enabled: c.timelapse_enabled,
    };
  }
  return drafts[c.id]!;
}

const dirty = (c: Camera) => {
  const d = drafts[c.id];
  return !!d && (Object.keys(d) as (keyof Draft)[]).some((k) => d[k] !== c[k]);
};

async function saveCamera(c: Camera) {
  const d = drafts[c.id];
  if (!d) return;
  if (!d.name.trim()) return toast.error("Nama kamera wajib diisi.");
  saving.value = c.id;
  try {
    const updated = await api.updateCamera(c.id, { ...d, name: d.name.trim() });
    cams.replace(updated);
    delete drafts[c.id];
    toast.ok(`Pengaturan ${updated.name} disimpan. Perekam dimulai ulang.`);
  } catch (e) {
    toast.error(e instanceof ApiError ? e.message : "Gagal menyimpan.");
  } finally {
    saving.value = null;
  }
}

async function sync() {
  syncing.value = true;
  try {
    cams.list = await api.syncCameras();
    toast.ok("Daftar kamera disinkronkan dengan go2rtc.");
  } catch (e) {
    toast.error((e as Error).message);
  } finally {
    syncing.value = false;
  }
}

// --- global settings & disk estimate ---
const settings = ref<Settings | null>(null);
const stats = ref<SystemStats | null>(null);
const savingSettings = ref(false);

// Estimate = each camera's last-24h size × its retention days.
const estimate = computed(() => {
  if (!stats.value) return null;
  let bytes = 0;
  for (const c of cams.list) {
    const d = drafts[c.id] ?? c;
    if (!d.record_enabled) continue;
    bytes +=
      (stats.value.cameras.find((u) => u.camera_id === c.id)?.last_24h_bytes ??
        0) * d.retention_days;
  }
  const usable =
    stats.value.host.disk_free +
    stats.value.cameras.reduce((n, u) => n + u.bytes, 0);
  return { bytes, usable };
});

async function saveSettings() {
  if (!settings.value) return;
  savingSettings.value = true;
  try {
    settings.value = await api.saveSettings(settings.value);
    toast.ok("Pengaturan disimpan.");
  } catch (e) {
    toast.error((e as Error).message);
  } finally {
    savingSettings.value = false;
  }
}

// --- appearance ---
const theme = ref<"system" | "dark" | "light">(loadPref("theme", "system"));
function setTheme(t: typeof theme.value) {
  theme.value = t;
  savePref("theme", t);
  if (t === "system") document.documentElement.removeAttribute("data-theme");
  else document.documentElement.dataset.theme = t;
}

// --- install as app ---
const pwa = usePwaInstall();
async function installApp() {
  if (await pwa.install())
    toast.ok("Aplikasi dipasang. Buka dari layar utama.");
}

// --- account ---
const pw = reactive({ current: "", next: "", confirm: "" });
const pwBusy = ref(false);
const pwError = ref("");
async function changePassword() {
  pwError.value = "";
  if (pw.next.length < 10)
    return (pwError.value = "Password baru minimal 10 karakter.");
  if (pw.next !== pw.confirm)
    return (pwError.value = "Konfirmasi password tidak sama.");
  pwBusy.value = true;
  try {
    await api.changePassword(pw.current, pw.next);
    Object.assign(pw, { current: "", next: "", confirm: "" });
    toast.ok("Password diganti. Perangkat lain sudah dikeluarkan.");
  } catch (e) {
    pwError.value =
      e instanceof ApiError ? e.message : "Gagal mengganti password.";
  } finally {
    pwBusy.value = false;
  }
}

async function logout() {
  await auth.logout();
  cams.disconnect();
  router.replace({ name: "login" });
}

onMounted(async () => {
  if (!isAdmin.value) return;
  try {
    [settings.value, stats.value] = await Promise.all([
      api.settings(),
      api.stats(),
    ]);
  } catch (e) {
    toast.error((e as Error).message);
  }
});

const input =
  "w-full rounded-lg border border-line bg-bg px-3 py-2 text-sm outline-none focus:border-primary";
</script>

<template>
  <div class="max-w-4xl">
    <header class="mb-6">
      <h1 class="text-xl font-bold">Pengaturan</h1>
    </header>

    <!-- Cameras -->
    <section v-if="isAdmin" class="mb-8" aria-labelledby="cams-h">
      <div class="mb-3 flex items-center gap-3">
        <h2 id="cams-h" class="mr-auto text-base font-semibold">Kamera</h2>
        <button
          type="button"
          class="grid size-10 place-items-center rounded-lg border border-line hover:bg-surface-2"
          :disabled="syncing"
          title="Sinkronkan dengan go2rtc (ambil kamera yang ditambahkan langsung di go2rtc)"
          aria-label="Sinkronkan dengan go2rtc"
          @click="sync"
        >
          <RefreshCw
            class="size-4"
            :class="syncing && 'animate-spin'"
            aria-hidden="true"
          />
        </button>
        <button
          v-if="!adding"
          type="button"
          class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-3 text-sm font-semibold text-on-primary"
          @click="adding = true"
        >
          <Plus class="size-4" aria-hidden="true" /> Tambah kamera
        </button>
      </div>
      <p class="mb-4 text-sm text-muted">
        Tambah kamera RTSP di sini, atau ubah alamatnya di
        <em>Sumber &amp; koneksi</em> bila IP kamera berubah. Stream yang
        ditambahkan langsung di go2rtc ikut muncul setelah disinkronkan.
      </p>

      <AddCameraPanel v-if="adding" @added="onAdded" @cancel="adding = false" />

      <div class="grid gap-4 md:grid-cols-2">
        <div
          v-for="c in cams.list"
          :key="c.id"
          class="rounded-[var(--radius-card)] border border-line bg-surface p-4"
        >
          <form @submit.prevent="saveCamera(c)">
            <div class="mb-3 flex items-center justify-between gap-2">
              <span class="text-xs text-muted"
                >ID stream: <code>{{ c.slug }}</code></span
              >
              <span class="text-xs text-muted tabular">{{
                c.codec ? `${c.codec.toUpperCase()} ${c.width}×${c.height}` : ""
              }}</span>
            </div>
            <label class="mb-1 block text-sm font-medium" :for="`name-${c.id}`"
              >Nama tampilan</label
            >
            <input
              :id="`name-${c.id}`"
              v-model="draftFor(c).name"
              :class="input"
              maxlength="60"
              required
            />

            <fieldset class="mt-4 space-y-2.5">
              <legend class="sr-only">Opsi</legend>
              <label class="flex items-center justify-between gap-3 text-sm">
                <span>Rekam 24 jam</span>
                <input
                  v-model="draftFor(c).record_enabled"
                  type="checkbox"
                  class="size-5 accent-[var(--primary)]"
                />
              </label>
              <label class="flex items-center justify-between gap-3 text-sm">
                <span>Snapshot per menit</span>
                <input
                  v-model="draftFor(c).snapshot_enabled"
                  type="checkbox"
                  class="size-5 accent-[var(--primary)]"
                />
              </label>
              <label class="flex items-center justify-between gap-3 text-sm">
                <span>Timelapse harian</span>
                <input
                  v-model="draftFor(c).timelapse_enabled"
                  type="checkbox"
                  class="size-5 accent-[var(--primary)]"
                />
              </label>
            </fieldset>

            <div class="mt-4 grid grid-cols-2 gap-3">
              <div>
                <label
                  class="mb-1 block text-sm font-medium"
                  :for="`ret-${c.id}`"
                  >Retensi (hari)</label
                >
                <input
                  :id="`ret-${c.id}`"
                  v-model.number="draftFor(c).retention_days"
                  type="number"
                  min="1"
                  max="365"
                  :class="input"
                />
              </div>
              <div>
                <label
                  class="mb-1 block text-sm font-medium"
                  :for="`rs-${c.id}`"
                  >Stream direkam</label
                >
                <select
                  :id="`rs-${c.id}`"
                  v-model="draftFor(c).record_stream"
                  :class="input"
                >
                  <option value="main">Utama (HD)</option>
                  <option value="sub" :disabled="!c.has_sub_stream">
                    Sub (SD){{ c.has_sub_stream ? "" : " — tidak ada" }}
                  </option>
                </select>
              </div>
            </div>

            <div class="mt-4 flex justify-end">
              <button
                type="submit"
                class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary disabled:opacity-50"
                :disabled="!dirty(c) || saving === c.id"
              >
                <LoaderCircle
                  v-if="saving === c.id"
                  class="size-4 animate-spin"
                  aria-hidden="true"
                />
                Simpan
              </button>
            </div>
          </form>
          <CameraSourcesPanel :camera="c" />
        </div>
      </div>
    </section>

    <!-- Recording & storage -->
    <section v-if="isAdmin && settings" class="mb-8" aria-labelledby="rec-h">
      <h2 id="rec-h" class="mb-3 text-base font-semibold">
        Rekaman & penyimpanan
      </h2>
      <form
        class="rounded-[var(--radius-card)] border border-line bg-surface p-4"
        @submit.prevent="saveSettings"
      >
        <p v-if="estimate" class="mb-4 rounded-lg bg-surface-2 p-3 text-sm">
          Dengan retensi saat ini, perkiraan pemakaian
          <strong class="tabular">{{ fmtBytes(estimate.bytes) }}</strong> dari
          <strong class="tabular">{{ fmtBytes(estimate.usable) }}</strong> yang
          bisa dipakai
          <span class="text-muted"
            >(berdasarkan ukuran rekaman 24 jam terakhir)</span
          >.
        </p>
        <div class="grid gap-4 sm:grid-cols-3">
          <div>
            <label class="mb-1 block text-sm font-medium" for="minfree"
              >Sisa disk minimal (%)</label
            >
            <input
              id="minfree"
              v-model.number="settings.min_free_percent"
              type="number"
              min="1"
              max="50"
              :class="input"
            />
            <p class="mt-1 text-xs text-muted">
              Di bawah ini, rekaman tertua dihapus otomatis.
            </p>
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium" for="tlkeep"
              >Simpan frame timelapse (hari)</label
            >
            <input
              id="tlkeep"
              v-model.number="settings.timelapse_keep_days"
              type="number"
              min="0"
              max="30"
              :class="input"
            />
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium" for="tlfps"
              >FPS timelapse</label
            >
            <input
              id="tlfps"
              v-model.number="settings.timelapse_fps"
              type="number"
              min="1"
              max="60"
              :class="input"
            />
          </div>
        </div>
        <div class="mt-4 flex justify-end">
          <button
            type="submit"
            class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary"
            :disabled="savingSettings"
          >
            <LoaderCircle
              v-if="savingSettings"
              class="size-4 animate-spin"
              aria-hidden="true"
            />
            Simpan
          </button>
        </div>
      </form>
    </section>

    <UsersSection v-if="isAdmin" />

    <!-- Appearance -->
    <section class="mb-8" aria-labelledby="look-h">
      <h2 id="look-h" class="mb-3 text-base font-semibold">Tampilan</h2>
      <div
        class="flex w-fit overflow-hidden rounded-lg border border-line"
        role="group"
        aria-label="Tema"
      >
        <button
          v-for="t in [
            ['system', 'Ikuti sistem'],
            ['dark', 'Gelap'],
            ['light', 'Terang'],
          ] as const"
          :key="t[0]"
          type="button"
          class="min-h-10 px-4 text-sm font-semibold"
          :class="
            theme === t[0] ? 'bg-primary text-on-primary' : 'hover:bg-surface-2'
          "
          :aria-pressed="theme === t[0]"
          @click="setTheme(t[0])"
        >
          {{ t[1] }}
        </button>
      </div>
    </section>

    <!-- Install as app -->
    <section class="mb-8" aria-labelledby="app-h">
      <h2 id="app-h" class="mb-3 text-base font-semibold">Aplikasi</h2>
      <div
        class="flex max-w-2xl items-start gap-4 rounded-[var(--radius-card)] border border-line bg-surface p-4"
      >
        <img
          src="/icons/icon-192.png"
          alt=""
          class="size-12 shrink-0 rounded-xl"
        />
        <div class="min-w-0 flex-1 text-sm">
          <p class="font-semibold">Pasang CCTV Rumah di perangkat ini</p>
          <p class="mt-0.5 text-muted">
            Buka langsung dari layar utama, layar penuh tanpa bilah browser, dan
            lebih cepat terbuka.
          </p>
          <p
            v-if="pwa.installed.value"
            class="mt-3 flex items-center gap-1.5 font-semibold text-ok"
          >
            <CircleCheck class="size-4" aria-hidden="true" /> Sudah terpasang
          </p>
          <button
            v-else-if="pwa.canPrompt.value"
            type="button"
            class="mt-3 flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 font-semibold text-on-primary"
            @click="installApp"
          >
            <Download class="size-4" aria-hidden="true" /> Pasang aplikasi
          </button>
          <p
            v-else-if="pwa.iosManual.value"
            class="mt-3 rounded-lg bg-surface-2 p-3"
          >
            Di <strong>Safari</strong>: ketuk
            <Share
              class="inline size-4 align-text-bottom"
              aria-label="Bagikan"
            />
            <strong>Bagikan</strong>, lalu
            <strong>Tambah ke Layar Utama</strong>.
          </p>
          <p v-else-if="!pwa.secure" class="mt-3 rounded-lg bg-surface-2 p-3">
            Pemasangan butuh HTTPS. Buka lewat
            <strong>https://cctv.tarkiman.com</strong>.
          </p>
          <p v-else class="mt-3 rounded-lg bg-surface-2 p-3">
            Buka menu browser (⋮), lalu pilih
            <strong>Instal aplikasi</strong> atau
            <strong>Tambahkan ke layar utama</strong>.
          </p>
        </div>
      </div>
    </section>

    <!-- Account -->
    <section class="mb-8" aria-labelledby="acc-h">
      <h2 id="acc-h" class="mb-3 text-base font-semibold">Akun</h2>
      <form
        class="max-w-md rounded-[var(--radius-card)] border border-line bg-surface p-4"
        @submit.prevent="changePassword"
      >
        <p class="mb-4 text-sm text-muted">
          Masuk sebagai
          <strong class="text-fg">{{ auth.user?.username }}</strong>
        </p>
        <input
          type="text"
          autocomplete="username"
          :value="auth.user?.username"
          class="sr-only"
          tabindex="-1"
          aria-hidden="true"
          readonly
        />
        <label class="mb-1 block text-sm font-medium" for="pw-cur"
          >Password saat ini</label
        >
        <input
          id="pw-cur"
          v-model="pw.current"
          type="password"
          autocomplete="current-password"
          :class="input"
          class="mb-3"
        />
        <label class="mb-1 block text-sm font-medium" for="pw-new"
          >Password baru</label
        >
        <input
          id="pw-new"
          v-model="pw.next"
          type="password"
          autocomplete="new-password"
          minlength="10"
          :class="input"
          class="mb-1"
          aria-describedby="pw-hint"
        />
        <p id="pw-hint" class="mb-3 text-xs text-muted">Minimal 10 karakter.</p>
        <label class="mb-1 block text-sm font-medium" for="pw-conf"
          >Ulangi password baru</label
        >
        <input
          id="pw-conf"
          v-model="pw.confirm"
          type="password"
          autocomplete="new-password"
          :class="input"
          class="mb-3"
        />
        <p v-if="pwError" class="mb-3 text-sm text-danger" role="alert">
          {{ pwError }}
        </p>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <button
            type="button"
            class="flex min-h-10 items-center gap-2 rounded-lg px-3 text-sm font-semibold text-danger hover:bg-danger/10"
            @click="logout"
          >
            <LogOut class="size-4" aria-hidden="true" /> Keluar
          </button>
          <button
            type="submit"
            class="flex min-h-10 items-center gap-2 rounded-lg bg-primary px-4 text-sm font-semibold text-on-primary"
            :disabled="pwBusy || !pw.current"
          >
            <LoaderCircle
              v-if="pwBusy"
              class="size-4 animate-spin"
              aria-hidden="true"
            />
            Ganti password
          </button>
        </div>
      </form>
      <DevicesSection />
    </section>
  </div>
</template>
