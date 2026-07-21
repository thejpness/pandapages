import assert from 'node:assert/strict'
import test from 'node:test'
import { transformWithOxc } from 'vite'
import { loadTypeScript } from './helpers/typescript-module.mjs'

function moduleURL(source) {
  return (
    'data:text/javascript;base64,' +
    Buffer.from(source).toString('base64') +
    '#' +
    Date.now() +
    Math.random()
  )
}

async function compiledModuleURL(relativePath) {
  const sourceURL = new URL(relativePath, import.meta.url)
  const source = await (await import('node:fs/promises')).readFile(sourceURL, 'utf8')
  const transformed = await transformWithOxc(source, sourceURL.pathname)
  return moduleURL(transformed.code)
}

function position(ordinal, offset, percent) {
  return {
    locator: {
      schema: 2,
      segment: {
        key: String(ordinal).padStart(64, '0'),
        occurrence: 1,
        ordinal,
        offset,
      },
    },
    percent,
  }
}

function fakeTimers() {
  let nextID = 1
  const callbacks = new Map()
  return {
    setTimeout(callback) {
      const id = nextID
      nextID += 1
      callbacks.set(id, callback)
      return id
    },
    clearTimeout(id) {
      callbacks.delete(id)
    },
    runAll() {
      const pending = [...callbacks.values()]
      callbacks.clear()
      for (const callback of pending) callback()
    },
    count() {
      return callbacks.size
    },
  }
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

async function readerProgressHarness(t) {
  const harnessKey =
    '__pandaReaderProgressLifecycle_' +
    Date.now() +
    Math.random().toString(16).slice(2)
  const timers = fakeTimers()
  const originalWindow = globalThis.window
  globalThis.window = {
    setTimeout: timers.setTimeout,
    clearTimeout: timers.clearTimeout,
  }

  const state = {
    baseline: null,
    captures: 0,
    navigations: 0,
    current: position(1, 0, 0),
    writes: [],
  }
  globalThis[harnessKey] = state

  const vueURL = moduleURL(`
    export const ref = (value) => ({ value })
    export const shallowRef = ref
    export const computed = (read) => ({
      get value() {
        return read()
      },
    })
  `)
  const apiURL = moduleURL(`
    const state = globalThis[${JSON.stringify(harnessKey)}]
    export const getAPIErrorStatus = (error) => error?.status ?? null
    export const getProgress = async () => ({ progress: state.baseline })
    export const saveProgress = async (
      slug,
      version,
      locator,
      percent,
      options,
    ) => {
      state.writes.push({ slug, version, locator, percent, options })
    }
  `)
  const mappingURL = moduleURL(`
    export const mapReaderProgressAcrossVersions = () => ({
      kind: 'none',
      confidence: 'none',
    })
  `)
  const scrollURL = moduleURL(`
    export const findReaderResumeSegment = () => null
  `)
  const policyURL = moduleURL(`
    export const readerLifecyclePersistenceAllowed = (state) =>
      state.baselineStatus === 'ready' &&
      !state.sessionLoss &&
      !state.decisionPending &&
      !state.awaitingIntent
    export const readerLibraryPersistenceStrategy = () => 'drain'
    export const readerProgressPresentation = () => ({
      text: '',
      retryKind: null,
      retryDisabled: false,
    })
  `)
  const baselineURL = moduleURL(`
    export function createProgressBaselineController(options) {
      let disposed = false
      let listener = () => {}
      let state = {
        status: 'loading',
        value: null,
        error: null,
        attempt: 0,
      }
      return {
        subscribe(next) {
          listener = next
          next(state)
          return () => {
            listener = () => {}
          }
        },
        async load() {
          const value = await options.load()
          state = {
            status: 'ready',
            value,
            error: null,
            attempt: 1,
          }
          if (!disposed) listener(state)
          return state
        },
        retry() {
          return this.load()
        },
        dispose() {
          disposed = true
        },
      }
    }
  `)
  const coordinatorURL = await compiledModuleURL(
    '../src/lib/progress-save-coordinator.ts',
  )

  const replacements = new Map([
    ["'vue'", JSON.stringify(vueURL)],
    ["'../lib/api'", JSON.stringify(apiURL)],
    [
      "'../lib/reader-cross-version-progress'",
      JSON.stringify(mappingURL),
    ],
    ["'../lib/reader-scroll-location'", JSON.stringify(scrollURL)],
    [
      "'../lib/progress-baseline-controller'",
      JSON.stringify(baselineURL),
    ],
    ["'../lib/reader-progress-policy'", JSON.stringify(policyURL)],
    [
      "'../lib/progress-save-coordinator'",
      JSON.stringify(coordinatorURL),
    ],
  ])

  const { module } = await loadTypeScript(
    '../src/composables/useReaderProgress.ts',
    import.meta.url,
    (source) => {
      let replaced = source
      for (const [dependency, replacement] of replacements) {
        replaced = replaced.replaceAll(dependency, replacement)
      }
      return replaced
    },
  )

  const progress = module.useReaderProgress({
    capture() {
      state.captures += 1
      return state.current
    },
    onSessionLoss() {},
    async navigateToLibrary() {
      state.navigations += 1
    },
    onNavigationError() {},
  })

  progress.prepare('test-story')
  progress.begin('test-story', 1, [])
  await settle()
  assert.equal(progress.baselineState.value.status, 'ready')
  state.captures = 0

  t.after(() => {
    progress.dispose()
    delete globalThis[harnessKey]
    if (originalWindow === undefined) delete globalThis.window
    else globalThis.window = originalWindow
  })

  return { progress, state, timers }
}

for (const lifecycle of [
  'pagehide',
  'visibility-hidden',
  'unmount',
]) {
  test(`${lifecycle} during capture suppression neither captures nor persists an intermediate position`, async (t) => {
    const { progress, state, timers } = await readerProgressHarness(t)
    const release = progress.suppressCapture()
    state.current = position(7, 0.63, 0.72)

    progress.pageHide()
    if (lifecycle === 'unmount') progress.dispose()
    timers.runAll()
    await settle()

    assert.equal(state.captures, 0)
    assert.deepEqual(state.writes, [])
    assert.equal(timers.count(), 0)
    release()
  })
}

test('suppressed lifecycle flush drains only a coordinator-owned snapshot', async (t) => {
  const { progress, state, timers } = await readerProgressHarness(t)
  const owned = position(4, 0.35, 0.48)
  progress.movement(owned)
  const release = progress.suppressCapture()
  state.captures = 0
  state.current = position(9, 0.81, 0.92)

  progress.pageHide()
  timers.runAll()
  await settle()

  assert.equal(state.captures, 0)
  assert.equal(state.writes.length, 1)
  assert.deepEqual(state.writes[0].locator, owned.locator)
  assert.equal(state.writes[0].percent, owned.percent)
  assert.deepEqual(state.writes[0].options, { keepalive: true })
  release()
})

test('suppressed Library navigation neither captures nor persists an intermediate position', async (t) => {
  const { progress, state, timers } = await readerProgressHarness(t)
  const release = progress.suppressCapture()
  state.current = position(8, 0.72, 0.84)

  await progress.goLibrary()
  timers.runAll()
  await settle()

  assert.equal(state.captures, 0)
  assert.deepEqual(state.writes, [])
  assert.equal(state.navigations, 1)
  assert.equal(timers.count(), 0)
  release()
})

test('suppressed Library navigation drains only a coordinator-owned snapshot', async (t) => {
  const { progress, state, timers } = await readerProgressHarness(t)
  const owned = position(4, 0.35, 0.48)
  progress.movement(owned)
  const release = progress.suppressCapture()
  state.captures = 0
  state.current = position(9, 0.81, 0.92)

  await progress.goLibrary()
  timers.runAll()
  await settle()

  assert.equal(state.captures, 0)
  assert.equal(state.writes.length, 1)
  assert.deepEqual(state.writes[0].locator, owned.locator)
  assert.equal(state.writes[0].percent, owned.percent)
  assert.equal(state.writes[0].options, undefined)
  assert.equal(state.navigations, 1)
  assert.equal(timers.count(), 0)
  release()
})

test('ordinary Library navigation captures, persists, and then navigates', async (t) => {
  const { progress, state, timers } = await readerProgressHarness(t)
  const current = position(6, 0.52, 0.65)
  state.current = current

  await progress.goLibrary()
  timers.runAll()
  await settle()

  assert.equal(state.captures, 1)
  assert.equal(state.writes.length, 1)
  assert.deepEqual(state.writes[0].locator, current.locator)
  assert.equal(state.writes[0].percent, current.percent)
  assert.equal(state.writes[0].options, undefined)
  assert.equal(state.navigations, 1)
  assert.equal(timers.count(), 0)
})

test('ordinary pagehide captures and persists the current position with keepalive', async (t) => {
  const { progress, state, timers } = await readerProgressHarness(t)
  const current = position(5, 0.44, 0.57)
  state.current = current

  progress.pageHide()
  timers.runAll()
  await settle()

  assert.equal(state.captures, 1)
  assert.equal(state.writes.length, 1)
  assert.deepEqual(state.writes[0].locator, current.locator)
  assert.equal(state.writes[0].percent, current.percent)
  assert.deepEqual(state.writes[0].options, { keepalive: true })
})
