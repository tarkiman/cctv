// Response types of the nvrd API (nvr/docs/06-api.md).

export type CameraState = 'starting' | 'running' | 'degraded' | 'offline' | 'disabled'

export interface CameraStatus {
  camera_id: number
  state: CameraState
  message?: string
  last_segment_at: string | null
  last_snapshot_at: string | null
  restarts: number
  since: string
  detect: 'off' | 'starting' | 'running' | 'error'
  detect_error?: string
  motion: boolean
  last_motion_at: string | null
}

export interface Camera {
  id: number
  slug: string
  name: string
  has_sub_stream: boolean
  codec?: string
  width?: number
  height?: number
  record_enabled: boolean
  record_stream: 'main' | 'sub'
  retention_days: number
  event_retention_days: number
  snapshot_enabled: boolean
  timelapse_enabled: boolean
  detect_enabled: boolean
  sort_order: number
  location_id: number
  status: CameraStatus
}

export interface User {
  id: number
  username: string
  role: 'admin' | 'viewer'
}

export interface TimeRange {
  start: string
  end: string
}

export interface Timeline {
  date: string
  timezone: string
  start: string
  end: string
  ranges: TimeRange[]
  events: unknown[]
}

export interface Recording {
  id: number
  start: string
  end: string
  codec?: string
  width?: number
  height?: number
  size_bytes: number
  url: string
}

export interface RecordingAt {
  recording: Recording
  offset: number
}

export interface TimelapseItem {
  camera_id: number
  dates: string[]
  today_frames: number
  enabled: boolean
}

export interface HostStats {
  cpu_percent: number
  cpu_cores: number
  load: [number, number, number]
  mem_total: number
  mem_available: number
  swap_total: number
  swap_free: number
  disk_total: number
  disk_free: number
  uptime_seconds: number
}

export interface CameraUsage {
  camera_id: number
  bytes: number
  segments: number
  oldest_at: string | null
  last_24h_bytes: number
}

export interface SystemStats {
  host: HostStats
  cameras: CameraUsage[]
  version: string
  go2rtc_version: string
  timezone: string
  notify_targets: number
}

export interface WorkerStatus {
  id: number
  slug: string
  name: string
  status: CameraStatus
  log: string[]
}

export type Settings = Record<
  'min_free_percent' | 'timelapse_keep_days' | 'timelapse_fps' | 'detect_fps' | 'motion_threshold' | 'motion_min_area',
  number
>

export interface CameraEvent {
  id: number
  camera_id: number
  label: 'motion' | 'person'
  score: number | null
  start: string
  end: string | null
  reviewed: boolean
}

export interface ExportEstimate {
  seconds: number
  bytes: number
  segments: number
  gap_seconds: number
}

export interface UserItem {
  id: number
  username: string
  role: 'admin' | 'viewer'
  access_all: boolean
  sessions: number
  last_seen: string | null
  created_at: string
}

export interface Device {
  id: string
  current: boolean
  user_agent: string
  ip: string
  created_at: string
  last_seen: string
}

export interface SourceInput {
  url: string
  username: string
  password: string
}

export interface SourceInfo {
  url: string
  username: string
  has_password: boolean
}

export interface CameraSources {
  editable: boolean
  reason?: string
  xiaomi: boolean
  main: SourceInfo | null
  sub: SourceInfo | null
}

export interface ProbeResult {
  ok: boolean
  error?: string
  info?: { codec: string; width: number; height: number; fps: number; audio?: string }
  advice?: string[]
}

export interface Location {
  id: number
  name: string
  cameras: number
}

export interface Grants {
  access_all: boolean
  locations: number[]
  cameras: number[]
  deny_cameras: number[]
}
