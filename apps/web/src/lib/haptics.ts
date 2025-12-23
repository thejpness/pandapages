export type HapticKind = 'light' | 'select' | 'medium' | 'heavy'

const DEFAULT_ENABLED = true
const STORAGE_KEY = 'pp:haptics' // 'on' | 'off'

// Prevent “buzz spam”
let lastAt = 0
const COOLDOWN_MS = 40

function nowMs() {
  return typeof performance !== 'undefined' ? performance.now() : Date.now()
}

function canVibrate(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function'
}

function prefersReducedMotion(): boolean {
  try {
    return !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

function isEnabledByUser(): boolean {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'off') return false
    if (v === 'on') return true
  } catch {
    // ignore
  }
  return DEFAULT_ENABLED
}

export function setHapticsEnabled(enabled: boolean) {
  try {
    localStorage.setItem(STORAGE_KEY, enabled ? 'on' : 'off')
  } catch {
    // ignore
  }
}

function patternFor(kind: HapticKind): number | number[] {
  switch (kind) {
    case 'light':
      return [6]
    case 'select':
      return [4]
    case 'medium':
      return [12]
    case 'heavy':
      return [18]
    default:
      return [4]
  }
}

export function haptic(kind: HapticKind = 'select') {
  if (!isEnabledByUser()) return
  if (prefersReducedMotion()) return
  if (!canVibrate()) return

  const t = nowMs()
  if (t - lastAt < COOLDOWN_MS) return
  lastAt = t

  try {
    navigator.vibrate(patternFor(kind))
  } catch {
    // ignore
  }
}
