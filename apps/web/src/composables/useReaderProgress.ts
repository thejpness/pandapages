import { computed, ref, shallowRef } from 'vue'
import {
  getAPIErrorStatus,
  getProgress,
  saveProgress,
  type ProgressState,
} from '../lib/api'
import {
  mapReaderProgressAcrossVersions,
  type CrossVersionMapping,
} from '../lib/reader-cross-version-progress'
import { findReaderResumeSegment } from '../lib/reader-scroll-location'
import type { ReaderLocatorV2, ReaderStorySegment } from '../lib/reader-locator-v2'
import {
  createProgressBaselineController,
  type ProgressBaselineController,
  type ProgressBaselineState,
} from '../lib/progress-baseline-controller'
import {
  readerLifecyclePersistenceAllowed,
  readerLibraryPersistenceStrategy,
  readerProgressPresentation,
} from '../lib/reader-progress-policy'
import {
  createProgressSaveCoordinator,
  progressSnapshotsDiffer,
  type ProgressSaveCoordinator,
  type ProgressSaveState,
  type ProgressSnapshot,
} from '../lib/progress-save-coordinator'

export type ReaderCapturedPosition = {
  locator: ReaderLocatorV2
  percent: number
}

export type ReaderProgressDecision =
  | { kind: 'resume'; locator: ReaderLocatorV2; percent: number }
  | {
      kind: 'changed'
      savedVersion: number
      mapping: CrossVersionMapping
    }

export type UseReaderProgressOptions = {
  capture: () => ReaderCapturedPosition | null
  onSessionLoss: (slug: string) => Promise<void> | void
  navigateToLibrary: () => Promise<void>
  onNavigationError: (message: string) => void
}

type StoryContext = {
  slug: string
  version: number
  segments: readonly ReaderStorySegment[]
  generation: number
}

const initialBaseline = (): ProgressBaselineState<ProgressState | null> => ({
  status: 'loading',
  value: null,
  error: null,
  attempt: 0,
})

const initialSave = (): ProgressSaveState => ({
  status: 'idle',
  desired: null,
  confirmed: null,
  error: null,
})

function clamp01(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(1, value))
}

function isMeaningful(progress: ProgressState): boolean {
  return !(
    progress.locator.segment.ordinal === 1 &&
    progress.locator.segment.offset <= 0.02 &&
    progress.percent <= 0.02
  )
}

export function useReaderProgress(options: UseReaderProgressOptions) {
  const baselineState = ref<ProgressBaselineState<ProgressState | null>>(
    initialBaseline(),
  )
  const saveState = ref<ProgressSaveState>(initialSave())
  const decision = shallowRef<ReaderProgressDecision | null>(null)
  const captureSuppressed = ref(false)
  const captureSuppressionOwners = new Set<symbol>()
  const navigatingToLibrary = ref(false)
  const leaveAfterSaveFailure = ref(false)
  const announcement = ref('')

  let context: StoryContext | null = null
  let preparedSlug: string | null = null
  let generation = 0
  let baselineController: ProgressBaselineController<ProgressState | null> | null = null
  let unsubscribeBaseline: (() => void) | null = null
  let coordinator: ProgressSaveCoordinator | null = null
  let unsubscribeSave: (() => void) | null = null
  let baselineOrigin: ProgressSnapshot | null = null
  let movedBeforeReady = false
  let awaitingIntent = false
  let handlingSessionLoss = false
  let lifecycleSuppressed = false

  const presentation = computed(() =>
    readerProgressPresentation({
      baselineStatus: baselineState.value.status,
      baselineAttempt: baselineState.value.attempt,
      saveStatus: saveState.value.status,
    }),
  )
  const baselineChecking = computed(
    () =>
      baselineState.value.status === 'loading' &&
      baselineState.value.attempt > 1,
  )
  const saveActive = computed(() => saveState.value.status === 'saving')
  const statusText = computed(() => presentation.value.text)
  const retryKind = computed(() => presentation.value.retryKind)
  const retryDisabled = computed(() =>
    presentation.value.retryDisabled || saveActive.value,
  )
  const captureEnabled = computed(
    () => !captureSuppressed.value && decision.value === null,
  )

  function suppressCapture(): () => void {
    const owner = Symbol('reader-progress-capture-suppression')
    captureSuppressionOwners.add(owner)
    captureSuppressed.value = true
    let released = false
    return () => {
      if (released) return
      released = true
      captureSuppressionOwners.delete(owner)
      captureSuppressed.value = captureSuppressionOwners.size > 0
    }
  }

  function clearCaptureSuppressions() {
    captureSuppressionOwners.clear()
    captureSuppressed.value = false
  }

  function snapshot(
    position: ReaderCapturedPosition | null,
    activeContext = context,
  ): ProgressSnapshot | null {
    if (!position || !activeContext) return null
    return {
      slug: activeContext.slug,
      version: activeContext.version,
      locator: position.locator,
      percent: clamp01(position.percent),
    }
  }

  function disposeCoordinator() {
    unsubscribeSave?.()
    unsubscribeSave = null
    coordinator?.dispose()
    coordinator = null
  }

  function disposeBaseline() {
    unsubscribeBaseline?.()
    unsubscribeBaseline = null
    baselineController?.dispose()
    baselineController = null
  }

  function dispose() {
    generation += 1
    disposeBaseline()
    disposeCoordinator()
    context = null
    preparedSlug = null
    baselineOrigin = null
    movedBeforeReady = false
    awaitingIntent = false
    handlingSessionLoss = false
    lifecycleSuppressed = false
    clearCaptureSuppressions()
    decision.value = null
    baselineState.value = initialBaseline()
    saveState.value = initialSave()
    leaveAfterSaveFailure.value = false
    navigatingToLibrary.value = false
    announcement.value = ''
  }

  async function sessionLossFor(
    slug: string,
    activeGeneration: number,
  ) {
    if (
      handlingSessionLoss ||
      generation !== activeGeneration ||
      preparedSlug !== slug
    ) return
    handlingSessionLoss = true
    leaveAfterSaveFailure.value = false
    disposeBaseline()
    disposeCoordinator()
    await options.onSessionLoss(slug)
  }

  async function sessionLoss(active: StoryContext) {
    await sessionLossFor(active.slug, active.generation)
  }

  function createCoordinator(active: StoryContext): ProgressSaveCoordinator {
    disposeCoordinator()
    const created = createProgressSaveCoordinator({
      persist: (progress, persistenceOptions) =>
        saveProgress(
          progress.slug,
          progress.version,
          progress.locator,
          progress.percent,
          persistenceOptions,
        ),
      debounceMs: 450,
      setTimer: (callback, delayMs) => window.setTimeout(callback, delayMs),
      clearTimer: (handle) => window.clearTimeout(handle),
    })
    coordinator = created
    unsubscribeSave = created.subscribe((state) => {
      if (context?.generation !== active.generation) return
      saveState.value = state
      if (state.status === 'error' && getAPIErrorStatus(state.error) === 401) {
        void sessionLoss(active)
      }
    })
    return created
  }

  function confirmedSnapshot(
    progress: ProgressState | null,
    active: StoryContext,
  ): ProgressSnapshot | null {
    if (!progress || progress.version !== active.version) return null
    return {
      slug: active.slug,
      version: progress.version,
      locator: progress.locator,
      percent: clamp01(progress.percent),
    }
  }

  function initializeReadyBaseline(
    progress: ProgressState | null,
    attempt: number,
    active: StoryContext,
  ) {
    if (
      context?.generation !== active.generation ||
      handlingSessionLoss ||
      coordinator
    ) return

    const current = snapshot(options.capture(), active)
    const confirmed = confirmedSnapshot(progress, active)
    const created = createCoordinator(active)
    const recoveredAfterMovement = attempt > 1 && movedBeforeReady

    if (progress && progress.version !== active.version) {
      const mapping = mapReaderProgressAcrossVersions({
        oldVersion: progress.version,
        oldLocator: progress.locator,
        oldPercent: progress.percent,
        currentVersion: active.version,
        currentSegments: active.segments,
      })
      if (context?.generation !== active.generation) return
      created.initialize(null, null)
      decision.value = {
        kind: 'changed',
        savedVersion: progress.version,
        mapping,
      }
      awaitingIntent = false
      movedBeforeReady = false
      return
    }

    const restorable =
      progress && progress.version === active.version
        ? findReaderResumeSegment(active.segments, progress.locator)
        : null

    if (
      progress &&
      restorable &&
      isMeaningful(progress) &&
      !recoveredAfterMovement
    ) {
      created.initialize(confirmed, confirmed)
      decision.value = {
        kind: 'resume',
        locator: progress.locator,
        percent: clamp01(progress.percent),
      }
      awaitingIntent = false
      movedBeforeReady = false
      return
    }

    if (movedBeforeReady && current) {
      created.initialize(confirmed, confirmed)
      created.update(current, { force: true })
    } else if (confirmed) {
      created.initialize(confirmed, confirmed)
      awaitingIntent = !restorable
    } else {
      // A known empty baseline is safe, but the untouched opening position
      // does not need an eager write.
      created.initialize(current, current)
    }

    if (attempt > 1) announcement.value = 'Progress is available again.'
    movedBeforeReady = false
  }

  function prepare(slug: string) {
    dispose()
    preparedSlug = slug
    const activeGeneration = generation
    const created = createProgressBaselineController({
      load: async () => (await getProgress(slug)).progress,
    })
    baselineController = created
    unsubscribeBaseline = created.subscribe((state) => {
      if (
        generation !== activeGeneration ||
        preparedSlug !== slug
      ) return
      baselineState.value = state
      const active = context
      if (
        state.status === 'ready' &&
        active?.generation === activeGeneration
      ) {
        initializeReadyBaseline(state.value, state.attempt, active)
      } else if (
        state.status === 'unavailable' &&
        getAPIErrorStatus(state.error) === 401
      ) {
        void sessionLossFor(slug, activeGeneration)
      }
    })
    void created.load()
  }

  function begin(
    slug: string,
    version: number,
    segments: readonly ReaderStorySegment[],
  ) {
    if (preparedSlug !== slug || !baselineController) prepare(slug)
    if (handlingSessionLoss) return

    const active: StoryContext = { slug, version, segments, generation }
    context = active
    baselineOrigin = snapshot(options.capture(), active)
    if (baselineState.value.status === 'ready') {
      initializeReadyBaseline(
        baselineState.value.value,
        baselineState.value.attempt,
        active,
      )
    }
  }

  function movement(position: ReaderCapturedPosition) {
    const current = snapshot(position)
    if (!current || handlingSessionLoss || captureSuppressed.value) return

    if (baselineState.value.status !== 'ready') {
      if (progressSnapshotsDiffer(current, baselineOrigin)) movedBeforeReady = true
      return
    }
    if (decision.value) return
    if (awaitingIntent) awaitingIntent = false
    coordinator?.update(current)
  }

  function desired(
    position: ReaderCapturedPosition | null,
    options: { force?: boolean; debounce?: boolean } = {},
  ) {
    const current = snapshot(position)
    if (!current || handlingSessionLoss) return
    if (baselineState.value.status !== 'ready') {
      if (progressSnapshotsDiffer(current, baselineOrigin)) movedBeforeReady = true
      return
    }
    if (decision.value) return
    awaitingIntent = false
    coordinator?.update(current, options)
  }

  async function retry() {
    if (
      baselineState.value.status === 'unavailable' ||
      baselineChecking.value
    ) {
      await baselineController?.retry()
      return
    }
    try {
      await coordinator?.retry()
    } catch {
      // The coordinator retains the desired snapshot and truthful error state.
    }
  }

  async function resume(
    restore: (locator: ReaderLocatorV2) => Promise<boolean>,
  ) {
    const offer = decision.value
    const activeGeneration = context?.generation
    if (offer?.kind !== 'resume' || activeGeneration === undefined) return
    const releaseCaptureSuppression = suppressCapture()
    try {
      const restored = await restore(offer.locator)
      if (context?.generation !== activeGeneration || !restored) return
      decision.value = null
      awaitingIntent = true
      announcement.value = 'Reading place restored.'
    } finally {
      releaseCaptureSuppression()
    }
  }

  async function continueUpdated(
    restore: (locator: ReaderLocatorV2) => Promise<boolean>,
  ): Promise<boolean> {
    const offer = decision.value
    const activeGeneration = context?.generation
    if (
      offer?.kind !== 'changed' ||
      offer.mapping.kind === 'none' ||
      activeGeneration === undefined
    ) {
      return false
    }

    const releaseCaptureSuppression = suppressCapture()
    let restored: boolean
    try {
      restored = await restore(offer.mapping.locator)
    } finally {
      releaseCaptureSuppression()
    }
    if (
      !restored ||
      context?.generation !== activeGeneration ||
      decision.value !== offer
    ) {
      return false
    }

    decision.value = null
    awaitingIntent = true
    announcement.value =
      offer.mapping.confidence === 'high'
        ? 'The same reading place was restored.'
        : offer.mapping.confidence === 'medium'
          ? 'The same chapter was restored.'
          : 'An approximate reading place was restored.'
    return true
  }

  async function startCurrentVersion(
    moveToBeginning: () => Promise<ReaderCapturedPosition | null>,
  ): Promise<boolean> {
    const offer = decision.value
    const activeGeneration = context?.generation
    if (!offer || activeGeneration === undefined) return false
    const releaseCaptureSuppression = suppressCapture()
    try {
      const beginning = await moveToBeginning()
      if (
        !beginning ||
        context?.generation !== activeGeneration ||
        decision.value !== offer
      ) {
        return false
      }
      const current = snapshot(beginning)
      if (!current || baselineState.value.status !== 'ready') return false

      decision.value = null
      awaitingIntent = false
      coordinator?.update(current, { force: true, debounce: false })
      try {
        await coordinator?.flush()
      } catch {
        // The normal save-error state remains visible and retryable.
      }
      return true
    } finally {
      releaseCaptureSuppression()
    }
  }

  function dismissDecision() {
    if (decision.value?.kind !== 'resume') return
    decision.value = null
    awaitingIntent = true
  }

  async function returnToLibraryFromVersionDecision() {
    if (
      decision.value?.kind !== 'changed' ||
      navigatingToLibrary.value
    ) {
      return
    }
    leaveAfterSaveFailure.value = false
    navigatingToLibrary.value = true
    lifecycleSuppressed = true
    try {
      await options.navigateToLibrary()
    } catch {
      lifecycleSuppressed = false
      options.onNavigationError('The Library could not be opened. Try again.')
    } finally {
      navigatingToLibrary.value = false
    }
  }

  async function goLibrary() {
    if (navigatingToLibrary.value) return
    leaveAfterSaveFailure.value = false
    navigatingToLibrary.value = true

    if (
      readerLibraryPersistenceStrategy(baselineState.value.status) ===
      'immediate'
    ) {
      lifecycleSuppressed = true
      try {
        await options.navigateToLibrary()
      } catch {
        lifecycleSuppressed = false
        options.onNavigationError('The Library could not be opened. Try again.')
      } finally {
        navigatingToLibrary.value = false
      }
      return
    }

    try {
      if (saveState.value.status === 'error') {
        await coordinator?.retry()
      } else {
        if (
          !captureSuppressed.value &&
          !decision.value &&
          !awaitingIntent
        ) {
          const current = snapshot(options.capture())
          if (current) coordinator?.update(current, { debounce: false })
        }
        await coordinator?.flush()
      }
    } catch (error) {
      if (getAPIErrorStatus(error) !== 401) leaveAfterSaveFailure.value = true
      navigatingToLibrary.value = false
      return
    }

    lifecycleSuppressed = true
    try {
      await options.navigateToLibrary()
    } catch {
      lifecycleSuppressed = false
      options.onNavigationError('The Library could not be opened. Try again.')
    } finally {
      navigatingToLibrary.value = false
    }
  }

  async function leaveAnyway() {
    leaveAfterSaveFailure.value = false
    lifecycleSuppressed = true
    try {
      await options.navigateToLibrary()
    } catch {
      lifecycleSuppressed = false
      options.onNavigationError('The Library could not be opened. Try again.')
    }
  }

  function pageHide() {
    if (lifecycleSuppressed) return
    if (
      !readerLifecyclePersistenceAllowed({
        baselineStatus: baselineState.value.status,
        sessionLoss: handlingSessionLoss,
        decisionPending: decision.value !== null,
        awaitingIntent,
      })
    ) return
    // A placement owner may be restoring a semantic anchor while the page is
    // hidden or unmounted. In that state the rendered position is transient:
    // only drain progress the coordinator already owns, never capture that
    // intermediate position as a new desired snapshot.
    if (!captureSuppressed.value) {
      const current = snapshot(options.capture())
      if (current) coordinator?.update(current, { debounce: false })
    }
    void coordinator?.bestEffortKeepaliveFlush().catch(() => {
      // The browser may terminate before a keepalive response is observed.
    })
  }

  return {
    baselineState,
    saveState,
    decision,
    captureSuppressed,
    captureEnabled,
    suppressCapture,
    clearCaptureSuppressions,
    navigatingToLibrary,
    leaveAfterSaveFailure,
    announcement,
    statusText,
    retryKind,
    retryDisabled,
    saveActive,
    prepare,
    begin,
    movement,
    desired,
    retry,
    resume,
    continueUpdated,
    startCurrentVersion,
    dismissDecision,
    returnToLibraryFromVersionDecision,
    goLibrary,
    leaveAnyway,
    pageHide,
    dispose,
  }
}
