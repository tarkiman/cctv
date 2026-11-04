import type {
  Camera,
  CameraSources,
  Device,
  ExportEstimate,
  RecordingAt,
  ProbeResult,
  Recording,
  Settings,
  SourceInput,
  SystemStats,
  Timeline,
  TimelapseItem,
  User,
  UserItem,
  WorkerStatus,
} from './types'

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

// Called on 401 so the app can send the user to the login page.
let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'X-Requested-With': 'nvr' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
    })
  } catch {
    throw new ApiError(0, 'network', 'Tidak bisa terhubung ke server. Periksa koneksi Anda.')
  }
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const err = data?.error ?? { code: 'unknown', message: `Kesalahan server (${res.status}).` }
    if (res.status === 401 && path !== '/auth/login') onUnauthorized?.()
    throw new ApiError(res.status, err.code, err.message)
  }
  return data as T
}

const q = (params: Record<string, string | number>) => new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString()

export const api = {
  login: (username: string, password: string) => request<User>('POST', '/auth/login', { username, password }),
  logout: () => request<void>('POST', '/auth/logout'),
  me: () => request<User>('GET', '/auth/me'),
  changePassword: (current_password: string, new_password: string) =>
    request<void>('PUT', '/auth/password', { current_password, new_password }),

  sessions: () => request<Device[]>('GET', '/auth/sessions'),
  deleteSession: (id: string) => request<void>('DELETE', `/auth/sessions/${encodeURIComponent(id)}`),
  logoutOthers: () => request<void>('POST', '/auth/sessions/logout-others'),

  users: () => request<UserItem[]>('GET', '/users'),
  createUser: (username: string, password: string, role: UserItem['role']) => request<UserItem[]>('POST', '/users', { username, password, role }),
  updateUser: (id: number, patch: { role?: UserItem['role']; password?: string }) => request<UserItem[]>('PATCH', `/users/${id}`, patch),
  deleteUser: (id: number) => request<UserItem[]>('DELETE', `/users/${id}`),

  cameras: () => request<Camera[]>('GET', '/cameras'),
  camera: (id: number) => request<Camera>('GET', `/cameras/${id}`),
  updateCamera: (id: number, patch: Partial<Camera>) => request<Camera>('PATCH', `/cameras/${id}`, patch),
  cameraSources: (id: number) => request<CameraSources>('GET', `/cameras/${id}/sources`),
  setCameraSources: (id: number, main: SourceInput, sub: SourceInput | null) =>
    request<CameraSources>('PUT', `/cameras/${id}/sources`, { main, sub }),
  createCamera: (slug: string, name: string, main: SourceInput, sub: SourceInput | null) =>
    request<Camera>('POST', '/cameras', { slug, name, main, sub }),
  deleteCamera: (id: number, confirm: string) => request<void>('DELETE', `/cameras/${id}`, { confirm }),
  probeCamera: (source: SourceInput, stream: 'main' | 'sub', cameraId?: number) =>
    request<ProbeResult>('POST', '/cameras/probe', { source, stream, camera_id: cameraId ?? 0 }),
  syncCameras: () => request<Camera[]>('POST', '/cameras/sync'),
  snapshotUrl: (id: number, bust?: string | number) => `/api/v1/cameras/${id}/snapshot.jpg${bust ? `?t=${bust}` : ''}`,

  timeline: (id: number, date: string) => request<Timeline>('GET', `/cameras/${id}/timeline?${q({ date })}`),
  recordingAt: (id: number, t: Date) => request<RecordingAt>('GET', `/cameras/${id}/recordings/at?${q({ t: t.toISOString().replace(/\.\d+Z$/, 'Z') })}`),
  recordingNext: (recordingId: number) => request<Recording>('GET', `/recordings/${recordingId}/next`),

  exportEstimate: (id: number, from: string, to: string) => request<ExportEstimate>('GET', `/cameras/${id}/export/estimate?${q({ from, to })}`),
  exportUrl: (id: number, from: string, to: string) => `/api/v1/cameras/${id}/export?${q({ from, to })}`,

  timelapse: () => request<TimelapseItem[]>('GET', '/timelapse'),
  timelapseUrl: (cameraId: number, date: string) => `/api/v1/timelapse/${cameraId}/${date}.mp4`,

  stats: () => request<SystemStats>('GET', '/system/stats'),
  workers: () => request<WorkerStatus[]>('GET', '/system/status'),
  notifyTest: () => request<void>('POST', '/system/notify-test'),
  settings: () => request<Settings>('GET', '/settings'),
  saveSettings: (s: Partial<Settings>) => request<Settings>('PUT', '/settings', s),

  liveWsUrl: (cameraId: number, quality: 'main' | 'sub') => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}/api/v1/live/ws?${q({ camera: cameraId, quality })}`
  },
}
