import type { JsonObject } from './api'

export type ProgressSaveStatus = 'idle' | 'dirty' | 'saving' | 'saved' | 'error'

export type ProgressSnapshot = {
  slug: string
  version: number
  locator: JsonObject
  percent: number
}

export type ProgressPersistenceOptions = {
  keepalive?: boolean
}

export type ProgressSaveState = {
  status: ProgressSaveStatus
  desired: ProgressSnapshot | null
  confirmed: ProgressSnapshot | null
  error: unknown
}

export type ProgressSaveCoordinator = {
  initialize: (
    confirmed: ProgressSnapshot | null,
    desired?: ProgressSnapshot | null
  ) => void
  update: (
    snapshot: ProgressSnapshot,
    options?: { force?: boolean; debounce?: boolean }
  ) => void
  flush: (options?: ProgressPersistenceOptions) => Promise<void>
  retry: () => Promise<void>
  bestEffortKeepaliveFlush: () => Promise<void>
  current: () => ProgressSaveState
  subscribe: (listener: (state: ProgressSaveState) => void) => () => void
  dispose: () => void
}

type TimerHandle = number

export type ProgressSaveCoordinatorOptions = {
  persist: (
    snapshot: ProgressSnapshot,
    options?: ProgressPersistenceOptions
  ) => Promise<void>
  debounceMs?: number
  setTimer: (callback: () => void, delayMs: number) => TimerHandle
  clearTimer: (handle: TimerHandle) => void
}

type DrainWaiter = {
  resolve: () => void
  reject: (error: unknown) => void
}

const scrollTolerancePx = 24
const percentTolerance = 0.01

function cloneSnapshot(snapshot: ProgressSnapshot | null): ProgressSnapshot | null {
  if (!snapshot) return null
  return {
    slug: snapshot.slug,
    version: snapshot.version,
    locator: JSON.parse(JSON.stringify(snapshot.locator)) as JsonObject,
    percent: snapshot.percent,
  }
}

function locatorMode(snapshot: ProgressSnapshot): unknown {
  return snapshot.locator.mode
}

function numberValue(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function exactlyEqual(left: ProgressSnapshot | null, right: ProgressSnapshot | null): boolean {
  if (!left || !right) return left === right
  return (
    left.slug === right.slug &&
    left.version === right.version &&
    left.percent === right.percent &&
    JSON.stringify(left.locator) === JSON.stringify(right.locator)
  )
}

export function progressSnapshotsDiffer(
  desired: ProgressSnapshot | null,
  confirmed: ProgressSnapshot | null
): boolean {
  if (!desired) return false
  if (!confirmed) return true
  if (desired.slug !== confirmed.slug || desired.version !== confirmed.version) {
    return true
  }
  if (Math.abs(desired.percent - confirmed.percent) >= percentTolerance) {
    return true
  }

  const desiredMode = locatorMode(desired)
  const confirmedMode = locatorMode(confirmed)
  if (desiredMode !== confirmedMode) return true

  if (desiredMode === 'scroll') {
    const desiredY = numberValue(desired.locator.scrollY)
    const confirmedY = numberValue(confirmed.locator.scrollY)
    if (desiredY === null || confirmedY === null) {
      return !exactlyEqual(desired, confirmed)
    }
    return Math.abs(desiredY - confirmedY) >= scrollTolerancePx
  }

  if (desiredMode === 'paged') {
    const desiredPage = numberValue(desired.locator.page)
    const confirmedPage = numberValue(confirmed.locator.page)
    if (desiredPage === null || confirmedPage === null) {
      return !exactlyEqual(desired, confirmed)
    }
    return desiredPage !== confirmedPage
  }

  return !exactlyEqual(desired, confirmed)
}

export function createProgressSaveCoordinator(
  options: ProgressSaveCoordinatorOptions
): ProgressSaveCoordinator {
  const debounceMs = options.debounceMs ?? 450
  const listeners = new Set<(state: ProgressSaveState) => void>()
  const waiters = new Set<DrainWaiter>()

  let status: ProgressSaveStatus = 'idle'
  let desired: ProgressSnapshot | null = null
  let confirmed: ProgressSnapshot | null = null
  let lastError: unknown = null
  let timer: TimerHandle | null = null
  let inFlight = false
  let forcePending = false
  let nextKeepalive = false
  let disposed = false

  function snapshotState(): ProgressSaveState {
    return {
      status,
      desired: cloneSnapshot(desired),
      confirmed: cloneSnapshot(confirmed),
      error: lastError,
    }
  }

  function emit() {
    if (disposed) return
    const state = snapshotState()
    for (const listener of listeners) listener(state)
  }

  function clearScheduledTimer() {
    if (timer === null) return
    options.clearTimer(timer)
    timer = null
  }

  function dirty(): boolean {
    return forcePending || progressSnapshotsDiffer(desired, confirmed)
  }

  function resolveWaiters() {
    for (const waiter of waiters) waiter.resolve()
    waiters.clear()
  }

  function rejectWaiters(error: unknown) {
    for (const waiter of waiters) waiter.reject(error)
    waiters.clear()
  }

  function schedule() {
    clearScheduledTimer()
    timer = options.setTimer(() => {
      timer = null
      void flush().catch(() => {
        // State and retry ownership remain inside the coordinator.
      })
    }, debounceMs)
  }

  async function pump() {
    if (disposed || inFlight || !desired || !dirty()) return

    const captured = cloneSnapshot(desired)
    if (!captured) return
    const keepalive = nextKeepalive
    nextKeepalive = false
    forcePending = false
    inFlight = true
    status = 'saving'
    lastError = null
    emit()

    try {
      await options.persist(captured, keepalive ? { keepalive: true } : undefined)
    } catch (error) {
      inFlight = false
      if (disposed) return
      forcePending = true
      nextKeepalive = false
      lastError = error
      status = 'error'
      emit()
      rejectWaiters(error)
      return
    }

    inFlight = false
    if (disposed) return

    confirmed = captured
    lastError = null
    if (exactlyEqual(desired, captured)) {
      forcePending = false
    }

    if (dirty()) {
      status = 'saving'
      emit()
      void pump()
      return
    }

    nextKeepalive = false
    status = 'saved'
    emit()
    resolveWaiters()
  }

  function initialize(
    confirmedSnapshot: ProgressSnapshot | null,
    desiredSnapshot: ProgressSnapshot | null = confirmedSnapshot
  ) {
    if (disposed) return
    if (inFlight) {
      throw new Error('cannot initialize progress while a save is in flight')
    }

    clearScheduledTimer()
    confirmed = cloneSnapshot(confirmedSnapshot)
    desired = cloneSnapshot(desiredSnapshot)
    forcePending = false
    nextKeepalive = false
    lastError = null
    status = progressSnapshotsDiffer(desired, confirmed) ? 'dirty' : 'idle'
    emit()
  }

  function update(
    snapshot: ProgressSnapshot,
    updateOptions: { force?: boolean; debounce?: boolean } = {}
  ) {
    if (disposed) return

    desired = cloneSnapshot(snapshot)
    if (updateOptions.force) forcePending = true

    if (status === 'error') {
      // A previous failed write remains dirty even if the latest movement is
      // within tolerance of the last confirmed server state.
      forcePending = true
      emit()
      return
    }

    if (inFlight) {
      status = 'saving'
      emit()
      return
    }

    if (!dirty()) {
      clearScheduledTimer()
      status = status === 'saved' ? 'saved' : 'idle'
      emit()
      return
    }

    status = 'dirty'
    emit()
    if (updateOptions.debounce !== false) schedule()
  }

  function flush(
    persistenceOptions: ProgressPersistenceOptions = {}
  ): Promise<void> {
    if (disposed) return Promise.reject(new Error('progress coordinator is disposed'))

    clearScheduledTimer()
    if (persistenceOptions.keepalive) nextKeepalive = true

    if (!inFlight && !dirty()) {
      nextKeepalive = false
      return Promise.resolve()
    }

    const drained = new Promise<void>((resolve, reject) => {
      waiters.add({ resolve, reject })
    })

    if (!inFlight) {
      lastError = null
      void pump()
    }
    return drained
  }

  function retry(): Promise<void> {
    return flush()
  }

  function bestEffortKeepaliveFlush(): Promise<void> {
    return flush({ keepalive: true })
  }

  function subscribe(listener: (state: ProgressSaveState) => void) {
    if (disposed) return () => {}
    listeners.add(listener)
    listener(snapshotState())
    return () => listeners.delete(listener)
  }

  function dispose() {
    if (disposed) return
    disposed = true
    clearScheduledTimer()
    listeners.clear()
    rejectWaiters(new Error('progress coordinator is disposed'))
  }

  return {
    initialize,
    update,
    flush,
    retry,
    bestEffortKeepaliveFlush,
    current: snapshotState,
    subscribe,
    dispose,
  }
}
