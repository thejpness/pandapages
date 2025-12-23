export type ReadMode = 'scroll' | 'paged'

export type ReaderPrefs = {
  fontPx: number
  lineHeight: number
  widthPx: number
  theme: 'night' | 'warm'
  mode: ReadMode
}

const KEY = 'pp_reader_prefs_v1'

const defaults: ReaderPrefs = {
  fontPx: 20,
  lineHeight: 1.65,
  widthPx: 720,
  theme: 'night',
  mode: 'scroll',
}

export function loadPrefs(): ReaderPrefs {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return defaults
    return { ...defaults, ...JSON.parse(raw) }
  } catch {
    return defaults
  }
}

export function savePrefs(p: ReaderPrefs) {
  localStorage.setItem(KEY, JSON.stringify(p))
}
