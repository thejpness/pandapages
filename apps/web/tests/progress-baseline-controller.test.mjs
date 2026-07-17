import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { transformWithOxc } from 'vite'

async function loadTypeScript(relativePath) {
  const sourceURL = new URL(relativePath, import.meta.url)
  const source = await readFile(sourceURL, 'utf8')
  const transformed = await transformWithOxc(source, sourceURL.pathname)
  const moduleURL =
    'data:text/javascript;base64,' +
    Buffer.from(transformed.code).toString('base64') +
    '#' +
    Date.now() +
    Math.random()

  return { module: await import(moduleURL), source }
}

function deferred() {
  let resolve
  let reject
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

async function baselineController(load) {
  const { module } = await loadTypeScript(
    '../src/lib/progress-baseline-controller.ts'
  )
  return module.createProgressBaselineController({ load })
}

async function recoveryModules() {
  const [baseline, coordinator] = await Promise.all([
    loadTypeScript('../src/lib/progress-baseline-controller.ts'),
    loadTypeScript('../src/lib/progress-save-coordinator.ts'),
  ])
  return {
    baseline: baseline.module,
    coordinator: coordinator.module,
  }
}

function scrollSnapshot(scrollY, percent = scrollY / 1000) {
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

function createCoordinator(module, persistence = controlledPersistence()) {
  let nextTimer = 1
  const timers = new Map()
  return {
    persistence,
    coordinator: module.createProgressSaveCoordinator({
      persist: persistence.persist,
      debounceMs: 450,
      setTimer(callback) {
        const id = nextTimer
        nextTimer += 1
        timers.set(id, callback)
        return id
      },
      clearTimer(id) {
        timers.delete(id)
      },
    }),
  }
}

function applyRecoveryPlan(module, coordinator, { confirmed, current }) {
  const plan = module.planProgressBaselineCoordinatorRecovery({
    confirmed,
    current,
    retriedAfterUnavailableMovement: true,
  })
  coordinator.initialize(plan.initialConfirmed, plan.initialDesired)
  if (plan.updateDesired) {
    coordinator.update(plan.updateDesired, {
      force: plan.forceUpdate,
      debounce: false,
    })
  }
  return plan
}

async function failedReaderHarness(error = new TypeError('network unavailable')) {
  const writes = []
  let coordinator = null
  let navigations = 0
  let visiblePosition = 0.5
  let saveStatus = ''
  const controller = await baselineController(async () => {
    throw error
  })

  controller.subscribe((state) => {
    if (state.status === 'ready') {
      coordinator = {
        write(kind) {
          writes.push(kind)
          saveStatus = 'Saved'
        },
      }
    }
  })
  await controller.load()

  return {
    controller,
    writes,
    get navigations() {
      return navigations
    },
    get visiblePosition() {
      return visiblePosition
    },
    get saveStatus() {
      return saveStatus
    },
    event(kind) {
      coordinator?.write(kind)
    },
    startOver() {
      visiblePosition = 0
      coordinator?.write('start-over')
    },
    library() {
      if (coordinator) coordinator.write('library')
      navigations += 1
    },
  }
}

test('initial progress GET network failure leaves the baseline unavailable', async () => {
  const error = new TypeError('network unavailable')
  const controller = await baselineController(async () => {
    throw error
  })

  const state = await controller.load()
  assert.equal(state.status, 'unavailable')
  assert.equal(state.error, error)
})

test('initial progress GET 500 leaves the baseline unavailable', async () => {
  const error = Object.assign(new Error('progress query failed'), { status: 500 })
  const controller = await baselineController(async () => {
    throw error
  })

  const state = await controller.load()
  assert.equal(state.status, 'unavailable')
  assert.equal(state.error.status, 500)
})

test('a failed load does not initialize a writable coordinator', async () => {
  let coordinatorCreations = 0
  const controller = await baselineController(async () => {
    throw new TypeError('offline')
  })
  controller.subscribe((state) => {
    if (state.status === 'ready') coordinatorCreations += 1
  })

  await controller.load()
  assert.equal(coordinatorCreations, 0)
})

for (const event of [
  'scroll',
  'paged movement',
  'reader-mode change',
  'page-hide',
  'visibility change',
  'unmount',
  'route-slug change',
]) {
  test(`${event} while unavailable issues no progress PUT`, async () => {
    const reader = await failedReaderHarness()
    reader.event(event)
    assert.deepEqual(reader.writes, [])
  })
}

test('Library navigation while unavailable proceeds without a progress PUT', async () => {
  const reader = await failedReaderHarness()
  reader.library()
  assert.equal(reader.navigations, 1)
  assert.deepEqual(reader.writes, [])
})

test('Start Over while unavailable moves visibly without PUT or Saved', async () => {
  const reader = await failedReaderHarness()
  reader.startOver()
  assert.equal(reader.visiblePosition, 0)
  assert.deepEqual(reader.writes, [])
  assert.equal(reader.saveStatus, '')
  assert.equal(reader.controller.current().status, 'unavailable')
})

test('Retry is exposed as a second load after failure', async () => {
  let calls = 0
  const controller = await baselineController(async () => {
    calls += 1
    throw new TypeError('offline')
  })

  await controller.load()
  const retryState = await controller.retry()
  assert.equal(calls, 2)
  assert.equal(retryState.attempt, 2)
})

test('repeated Retry actions share one active progress GET', async () => {
  const retry = deferred()
  let calls = 0
  const controller = await baselineController(() => {
    calls += 1
    if (calls === 1) return Promise.reject(new TypeError('offline'))
    return retry.promise
  })

  await controller.load()
  const first = controller.retry()
  const second = controller.retry()
  assert.equal(first, second)
  await Promise.resolve()
  assert.equal(calls, 2)
  retry.resolve(null)
  await Promise.all([first, second])
})

test('a failed Retry remains unavailable and cannot enable PUT', async () => {
  const reader = await failedReaderHarness()
  const state = await reader.controller.retry()
  reader.event('scroll')
  assert.equal(state.status, 'unavailable')
  assert.deepEqual(reader.writes, [])
})

test('Retry 401 invokes the signed-session-loss transition', async () => {
  const locked = []
  const error = Object.assign(new Error('session ended'), { status: 401 })
  const controller = await baselineController(async () => {
    throw error
  })
  controller.subscribe((state) => {
    if (state.status === 'unavailable' && state.error?.status === 401) {
      locked.push('/unlock?next=/read/test-story')
    }
  })

  await controller.load()
  assert.deepEqual(locked, ['/unlock?next=/read/test-story'])
})

test('successful Retry changes the baseline to ready', async () => {
  let calls = 0
  const progress = {
    version: 1,
    locator: scrollSnapshot(320).locator,
    percent: 0.32,
  }
  const controller = await baselineController(async () => {
    calls += 1
    if (calls === 1) throw new TypeError('offline')
    return progress
  })

  await controller.load()
  const state = await controller.retry()
  assert.equal(state.status, 'ready')
  assert.deepEqual(state.value, progress)
})

test('successful Retry initializes the writable coordinator exactly once', async () => {
  let calls = 0
  let coordinatorCreations = 0
  const controller = await baselineController(async () => {
    calls += 1
    if (calls === 1) throw new TypeError('offline')
    return null
  })
  controller.subscribe((state) => {
    if (state.status === 'ready') coordinatorCreations += 1
  })

  await controller.load()
  await Promise.all([controller.retry(), controller.retry()])
  assert.equal(coordinatorCreations, 1)
})

test('existing server progress is not overwritten before baseline recovery', async () => {
  const retry = deferred()
  const writes = []
  let calls = 0
  let coordinator = null
  const controller = await baselineController(() => {
    calls += 1
    if (calls === 1) return Promise.reject(new TypeError('offline'))
    return retry.promise
  })
  controller.subscribe((state) => {
    if (state.status === 'ready') {
      coordinator = { update: (value) => writes.push(value) }
    }
  })

  await controller.load()
  coordinator?.update({ percent: 0.8 })
  const recovery = controller.retry()
  coordinator?.update({ percent: 0.9 })
  assert.deepEqual(writes, [])

  retry.resolve({
    version: 1,
    locator: scrollSnapshot(700).locator,
    percent: 0.7,
  })
  await recovery
  coordinator.update({ percent: 0.9 })
  assert.deepEqual(writes, [{ percent: 0.9 }])
})

test('retry recovery after unavailable movement does not confirm an older-version locator before PUT succeeds', async () => {
  const { baseline, coordinator: coordinatorModule } = await recoveryModules()
  const { coordinator, persistence } = createCoordinator(coordinatorModule)
  const current = scrollSnapshot(860, 0.86)
  const olderVersionProgress = {
    version: 2,
    locator: scrollSnapshot(220).locator,
    percent: 0.22,
  }
  const confirmed =
    olderVersionProgress.version === current.version
      ? {
          ...scrollSnapshot(220, olderVersionProgress.percent),
          locator: olderVersionProgress.locator,
        }
      : null

  const plan = applyRecoveryPlan(baseline, coordinator, { confirmed, current })

  assert.equal(confirmed, null)
  assert.equal(plan.initialConfirmed, null)
  assert.equal(plan.initialDesired, null)
  assert.deepEqual(plan.updateDesired, current)
  assert.equal(plan.forceUpdate, true)
  assert.equal(persistence.calls.length, 0)

  const dirty = coordinator.current()
  assert.equal(dirty.status, 'dirty')
  assert.deepEqual(dirty.confirmed, null)
  assert.deepEqual(dirty.desired, current)

  const flushed = coordinator.flush()
  assert.equal(coordinator.current().status, 'saving')
  assert.equal(persistence.calls.length, 1)
  assert.deepEqual(persistence.calls[0].snapshot, current)
  assert.equal(persistence.calls[0].snapshot.version, current.version)
  assert.notEqual(coordinator.current().status, 'saved')

  const duplicateFlush = coordinator.flush()
  assert.equal(persistence.calls.length, 1)
  persistence.calls[0].resolve()
  await Promise.all([flushed, duplicateFlush])

  const saved = coordinator.current()
  assert.equal(saved.status, 'saved')
  assert.deepEqual(saved.confirmed, current)
  assert.deepEqual(saved.desired, current)
})

test('failed recovery PUT after older-version retry remains a truthful save error', async () => {
  const { baseline, coordinator: coordinatorModule } = await recoveryModules()
  const { coordinator, persistence } = createCoordinator(coordinatorModule)
  const current = scrollSnapshot(910, 0.91)

  applyRecoveryPlan(baseline, coordinator, { confirmed: null, current })
  const flushed = coordinator.flush()
  const saveError = Object.assign(new Error('progress update failed'), {
    status: 503,
  })
  persistence.calls[0].reject(saveError)
  await assert.rejects(flushed, (error) => error === saveError)

  const state = coordinator.current()
  assert.equal(state.status, 'error')
  assert.deepEqual(state.confirmed, null)
  assert.deepEqual(state.desired, current)
  assert.equal(state.error, saveError)
  assert.equal(persistence.calls.length, 1)
})

test('retry movement remains desired when a current-version baseline is confirmed', async () => {
  const { baseline, coordinator: coordinatorModule } = await recoveryModules()
  const { coordinator, persistence } = createCoordinator(coordinatorModule)
  const confirmed = scrollSnapshot(240, 0.24)
  const current = scrollSnapshot(760, 0.76)

  applyRecoveryPlan(baseline, coordinator, { confirmed, current })
  const dirty = coordinator.current()
  assert.equal(dirty.status, 'dirty')
  assert.deepEqual(dirty.confirmed, confirmed)
  assert.deepEqual(dirty.desired, current)

  const flushed = coordinator.flush()
  assert.equal(persistence.calls.length, 1)
  assert.deepEqual(persistence.calls[0].snapshot, current)
  persistence.calls[0].resolve()
  await flushed
  assert.equal(coordinator.current().status, 'saved')
})

test('successful empty progress is a known baseline that enables first save', async () => {
  const empty = null
  const writes = []
  let coordinator = null
  const controller = await baselineController(async () => empty)
  controller.subscribe((state) => {
    if (state.status === 'ready') {
      coordinator = { update: (value) => writes.push(value) }
    }
  })

  const state = await controller.load()
  coordinator.update({ percent: 0.1 })
  assert.equal(state.status, 'ready')
  assert.deepEqual(state.value, empty)
  assert.deepEqual(writes, [{ percent: 0.1 }])
})

test('successful version-mismatch progress is known, not unavailable', async () => {
  const mismatch = {
    version: 2,
    locator: scrollSnapshot(200).locator,
    percent: 0.2,
  }
  const controller = await baselineController(async () => mismatch)
  const state = await controller.load()

  assert.equal(state.status, 'ready')
  assert.deepEqual(state.value, mismatch)
})

test('disposed pending loads cannot later expose a writable baseline', async () => {
  const pending = deferred()
  const statuses = []
  const controller = await baselineController(() => pending.promise)
  controller.subscribe((state) => statuses.push(state.status))
  const load = controller.load()
  controller.dispose()
  pending.resolve(null)
  await load

  assert.equal(statuses.includes('ready'), false)
})

test('Reader exposes an accessible unavailable and checking status with Retry', async () => {
  const reader = await readFile(
    new URL('../src/views/Reader.vue', import.meta.url),
    'utf8'
  )
  assert.match(reader, /return 'Progress unavailable'/)
  assert.match(reader, /return 'Checking progress…'/)
  assert.match(reader, /role="status" aria-live="polite" aria-atomic="true"/)
  assert.match(reader, /@click="retryProgressBaseline"/)
})

test('Reader gates lifecycle persistence and save-failure navigation UI on readiness', async () => {
  const reader = await readFile(
    new URL('../src/views/Reader.vue', import.meta.url),
    'utf8'
  )
  const pageHide = reader.slice(
    reader.indexOf('function onPageHide()'),
    reader.indexOf('function onVisibilityChange()')
  )
  const library = reader.slice(
    reader.indexOf('async function goLibrary()'),
    reader.indexOf('function leaveReaderAnyway()')
  )
  assert.match(pageHide, /progressBaselineState\.value\.status !== 'ready'/)
  assert.match(library, /progressBaselineState\.value\.status !== 'ready'/)
  assert.match(
    reader,
    /v-if="progressBaselineState\.status === 'ready' && leaveAfterSaveFailure"/
  )
  assert.match(reader, /disposeProgressBaselineController\(\)/)
})
