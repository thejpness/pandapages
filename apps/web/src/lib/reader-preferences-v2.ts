export type ReaderMode = 'scroll' | 'paged'

export type ReaderTheme = 'night' | 'warm'

export type ReaderFontFamily = 'book' | 'clear' | 'system'

export type ReaderPreferencesV2 = {
  schema: 2
  mode: ReaderMode
  theme: ReaderTheme
  fontFamily: ReaderFontFamily
  fontSize: number
  lineHeight: number
  contentWidth: number
}

export type ReaderPreferencesStorage = {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
}

export const READER_PREFERENCES_V2_KEY = 'pp_reader_prefs_v2'

export const READER_PREFERENCE_LIMITS = Object.freeze({
  fontSize: Object.freeze({ min: 17, max: 32 }),
  lineHeight: Object.freeze({ min: 1.4, max: 2 }),
  contentWidth: Object.freeze({ min: 560, max: 900 }),
})

export const READER_PREFERENCES_V2_DEFAULTS: Readonly<ReaderPreferencesV2> =
  Object.freeze({
    schema: 2,
    mode: 'scroll',
    theme: 'night',
    fontFamily: 'book',
    fontSize: 20,
    lineHeight: 1.65,
    contentWidth: 720,
  })

const preferenceKeys = [
  'schema',
  'mode',
  'theme',
  'fontFamily',
  'fontSize',
  'lineHeight',
  'contentWidth',
] as const

function defaults(): ReaderPreferencesV2 {
  return { ...READER_PREFERENCES_V2_DEFAULTS }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasExactPreferenceKeys(value: Record<string, unknown>): boolean {
  const allowed = new Set<string>(preferenceKeys)
  return (
    preferenceKeys.every((key) => Object.hasOwn(value, key)) &&
    Object.keys(value).every((key) => allowed.has(key))
  )
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.max(minimum, Math.min(maximum, value))
}

export function validateReaderPreferencesV2(
  value: unknown,
): ReaderPreferencesV2 | null {
  if (
    !isRecord(value) ||
    !hasExactPreferenceKeys(value) ||
    value.schema !== 2 ||
    (value.mode !== 'scroll' && value.mode !== 'paged') ||
    (value.theme !== 'night' && value.theme !== 'warm') ||
    (value.fontFamily !== 'book' &&
      value.fontFamily !== 'clear' &&
      value.fontFamily !== 'system') ||
    !isFiniteNumber(value.fontSize) ||
    !isFiniteNumber(value.lineHeight) ||
    !isFiniteNumber(value.contentWidth)
  ) {
    return null
  }

  return {
    schema: 2,
    mode: value.mode,
    theme: value.theme,
    fontFamily: value.fontFamily,
    fontSize: clamp(
      value.fontSize,
      READER_PREFERENCE_LIMITS.fontSize.min,
      READER_PREFERENCE_LIMITS.fontSize.max,
    ),
    lineHeight: clamp(
      value.lineHeight,
      READER_PREFERENCE_LIMITS.lineHeight.min,
      READER_PREFERENCE_LIMITS.lineHeight.max,
    ),
    contentWidth: clamp(
      value.contentWidth,
      READER_PREFERENCE_LIMITS.contentWidth.min,
      READER_PREFERENCE_LIMITS.contentWidth.max,
    ),
  }
}

export function parseReaderPreferencesV2(value: unknown): ReaderPreferencesV2 {
  return validateReaderPreferencesV2(value) ?? defaults()
}

function browserStorage(): ReaderPreferencesStorage | null {
  try {
    if (typeof localStorage === 'undefined') return null
    return localStorage
  } catch {
    return null
  }
}

export function loadReaderPreferencesV2(
  storage: ReaderPreferencesStorage | null = browserStorage(),
): ReaderPreferencesV2 {
  if (!storage) return defaults()

  try {
    const stored = storage.getItem(READER_PREFERENCES_V2_KEY)
    if (!stored) return defaults()
    return parseReaderPreferencesV2(JSON.parse(stored) as unknown)
  } catch {
    return defaults()
  }
}

export function saveReaderPreferencesV2(
  value: unknown,
  storage: ReaderPreferencesStorage | null = browserStorage(),
): boolean {
  const preferences = validateReaderPreferencesV2(value)
  if (!preferences || !storage) return false

  try {
    storage.setItem(READER_PREFERENCES_V2_KEY, JSON.stringify(preferences))
    return true
  } catch {
    return false
  }
}

export function resetReaderPreferencesV2(
  storage: ReaderPreferencesStorage | null = browserStorage(),
): ReaderPreferencesV2 {
  const preferences = defaults()
  saveReaderPreferencesV2(preferences, storage)
  return preferences
}
