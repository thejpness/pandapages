import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function bootstrapModule() {
  return (
    await loadTypeScript(
      '../src/lib/reader-theme-bootstrap.ts',
      import.meta.url,
    )
  ).module
}

function fakeRoot(onSetProperty = () => {}) {
  const properties = new Map()
  return {
    properties,
    dataset: {},
    style: {
      setProperty(name, value) {
        onSetProperty(name, value)
        properties.set(name, value)
      },
      removeProperty(name) {
        properties.delete(name)
      },
    },
  }
}

function memoryStorage(value) {
  return {
    getItem(key) {
      return key === 'pp_reader_prefs_v2' ? value : null
    },
    setItem() {},
  }
}

test('Reader route detection is narrow and deterministic', async () => {
  const bootstrap = await bootstrapModule()
  assert.equal(bootstrap.isReaderRoute('/read/a-story'), true)
  assert.equal(bootstrap.isReaderRoute('/read/a-story/'), true)
  assert.equal(bootstrap.isReaderRoute('/read/'), false)
  assert.equal(bootstrap.isReaderRoute('/read/a-story/chapter'), false)
  assert.equal(bootstrap.isReaderRoute('/library'), false)
  assert.equal(bootstrap.isReaderRoute('/admin/stories'), false)
})

test('theme application projects semantic variables and stale values fall back to Paper', async () => {
  const bootstrap = await bootstrapModule()
  const root = fakeRoot()
  root.dataset.readerThemeBooting = 'true'

  assert.equal(bootstrap.applyReaderTheme('mist', root), 'mist')
  assert.equal(root.dataset.readerTheme, 'mist')
  assert.equal(root.dataset.readerRouteTheme, 'true')
  assert.equal(root.properties.get('--reader-background'), '#E7EDF0')
  assert.equal(root.properties.get('--reader-surface'), '#F4F7F9')
  assert.equal(root.properties.get('--reader-color-scheme'), 'light')
  assert.equal('readerThemeBooting' in root.dataset, false)

  assert.equal(bootstrap.applyReaderTheme('obsolete', root), 'paper')
  assert.equal(root.dataset.readerTheme, 'paper')
  assert.equal(root.properties.get('--reader-background'), '#F5F1E8')
})

test('clearing removes every projected Reader variable and route marker', async () => {
  const bootstrap = await bootstrapModule()
  const root = fakeRoot()
  bootstrap.applyReaderTheme('night', root)
  root.dataset.readerThemeBooting = 'true'
  assert.ok(root.properties.size > 0)

  bootstrap.clearReaderTheme(root)
  assert.equal(root.properties.size, 0)
  assert.equal('readerTheme' in root.dataset, false)
  assert.equal('readerRouteTheme' in root.dataset, false)
  assert.equal('readerThemeBooting' in root.dataset, false)
})

test('bootstrap restores a saved theme synchronously before Reader mount', async (t) => {
  const bootstrap = await bootstrapModule()
  const root = fakeRoot()
  root.dataset.readerThemeBooting = 'true'
  const previousDocument = globalThis.document
  const previousStorage = globalThis.localStorage
  const previousWindow = globalThis.window
  t.after(() => {
    if (previousDocument === undefined) delete globalThis.document
    else globalThis.document = previousDocument
    if (previousStorage === undefined) delete globalThis.localStorage
    else globalThis.localStorage = previousStorage
    if (previousWindow === undefined) delete globalThis.window
    else globalThis.window = previousWindow
  })

  globalThis.document = { documentElement: root }
  globalThis.localStorage = memoryStorage(
    JSON.stringify({
      schema: 2,
      mode: 'paged',
      theme: 'night',
      fontFamily: 'clear',
      fontSize: 28,
      lineHeight: 1.8,
      contentWidth: 820,
    }),
  )
  globalThis.window = { location: { pathname: '/read/a-story' } }

  assert.equal(bootstrap.bootstrapReaderTheme(), 'night')
  assert.equal(root.dataset.readerTheme, 'night')
  assert.equal(root.properties.get('--reader-background'), '#121417')
  assert.equal(root.properties.get('--reader-color-scheme'), 'dark')
  assert.equal('readerThemeBooting' in root.dataset, false)
})

test('storage read failure falls back to Paper and releases the boot marker', async (t) => {
  const bootstrap = await bootstrapModule()
  const root = fakeRoot()
  root.dataset.readerThemeBooting = 'true'
  const previousStorage = globalThis.localStorage
  t.after(() => {
    if (previousStorage === undefined) delete globalThis.localStorage
    else globalThis.localStorage = previousStorage
  })
  globalThis.localStorage = {
    getItem() {
      throw new Error('storage unavailable')
    },
    setItem() {},
  }

  assert.equal(bootstrap.bootstrapReaderTheme('/read/a-story', root), 'paper')
  assert.equal(root.dataset.readerTheme, 'paper')
  assert.equal(root.properties.get('--reader-background'), '#F5F1E8')
  assert.equal('readerThemeBooting' in root.dataset, false)
})

test('failed saved-theme application retries Paper without retaining partial values', async (t) => {
  const bootstrap = await bootstrapModule()
  let rejectedNight = false
  const root = fakeRoot((name, value) => {
    if (
      !rejectedNight &&
      name === '--reader-progress' &&
      value === '#8AB4F8'
    ) {
      rejectedNight = true
      throw new Error('test-only Night application failure')
    }
  })
  root.dataset.readerThemeBooting = 'true'
  const previousStorage = globalThis.localStorage
  t.after(() => {
    if (previousStorage === undefined) delete globalThis.localStorage
    else globalThis.localStorage = previousStorage
  })
  globalThis.localStorage = memoryStorage(
    JSON.stringify({
      schema: 2,
      mode: 'scroll',
      theme: 'night',
      fontFamily: 'book',
      fontSize: 20,
      lineHeight: 1.65,
      contentWidth: 720,
    }),
  )

  assert.equal(bootstrap.bootstrapReaderTheme('/read/a-story', root), 'paper')
  assert.equal(rejectedNight, true)
  assert.equal(root.dataset.readerTheme, 'paper')
  assert.equal(root.dataset.readerRouteTheme, 'true')
  assert.equal(root.properties.get('--reader-background'), '#F5F1E8')
  assert.equal(root.properties.get('--reader-progress'), '#8A5A00')
  assert.equal(root.properties.get('--reader-color-scheme'), 'light')
  assert.equal('readerThemeBooting' in root.dataset, false)
})

test('unrecoverable theme application releases the marker and remains fatal', async () => {
  const bootstrap = await bootstrapModule()
  const root = fakeRoot(() => {
    throw new Error('style application unavailable')
  })
  root.dataset.readerThemeBooting = 'true'

  assert.throws(
    () => bootstrap.bootstrapReaderTheme('/read/a-story', root),
    /style application unavailable/,
  )
  assert.equal('readerThemeBooting' in root.dataset, false)
  assert.equal('readerTheme' in root.dataset, false)
})

test('bootstrap does not theme non-Reader application routes', async (t) => {
  const bootstrap = await bootstrapModule()
  const root = fakeRoot()
  let storageReads = 0
  const previousDocument = globalThis.document
  const previousStorage = globalThis.localStorage
  t.after(() => {
    if (previousDocument === undefined) delete globalThis.document
    else globalThis.document = previousDocument
    if (previousStorage === undefined) delete globalThis.localStorage
    else globalThis.localStorage = previousStorage
  })
  globalThis.document = { documentElement: root }
  globalThis.localStorage = {
    getItem() {
      storageReads += 1
      return null
    },
    setItem() {},
  }

  assert.equal(bootstrap.bootstrapReaderTheme('/library'), null)
  assert.equal(storageReads, 0)
  assert.equal(root.properties.size, 0)
  assert.deepEqual(root.dataset, {})
})
