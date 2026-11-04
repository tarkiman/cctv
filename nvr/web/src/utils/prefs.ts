// Per-device UI preferences. Storage can be unavailable (private mode, blocked site
// data), so every access is guarded and falls back to the default.
export function loadPref<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(`nvr.${key}`)
    return raw === null ? fallback : (JSON.parse(raw) as T)
  } catch {
    return fallback
  }
}

export function savePref<T>(key: string, value: T) {
  try {
    localStorage.setItem(`nvr.${key}`, JSON.stringify(value))
  } catch {
    // ignore: preferences are a convenience only
  }
}
