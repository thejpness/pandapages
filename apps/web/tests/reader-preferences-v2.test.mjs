import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function preferencesModule() {
  return (
    await loadTypeScript(
      '../src/lib/reader-preferences-v2.ts',
      import.meta.url,
    )
  ).module
}

function validPreferences(overrides = {}) {
  return {
    schema: 2,
    mode: 'scroll',
    theme: 'night',
    fontFamily: 'book',
    fontSize: 20,
    lineHeight: 1.65,
    contentWidth: 720,
    ...overrides,
  }
}

function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial))
  return {
    values,
    getItem(key) {
      return values.get(key) ?? null
    },
    setItem(key, value) {
      values.set(key, value)
    },
  }
}

test('preference v2 parsing accepts the exact schema and clamps numeric values', async () => {
  const preferences = await preferencesModule()
  const parsed = preferences.parseReaderPreferencesV2(
    validPreferences({
      mode: 'paged',
      theme: 'warm',
      fontFamily: 'clear',
      fontSize: 200,
      lineHeight: -1,
      contentWidth: 1200,
    }),
  )

  assert.deepEqual(parsed, {
    schema: 2,
    mode: 'paged',
    theme: 'warm',
    fontFamily: 'clear',
    fontSize: 32,
    lineHeight: 1.4,
    contentWidth: 900,
  })
  assert.equal(
    preferences.parseReaderPreferencesV2(
      validPreferences({ fontSize: 17, lineHeight: 2, contentWidth: 560 }),
    ).fontSize,
    17,
  )
})

test('malformed, incomplete, unknown and non-finite preferences use defaults', async () => {
  const preferences = await preferencesModule()
  const expected = { ...preferences.READER_PREFERENCES_V2_DEFAULTS }

  for (const invalid of [
    null,
    [],
    {},
    { ...validPreferences(), schema: 1 },
    { ...validPreferences(), mode: 'continuous' },
    { ...validPreferences(), theme: 'bright' },
    { ...validPreferences(), fontFamily: 'serif' },
    { ...validPreferences(), fontSize: '20' },
    { ...validPreferences(), lineHeight: Number.NaN },
    { ...validPreferences(), contentWidth: Number.POSITIVE_INFINITY },
    { ...validPreferences(), extra: true },
  ]) {
    assert.deepEqual(preferences.parseReaderPreferencesV2(invalid), expected)
    assert.equal(preferences.validateReaderPreferencesV2(invalid), null)
  }
})

test('loading is strict, ignores v1 data and treats malformed JSON as defaults', async () => {
  const preferences = await preferencesModule()
  const expected = { ...preferences.READER_PREFERENCES_V2_DEFAULTS }
  const onlyV1 = memoryStorage({
    pp_reader_prefs_v1: JSON.stringify({ fontPx: 31, mode: 'paged' }),
  })
  assert.deepEqual(preferences.loadReaderPreferencesV2(onlyV1), expected)

  const malformed = memoryStorage({ pp_reader_prefs_v2: '{not json' })
  assert.deepEqual(preferences.loadReaderPreferencesV2(malformed), expected)

  const valid = validPreferences({ fontFamily: 'system', contentWidth: 880 })
  const current = memoryStorage({ pp_reader_prefs_v2: JSON.stringify(valid) })
  assert.deepEqual(preferences.loadReaderPreferencesV2(current), valid)
})

test('defaults are immutable and callers never share a mutable defaults object', async () => {
  const preferences = await preferencesModule()
  assert.equal(Object.isFrozen(preferences.READER_PREFERENCES_V2_DEFAULTS), true)
  assert.equal(Object.isFrozen(preferences.READER_PREFERENCE_LIMITS), true)

  const first = preferences.parseReaderPreferencesV2(null)
  const second = preferences.parseReaderPreferencesV2(null)
  assert.notEqual(first, second)
  first.fontSize = 31
  first.theme = 'warm'

  assert.deepEqual(second, preferences.READER_PREFERENCES_V2_DEFAULTS)
  assert.deepEqual(
    preferences.loadReaderPreferencesV2(memoryStorage()),
    preferences.READER_PREFERENCES_V2_DEFAULTS,
  )
})

test('localStorage read and write failures are non-fatal', async () => {
  const preferences = await preferencesModule()
  const readFailure = {
    getItem() {
      throw new Error('storage unavailable')
    },
    setItem() {},
  }
  assert.deepEqual(
    preferences.loadReaderPreferencesV2(readFailure),
    preferences.READER_PREFERENCES_V2_DEFAULTS,
  )

  const writeFailure = {
    getItem() {
      return null
    },
    setItem() {
      throw new Error('quota exceeded')
    },
  }
  assert.equal(
    preferences.saveReaderPreferencesV2(validPreferences(), writeFailure),
    false,
  )
  assert.doesNotThrow(() =>
    preferences.resetReaderPreferencesV2(writeFailure),
  )
})

test('saving persists only validated and clamped v2 preferences', async () => {
  const preferences = await preferencesModule()
  const storage = memoryStorage()
  assert.equal(
    preferences.saveReaderPreferencesV2(
      validPreferences({ fontSize: 100, contentWidth: 100 }),
      storage,
    ),
    true,
  )
  assert.deepEqual(JSON.parse(storage.values.get('pp_reader_prefs_v2')), {
    ...validPreferences(),
    fontSize: 32,
    contentWidth: 560,
  })

  const beforeInvalidSave = storage.values.get('pp_reader_prefs_v2')
  assert.equal(
    preferences.saveReaderPreferencesV2(
      { ...validPreferences(), mode: 'unknown' },
      storage,
    ),
    false,
  )
  assert.equal(storage.values.get('pp_reader_prefs_v2'), beforeInvalidSave)
})

test('Reset to Defaults returns and persists a fresh validated value', async () => {
  const preferences = await preferencesModule()
  const storage = memoryStorage({
    pp_reader_prefs_v2: JSON.stringify(
      validPreferences({ theme: 'warm', fontSize: 30 }),
    ),
  })

  const reset = preferences.resetReaderPreferencesV2(storage)
  assert.deepEqual(reset, preferences.READER_PREFERENCES_V2_DEFAULTS)
  assert.notEqual(reset, preferences.READER_PREFERENCES_V2_DEFAULTS)
  assert.deepEqual(
    JSON.parse(storage.values.get('pp_reader_prefs_v2')),
    preferences.READER_PREFERENCES_V2_DEFAULTS,
  )

  reset.fontSize = 29
  assert.equal(preferences.loadReaderPreferencesV2(storage).fontSize, 20)
})
