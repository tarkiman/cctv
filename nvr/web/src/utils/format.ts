// All times are shown in WIB regardless of the viewer's device time zone.
export const TZ = 'Asia/Jakarta'

const timeFmt = new Intl.DateTimeFormat('id-ID', { timeZone: TZ, hour: '2-digit', minute: '2-digit', hour12: false })
const timeSecFmt = new Intl.DateTimeFormat('id-ID', {
  timeZone: TZ,
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})
const dateLongFmt = new Intl.DateTimeFormat('id-ID', { timeZone: TZ, weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })
const dateTimeFmt = new Intl.DateTimeFormat('id-ID', {
  timeZone: TZ,
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})
const isoDateFmt = new Intl.DateTimeFormat('en-CA', { timeZone: TZ, year: 'numeric', month: '2-digit', day: '2-digit' })

export const fmtTime = (d: Date) => timeFmt.format(d).replace('.', ':')
export const fmtTimeSec = (d: Date) => timeSecFmt.format(d).replaceAll('.', ':')
export const fmtDateLong = (d: Date) => dateLongFmt.format(d)
export const fmtDateTime = (d: Date) => dateTimeFmt.format(d).replace('.', ':')

/** YYYY-MM-DD of d in WIB. */
export const wibDate = (d: Date) => isoDateFmt.format(d)

/** Midnight WIB of a YYYY-MM-DD date (WIB is UTC+7 with no DST). */
export const wibMidnight = (date: string) => new Date(`${date}T00:00:00+07:00`)

export const addDays = (date: string, n: number) => wibDate(new Date(wibMidnight(date).getTime() + n * 86400_000 + 3600_000))

export function fmtRelative(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return 'belum pernah'
  const s = Math.round((now - new Date(iso).getTime()) / 1000)
  if (s < 10) return 'baru saja'
  if (s < 60) return `${s} detik lalu`
  const m = Math.round(s / 60)
  if (m < 60) return `${m} menit lalu`
  const h = Math.round(m / 60)
  if (h < 24) return `${h} jam lalu`
  return `${Math.round(h / 24)} hari lalu`
}

export function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / 1024 ** i
  return `${v.toLocaleString('id-ID', { maximumFractionDigits: v < 10 ? 1 : 0 })} ${units[i]}`
}

export function fmtDuration(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d} hari ${h} jam`
  if (h > 0) return `${h} jam ${m} menit`
  return `${m} menit`
}

/** Short human label for a User-Agent, e.g. "Chrome · Android". */
export function describeDevice(ua: string): string {
  if (!ua) return 'Perangkat tidak dikenal'
  const os = /Android/.test(ua)
    ? 'Android'
    : /iPhone|iPad|iPod/.test(ua)
      ? 'iOS'
      : /Windows/.test(ua)
        ? 'Windows'
        : /Mac OS X|Macintosh/.test(ua)
          ? 'macOS'
          : /Linux/.test(ua)
            ? 'Linux'
            : ''
  const browser = /OPR\/|Opera/.test(ua)
    ? 'Opera'
    : /Edg\//.test(ua)
      ? 'Edge'
      : /SamsungBrowser/.test(ua)
        ? 'Samsung Internet'
        : /Firefox\//.test(ua)
          ? 'Firefox'
          : /Chrome\//.test(ua)
            ? 'Chrome'
            : /Safari\//.test(ua)
              ? 'Safari'
              : /curl|Go-http|python/i.test(ua)
                ? 'Skrip'
                : 'Browser'
  return os ? `${browser} · ${os}` : browser
}

/** A random password that is easy to read aloud: 4 groups of 4 lowercase letters/digits. */
export function randomPassword(): string {
  const alphabet = 'abcdefghjkmnpqrstuvwxyz23456789'
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  const chars = Array.from(bytes, (b) => alphabet[b % alphabet.length])
  return [0, 4, 8, 12].map((i) => chars.slice(i, i + 4).join('')).join('-')
}
