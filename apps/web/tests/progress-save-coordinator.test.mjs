import assert from 'node:assert/strict'
import test from 'node:test'
import { loadTypeScript as loadModule } from './helpers/typescript-module.mjs'

const loadTypeScript = (relativePath, transform) =>
  loadModule(relativePath, import.meta.url, transform)

function deferred() {
  let resolve
  let reject
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function fakeTimers() {
  let nextID = 1
  const callbacks = new Map()
  return {
    setTimer(callback) {
      const id = nextID
      nextID += 1
      callbacks.set(id, callback)
      return id
    },
    clearTimer(id) {
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

function scrollSnapshot(scrollY, percent = scrollY / 1000, overrides = {}) {
  return {
    slug: 'test-story',
    version: 1,
    locator: {
      schema: 2,
      segment: {
        key: 'a'.repeat(64),
        occurrence: 1,
        ordinal: 1,
        offset: Math.max(0, Math.min(1, scrollY / 10000)),
      },
    },
    percent,
    ...overrides,
  }
}

function pagedSnapshot(page, percent = page / 10) {
  return {
    slug: 'test-story',
    version: 1,
    locator: {
      schema: 2,
      segment: {
        key: 'b'.repeat(64),
        occurrence: 1,
        ordinal: page + 1,
        offset: 0,
      },
    },
    percent,
  }
}

function controlledPersistence() {
  const calls = []
  return {
    calls,
    persist(snapshot, options) {
      const gate = deferred()
      calls.push({ snapshot, options, ...gate })
      return gate.promise
    },
  }
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
}

async function coordinatorHarness() {
  const { module } = await loadTypeScript(
    '../src/lib/progress-save-coordinator.ts'
  )
  const timers = fakeTimers()
  const persistence = controlledPersistence()
  const coordinator = module.createProgressSaveCoordinator({
    persist: persistence.persist,
    debounceMs: 450,
    setTimer: timers.setTimer,
    clearTimer: timers.clearTimer,
  })
  return { module, timers, persistence, coordinator }
}

test('successful persistence becomes saved only after server confirmation', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100, 0.1), { debounce: false })

  const flushed = coordinator.flush()
  assert.equal(coordinator.current().status, 'saving')
  assert.equal(persistence.calls.length, 1)
  assert.equal(coordinator.current().status === 'saved', false)

  persistence.calls[0].resolve()
  await flushed
  assert.equal(coordinator.current().status, 'saved')
  assert.deepEqual(coordinator.current().confirmed, scrollSnapshot(100, 0.1))
})

test('rejected persistence retains the latest desired snapshot in error', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  const confirmed = scrollSnapshot(0, 0)
  const desired = scrollSnapshot(120, 0.12)
  coordinator.initialize(confirmed)
  coordinator.update(desired, { debounce: false })

  const flushed = coordinator.flush()
  persistence.calls[0].reject(new Error('network unavailable'))
  await assert.rejects(flushed, /network unavailable/)

  const state = coordinator.current()
  assert.equal(state.status, 'error')
  assert.deepEqual(state.desired, desired)
  assert.deepEqual(state.confirmed, confirmed)
})

test('failed persistence is never emitted as saved', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  const statuses = []
  coordinator.subscribe((state) => statuses.push(state.status))
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100, 0.1), { debounce: false })

  const flushed = coordinator.flush()
  persistence.calls[0].reject(new Error('failed'))
  await assert.rejects(flushed)

  assert.deepEqual(statuses.slice(-2), ['saving', 'error'])
  assert.equal(statuses.includes('saved'), false)
})

test('only one persistence request can be in flight', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const firstFlush = coordinator.flush()
  coordinator.update(scrollSnapshot(200), { debounce: false })
  const secondFlush = coordinator.flush()

  assert.equal(persistence.calls.length, 1)
  persistence.calls[0].resolve()
  await settle()
  assert.equal(persistence.calls.length, 2)
  persistence.calls[1].resolve()
  await Promise.all([firstFlush, secondFlush])
})

test('updates while saving coalesce to the latest snapshot', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const flushed = coordinator.flush()
  coordinator.update(scrollSnapshot(150))
  coordinator.update(scrollSnapshot(240))
  coordinator.update(scrollSnapshot(300))

  assert.equal(persistence.calls.length, 1)
  persistence.calls[0].resolve()
  await settle()
  assert.equal(persistence.calls.length, 2)
  assert.deepEqual(persistence.calls[1].snapshot, scrollSnapshot(300))
  persistence.calls[1].resolve()
  await flushed
})

test('the newest queued snapshot is sent immediately after success', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const flushed = coordinator.flush()
  coordinator.update(scrollSnapshot(400))

  persistence.calls[0].resolve()
  await settle()
  assert.equal(coordinator.current().status, 'saving')
  assert.deepEqual(persistence.calls[1].snapshot, scrollSnapshot(400))
  persistence.calls[1].resolve()
  await flushed
})

test('an older completion cannot establish a newer snapshot as persisted', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const flushed = coordinator.flush()
  coordinator.update(scrollSnapshot(500))

  persistence.calls[0].resolve()
  await settle()
  assert.equal(coordinator.current().status, 'saving')
  assert.deepEqual(coordinator.current().confirmed, scrollSnapshot(100))
  assert.deepEqual(coordinator.current().desired, scrollSnapshot(500))

  persistence.calls[1].resolve()
  await flushed
  assert.deepEqual(coordinator.current().confirmed, scrollSnapshot(500))
})

test('retry sends the latest snapshot rather than the originally failed one', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const first = coordinator.flush()
  persistence.calls[0].reject(new Error('offline'))
  await assert.rejects(first)

  coordinator.update(scrollSnapshot(350))
  const retried = coordinator.retry()
  assert.deepEqual(persistence.calls[1].snapshot, scrollSnapshot(350))
  persistence.calls[1].resolve()
  await retried
})

test('repeated retry does not create concurrent writes', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const first = coordinator.flush()
  persistence.calls[0].reject(new Error('offline'))
  await assert.rejects(first)

  const retryOne = coordinator.retry()
  const retryTwo = coordinator.retry()
  assert.equal(persistence.calls.length, 2)
  persistence.calls[1].resolve()
  await Promise.all([retryOne, retryTwo])
})

test('movement after failure updates pending data without hiding the error', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const first = coordinator.flush()
  persistence.calls[0].reject(new Error('offline'))
  await assert.rejects(first)

  coordinator.update(scrollSnapshot(275))
  assert.equal(coordinator.current().status, 'error')
  assert.deepEqual(coordinator.current().desired, scrollSnapshot(275))
  assert.equal(persistence.calls.length, 1)
})

test('confirmed scroll and paged movement below thresholds is suppressed', async () => {
  const { coordinator, persistence, timers } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(100, 0.2))
  coordinator.update(scrollSnapshot(123, 0.209))
  timers.runAll()
  assert.equal(persistence.calls.length, 0)
  assert.equal(coordinator.current().status, 'idle')

  coordinator.initialize(pagedSnapshot(2, 0.2))
  coordinator.update(pagedSnapshot(2, 0.209))
  timers.runAll()
  assert.equal(persistence.calls.length, 0)
})

test('threshold suppression does not discard failed dirty data', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const first = coordinator.flush()
  persistence.calls[0].reject(new Error('offline'))
  await assert.rejects(first)

  coordinator.update(scrollSnapshot(1, 0.001))
  const retry = coordinator.retry()
  assert.deepEqual(persistence.calls[1].snapshot, scrollSnapshot(1, 0.001))
  persistence.calls[1].resolve()
  await retry
})

test('forced Start Over persists a zero snapshot through the coordinator', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(500, 0.5))
  coordinator.update(scrollSnapshot(0, 0), {
    force: true,
    debounce: false,
  })
  const flushed = coordinator.flush()

  assert.deepEqual(persistence.calls[0].snapshot, scrollSnapshot(0, 0))
  persistence.calls[0].resolve()
  await flushed
  assert.equal(coordinator.current().status, 'saved')
})

test('saveProgress rejects malformed success and preserves keepalive credentials', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  const { module: api } = await loadTypeScript(
    '../src/lib/api.ts',
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''")
  )

  let captured
  globalThis.fetch = async (url, init) => {
    captured = { url, init }
    return new Response(JSON.stringify({ ok: false }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  await assert.rejects(
    api.saveProgress('story', 1, scrollSnapshot(10).locator, 0.1, {
      keepalive: true,
    }),
    /Invalid progress-save response/
  )
  assert.equal(captured.url, '/api/v1/progress/story')
  assert.equal(captured.init.credentials, 'include')
  assert.equal(captured.init.keepalive, true)

  globalThis.fetch = async () =>
    new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  await api.saveProgress(
    'story',
    1,
    scrollSnapshot(10).locator,
    0.1
  )
})

test('saveProgress propagates transport and server failures', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  const { module: api } = await loadTypeScript(
    '../src/lib/api.ts',
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''")
  )

  globalThis.fetch = async () => {
    throw new TypeError('network unavailable')
  }
  await assert.rejects(
    api.saveProgress('story', 1, scrollSnapshot(10).locator, 0.1),
    /network unavailable/
  )

  globalThis.fetch = async () =>
    new Response(
      JSON.stringify({
        error: { code: 'db', message: 'progress update failed' },
      }),
      {
        status: 500,
        headers: { 'Content-Type': 'application/json' },
      }
    )
  await assert.rejects(
    api.saveProgress('story', 1, scrollSnapshot(10).locator, 0.1),
    (error) => error.status === 500
  )
})

test('transport and server errors remain retryable coordinator failures', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const first = coordinator.flush()
  const unavailable = Object.assign(new Error('unavailable'), { status: 503 })
  persistence.calls[0].reject(unavailable)
  await assert.rejects(first, (error) => error.status === 503)
  assert.equal(coordinator.current().status, 'error')

  const retry = coordinator.retry()
  persistence.calls[1].resolve()
  await retry
  assert.equal(coordinator.current().status, 'saved')
})

test('401 progress failure remains visible to the signed-session owner', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const flush = coordinator.flush()
  const ended = Object.assign(new Error('session ended'), { status: 401 })
  persistence.calls[0].reject(ended)

  await assert.rejects(flush, (error) => error.status === 401)
  assert.equal(coordinator.current().status, 'error')
  assert.equal(coordinator.current().error.status, 401)
})

test('explicit Library navigation can await a pending coordinator drain', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  let navigated = false
  const navigation = (async () => {
    await coordinator.flush()
    navigated = true
  })()

  assert.equal(navigated, false)
  persistence.calls[0].resolve()
  await navigation
  assert.equal(navigated, true)
})

test('failed Library drain remains retryable before navigation', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  let navigated = false
  let leaveGate = false
  const navigation = (async () => {
    try {
      await coordinator.flush()
      navigated = true
    } catch {
      leaveGate = true
    }
  })()

  persistence.calls[0].reject(new Error('offline'))
  await navigation
  assert.equal(navigated, false)
  assert.equal(leaveGate, true)
  assert.equal(coordinator.current().status, 'error')
})

test('page-hide keepalive never starts a parallel request', async () => {
  const { coordinator, persistence } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100), { debounce: false })
  const first = coordinator.flush()
  coordinator.update(scrollSnapshot(300), { debounce: false })
  const hiddenFlush = coordinator.bestEffortKeepaliveFlush()

  assert.equal(persistence.calls.length, 1)
  persistence.calls[0].resolve()
  await settle()
  assert.equal(persistence.calls.length, 2)
  assert.deepEqual(persistence.calls[1].options, { keepalive: true })
  persistence.calls[1].resolve()
  await Promise.all([first, hiddenFlush])
})

test('dispose clears timers and ignores later persistence completion', async () => {
  const { coordinator, persistence, timers } = await coordinatorHarness()
  coordinator.initialize(scrollSnapshot(0, 0))
  coordinator.update(scrollSnapshot(100))
  assert.equal(timers.count(), 1)
  coordinator.dispose()
  assert.equal(timers.count(), 0)
  timers.runAll()
  assert.equal(persistence.calls.length, 0)

  const second = await coordinatorHarness()
  second.coordinator.initialize(scrollSnapshot(0, 0))
  second.coordinator.update(scrollSnapshot(100), { debounce: false })
  const flushed = second.coordinator.flush()
  const beforeDispose = second.coordinator.current()
  second.coordinator.dispose()
  await assert.rejects(flushed, /disposed/)
  second.persistence.calls[0].resolve()
  await settle()
  assert.deepEqual(second.coordinator.current(), beforeDispose)
})
