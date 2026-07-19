<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  type Ref,
  watch,
} from 'vue'
import { useEventListener, usePreferredReducedMotion } from '@vueuse/core'
import { useRoute, useRouter } from 'vue-router'
import ReaderChaptersDialog from '../components/reader/ReaderChaptersDialog.vue'
import ReaderHeader from '../components/reader/ReaderHeader.vue'
import ReaderPagedView from '../components/reader/ReaderPagedView.vue'
import ReaderProgressStatus from '../components/reader/ReaderProgressStatus.vue'
import ReaderResumeDialog from '../components/reader/ReaderResumeDialog.vue'
import ReaderScrollView from '../components/reader/ReaderScrollView.vue'
import ReaderSettingsDialog from '../components/reader/ReaderSettingsDialog.vue'
import ReaderStoryState from '../components/reader/ReaderStoryState.vue'
import { useReaderPreferences } from '../composables/useReaderPreferences'
import {
  useReaderProgress,
  type ReaderCapturedPosition,
} from '../composables/useReaderProgress'
import { useReaderStory } from '../composables/useReaderStory'
import {
  buildReaderChapters,
  currentReaderChapter,
  type ReaderChapter,
} from '../lib/reader-chapters'
import type { ReaderLocatorV2 } from '../lib/reader-locator-v2'
import { planReaderModeTransition } from '../lib/reader-mode-transition'
import {
  READER_PREFERENCES_V2_DEFAULTS,
  validateReaderPreferencesV2,
  type ReaderMode,
  type ReaderPreferencesV2,
} from '../lib/reader-preferences-v2'
import { authState } from '../lib/session'
import { safeNextPath } from '../lib/session-navigation'
import { navigationDidFail } from '../lib/session-transitions'

type ReaderView = {
  mode: ReaderMode
  instanceId: symbol
  capture: () => ReaderCapturedPosition | null
  whenReady: () => Promise<void>
  restore: (
    locator: ReaderLocatorV2,
    options?: { allowMotion?: boolean },
  ) => Promise<boolean>
  moveToOrdinal: (
    ordinal: number,
    offset?: number,
    options?: { allowMotion?: boolean },
  ) => Promise<ReaderCapturedPosition | null>
  focusContent: () => void
}

type ReaderPlacementKind = 'preference' | 'chapter' | 'decision'

type ReaderPlacementOwner = {
  token: symbol
  kind: ReaderPlacementKind
}

type ReaderPlacementWaiter = {
  kind: ReaderPlacementKind
  valid: () => boolean
  resolve: (release: (() => void) | null) => void
}

type ReaderPlacementQueue = {
  owner: ReaderPlacementOwner | null
  waiters: ReaderPlacementWaiter[]
  preferencePending: Ref<boolean>
  chapterPending: Ref<boolean>
}

function createReaderPlacementQueue(): ReaderPlacementQueue {
  return {
    owner: null,
    waiters: [],
    preferencePending: ref(false),
    chapterPending: ref(false),
  }
}

function updateReaderPlacementPending(queue: ReaderPlacementQueue) {
  const pendingKinds = [
    queue.owner?.kind,
    ...queue.waiters.map(({ kind }) => kind),
  ]
  queue.preferencePending.value = pendingKinds.includes('preference')
  queue.chapterPending.value = pendingKinds.includes('chapter')
}

function claimReaderPlacement(
  queue: ReaderPlacementQueue,
  kind: ReaderPlacementKind,
): () => void {
  const owner = { token: Symbol('reader-placement'), kind }
  queue.owner = owner
  updateReaderPlacementPending(queue)
  let released = false
  return () => {
    if (released) return
    released = true
    if (queue.owner?.token !== owner.token) return
    queue.owner = null

    while (queue.waiters.length > 0) {
      const waiter = queue.waiters.shift()
      if (!waiter) break
      if (!waiter.valid()) {
        waiter.resolve(null)
        continue
      }

      // Ownership is transferred before this one waiter resumes.
      const releaseNext = claimReaderPlacement(queue, waiter.kind)
      waiter.resolve(releaseNext)
      return
    }
    updateReaderPlacementPending(queue)
  }
}

function acquireReaderPlacement(
  queue: ReaderPlacementQueue,
  kind: ReaderPlacementKind,
  valid: () => boolean,
): Promise<(() => void) | null> {
  if (!valid()) return Promise.resolve(null)
  if (!queue.owner) {
    return Promise.resolve(claimReaderPlacement(queue, kind))
  }
  return new Promise((resolve) => {
    queue.waiters.push({ kind, valid, resolve })
    updateReaderPlacementPending(queue)
  })
}

function resetReaderPlacementQueue(queue: ReaderPlacementQueue) {
  queue.owner = null
  for (const waiter of queue.waiters.splice(0)) waiter.resolve(null)
  updateReaderPlacementPending(queue)
}

const route = useRoute()
const router = useRouter()
const reducedMotion = usePreferredReducedMotion()
const slug = ref(String(route.params.slug))
const scrollReaderView = ref<ReaderView | null>(null)
const pagedReaderView = ref<ReaderView | null>(null)
const readerInitialized = ref(false)
const settingsOpen = ref(false)
const chaptersOpen = ref(false)
const activeOrdinal = ref(1)
const percent = ref(0)
const navigationMessage = ref('')
let preferenceGeneration = 0
let preferenceDraftGeneration = 0
let readerGeneration = 0
let resumeFocusPending = false
const readerPlacementQueue = createReaderPlacementQueue()

const { preferences, fontStack } = useReaderPreferences()
const settingsPreferences = ref<ReaderPreferencesV2>({ ...preferences.value })

async function moveToUnlock(storySlug: string) {
  authState.confirmLocked()
  const next = safeNextPath('/read/' + encodeURIComponent(storySlug))
  try {
    const result = await router.replace({ path: '/unlock', query: { next } })
    if (navigationDidFail(result)) {
      story.contentState.value = { status: 'unavailable' }
    }
  } catch {
    story.contentState.value = { status: 'unavailable' }
  }
}

function readerViewForMode(mode: ReaderMode): ReaderView | null {
  return mode === 'scroll' ? scrollReaderView.value : pagedReaderView.value
}

function currentReaderView(): ReaderView | null {
  return readerViewForMode(preferences.value.mode)
}

async function waitForMountedReaderView(
  mode: ReaderMode,
  previousInstanceId: symbol | null,
  operation: number,
  draft: number,
  expectedReaderGeneration: number,
): Promise<ReaderView | null> {
  const targetRef = mode === 'scroll' ? scrollReaderView : pagedReaderView
  const valid = (view: ReaderView | null) =>
    operation === preferenceGeneration &&
    draft === preferenceDraftGeneration &&
    expectedReaderGeneration === readerGeneration &&
    view?.mode === mode &&
    view.instanceId !== previousInstanceId
  if (valid(targetRef.value)) return targetRef.value

  return new Promise<ReaderView | null>((resolve) => {
    const stop = watch(
      [
        targetRef,
        () => preferenceGeneration,
        () => preferenceDraftGeneration,
        () => readerGeneration,
      ],
      ([view]) => {
        if (
          operation !== preferenceGeneration ||
          draft !== preferenceDraftGeneration ||
          expectedReaderGeneration !== readerGeneration
        ) {
          stop()
          resolve(null)
          return
        }
        if (valid(view)) {
          stop()
          resolve(view)
        }
      },
      { flush: 'post' },
    )
  })
}

function captureCurrent(): ReaderCapturedPosition | null {
  return currentReaderView()?.capture() ?? null
}

const progress = useReaderProgress({
  capture: captureCurrent,
  onSessionLoss: moveToUnlock,
  navigateToLibrary: async () => {
    await router.push('/library')
  },
  onNavigationError: (message) => {
    navigationMessage.value = message
  },
})

function suppressProgressCapture(): () => void {
  return progress.suppressCapture()
}

function clearProgressCaptureSuppressions() {
  progress.clearCaptureSuppressions()
}

function invalidateReaderPlacementWork() {
  preferenceGeneration += 1
  preferenceDraftGeneration += 1
  resetReaderPlacementQueue(readerPlacementQueue)
}

const story = useReaderStory({
  onSessionEnded: moveToUnlock,
  onReady: async (loaded) => {
    const activeGeneration = readerGeneration
    await currentReaderView()?.whenReady()
    if (activeGeneration !== readerGeneration) return
    activeOrdinal.value = loaded.segments[0]?.ordinal ?? 1
    percent.value = captureCurrent()?.percent ?? 0
    currentReaderView()?.focusContent()
    progress.begin(loaded.slug, loaded.version, loaded.segments)
    readerInitialized.value = true
  },
})

const chapters = computed(() =>
  buildReaderChapters(story.story.value?.segments ?? []),
)
const activeSegment = computed(
  () =>
    story.story.value?.segments.find(
      (segment) => segment.ordinal === activeOrdinal.value,
    ) ?? null,
)
const chapter = computed(() =>
  currentReaderChapter(chapters.value, activeSegment.value),
)
const themeClass = computed(
  () => 'reader-theme--' + preferences.value.theme,
)
const resumeOpen = computed(
  () =>
    progress.decision.value !== null &&
    !settingsOpen.value &&
    !chaptersOpen.value &&
    !readerPlacementQueue.preferencePending.value &&
    !readerPlacementQueue.chapterPending.value,
)
const resumeKind = computed(() => progress.decision.value?.kind ?? 'resume')
const resumePercent = computed(() =>
  progress.decision.value?.kind === 'resume'
    ? progress.decision.value.percent
    : 0,
)

function onPosition(position: ReaderCapturedPosition) {
  percent.value = position.percent
  progress.movement(position)
}

function onActive(ordinal: number) {
  activeOrdinal.value = ordinal
}

async function restore(locator: ReaderLocatorV2): Promise<boolean> {
  const activeGeneration = readerGeneration
  const restored = await currentReaderView()?.restore(locator)
  if (activeGeneration !== readerGeneration) return false
  const current = captureCurrent()
  if (current) {
    percent.value = current.percent
    activeOrdinal.value = current.locator.segment.ordinal
  }
  if (restored) resumeFocusPending = true
  return restored ?? false
}

async function moveToBeginning(): Promise<ReaderCapturedPosition | null> {
  const activeGeneration = readerGeneration
  const first = story.story.value?.segments[0]
  if (!first) return null
  const position = await currentReaderView()?.moveToOrdinal(
    first.ordinal,
    0,
    { allowMotion: false },
  )
  if (activeGeneration !== readerGeneration) return null
  if (position) {
    percent.value = position.percent
    activeOrdinal.value = first.ordinal
  }
  return position ?? null
}

async function applyPreferences(
  candidate: ReaderPreferencesV2,
  draft: number,
) {
  const validated = validateReaderPreferencesV2(candidate)
  if (!validated) return
  const activeGeneration = readerGeneration
  const releasePreferenceWork = await acquireReaderPlacement(
    readerPlacementQueue,
    'preference',
    () => activeGeneration === readerGeneration,
  )
  if (!releasePreferenceWork) return
  let releaseCaptureSuppression: (() => void) | null = null
  try {
    // FIFO acquisition is retained, but obsolete drafts are coalesced before
    // they enter the expensive view transition. The latest validated draft is
    // therefore the final operation that can apply and publish.
    if (
      activeGeneration !== readerGeneration ||
      draft !== preferenceDraftGeneration
    ) return

    const previous = preferences.value
    const sourceView = readerViewForMode(previous.mode)
    if (!sourceView || sourceView.mode !== previous.mode) {
      throw new Error('The source Reader view is unavailable.')
    }
    const transition = planReaderModeTransition(sourceView.capture())
    const anchor = transition.anchor
    const operation = ++preferenceGeneration
    const operationIsCurrent = () =>
      operation === preferenceGeneration &&
      draft === preferenceDraftGeneration &&
      activeGeneration === readerGeneration
    const modeChanged = previous.mode !== validated.mode
    const previousTargetInstanceId = modeChanged
      ? readerViewForMode(validated.mode)?.instanceId ?? null
      : null
    const debouncePagedReflow =
      !modeChanged &&
      previous.mode === 'paged' &&
      (previous.fontFamily !== validated.fontFamily ||
        previous.fontSize !== validated.fontSize ||
        previous.lineHeight !== validated.lineHeight ||
        previous.contentWidth !== validated.contentWidth)

    releaseCaptureSuppression = suppressProgressCapture()
    preferences.value = validated

    const targetView = modeChanged
      ? await waitForMountedReaderView(
          validated.mode,
          previousTargetInstanceId,
          operation,
          draft,
          activeGeneration,
        )
      : sourceView
    if (!targetView || !operationIsCurrent()) return

    if (debouncePagedReflow) {
      await new Promise<void>((resolve) => {
        window.setTimeout(resolve, 120)
      })
      if (!operationIsCurrent()) return
    }

    await targetView.whenReady()
    if (
      !operationIsCurrent() ||
      targetView !== readerViewForMode(validated.mode) ||
      targetView.mode !== validated.mode
    ) return

    if (anchor) {
      const restored = await targetView.restore(anchor.locator, { allowMotion: false })
      if (!restored || !operationIsCurrent()) {
        if (!restored) throw new Error('The target Reader view rejected the canonical location.')
        return
      }
    }

    const current = targetView.capture()
    if (anchor && current) {
      const expected = anchor.locator.segment
      const actual = current.locator.segment
      const sameIdentity =
        actual.key === expected.key &&
        actual.occurrence === expected.occurrence &&
        actual.ordinal === expected.ordinal
      const offsetStable = Math.abs(actual.offset - expected.offset) <= 0.08
      const percentStable = Math.abs(current.percent - anchor.percent) <= 0.03
      if (!sameIdentity || !offsetStable || !percentStable) {
        throw new Error('The target Reader view did not preserve the canonical location.')
      }
    } else if (anchor) {
      throw new Error('The target Reader view could not capture the restored location.')
    }

    if (!operationIsCurrent()) return
    if (current) {
      percent.value = current.percent
      activeOrdinal.value = current.locator.segment.ordinal
    }
  } catch {
    if (
      activeGeneration === readerGeneration &&
      draft === preferenceDraftGeneration
    ) {
      navigationMessage.value =
        'Your reading place could not be restored after that settings change.'
    }
  } finally {
    releaseCaptureSuppression?.()
    releasePreferenceWork()
  }
}

function queuePreferenceDraft(candidate: ReaderPreferencesV2) {
  if (!readerInitialized.value) return
  const validated = validateReaderPreferencesV2(candidate)
  if (!validated) return
  settingsPreferences.value = validated
  const draft = ++preferenceDraftGeneration
  void applyPreferences(validated, draft)
}

function updatePreferences(candidate: ReaderPreferencesV2) {
  queuePreferenceDraft(candidate)
}

function changeMode(mode: ReaderMode) {
  if (!readerInitialized.value) return
  if (settingsPreferences.value.mode === mode) return
  queuePreferenceDraft({ ...settingsPreferences.value, mode })
}

function resetPreferences() {
  queuePreferenceDraft({ ...READER_PREFERENCES_V2_DEFAULTS })
}

async function selectChapter(selected: ReaderChapter) {
  if (!readerInitialized.value) return
  const activeGeneration = readerGeneration
  chaptersOpen.value = false
  const releaseChapterNavigation = await acquireReaderPlacement(
    readerPlacementQueue,
    'chapter',
    () => activeGeneration === readerGeneration,
  )
  if (!releaseChapterNavigation) return
  try {
    await nextTick()
    await new Promise<void>((resolve) => {
      window.requestAnimationFrame(() =>
        window.requestAnimationFrame(() => resolve()),
      )
    })
    if (activeGeneration !== readerGeneration) return
    preferenceGeneration += 1
    const releaseCaptureSuppression = suppressProgressCapture()
    try {
      const position = await currentReaderView()?.moveToOrdinal(
        selected.ordinal,
        0,
        { allowMotion: true },
      )
      if (activeGeneration !== readerGeneration) return
      if (position) {
        percent.value = position.percent
        activeOrdinal.value = selected.ordinal
        progress.desired(position)
        progress.announcement.value = 'Moved to ' + selected.title + '.'
      }
    } finally {
      releaseCaptureSuppression()
    }
  } finally {
    releaseChapterNavigation()
  }
}

async function resumeCurrentVersion() {
  const activeGeneration = readerGeneration
  const releasePlacement = await acquireReaderPlacement(
    readerPlacementQueue,
    'decision',
    () => activeGeneration === readerGeneration,
  )
  if (!releasePlacement) return
  let placementReleased = false
  const releasePlacementOnce = () => {
    if (placementReleased) return
    placementReleased = true
    releasePlacement()
  }
  try {
    if (activeGeneration !== readerGeneration) return
    preferenceGeneration += 1
    await progress.resume(async (locator) => {
      try {
        return await restore(locator)
      } finally {
        releasePlacementOnce()
      }
    })
  } finally {
    releasePlacementOnce()
  }
}

async function startCurrentVersion() {
  const activeGeneration = readerGeneration
  const releasePlacement = await acquireReaderPlacement(
    readerPlacementQueue,
    'decision',
    () => activeGeneration === readerGeneration,
  )
  if (!releasePlacement) return
  let placementReleased = false
  const releasePlacementOnce = () => {
    if (placementReleased) return
    placementReleased = true
    releasePlacement()
  }
  try {
    if (activeGeneration !== readerGeneration) return
    preferenceGeneration += 1
    await progress.startCurrentVersion(async () => {
      try {
        return await moveToBeginning()
      } finally {
        releasePlacementOnce()
      }
    })
  } finally {
    releasePlacementOnce()
  }
}

function closeResume(open: boolean) {
  if (!open) progress.dismissDecision()
}

async function loadCurrentStory() {
  navigationMessage.value = ''
  await story.load(slug.value)
}

async function goLibraryWithoutProgress() {
  try {
    await router.push('/library')
  } catch {
    navigationMessage.value = 'The Library could not be opened. Try again.'
  }
}

async function routeChanged(nextSlug: string) {
  readerGeneration += 1
  invalidateReaderPlacementWork()
  settingsPreferences.value = { ...preferences.value }
  progress.pageHide()
  progress.dispose()
  clearProgressCaptureSuppressions()
  story.dispose()
  readerInitialized.value = false
  settingsOpen.value = false
  chaptersOpen.value = false
  navigationMessage.value = ''
  resumeFocusPending = false
  percent.value = 0
  activeOrdinal.value = 1
  slug.value = nextSlug
  document.title = 'Panda Pages'
  await loadCurrentStory()
}

function onPageHide() {
  progress.pageHide()
}

function onVisibilityChange() {
  if (document.visibilityState === 'hidden') progress.pageHide()
}

useEventListener(window, 'pagehide', onPageHide)
useEventListener(document, 'visibilitychange', onVisibilityChange)

watch(
  () => String(route.params.slug),
  (nextSlug) => {
    if (nextSlug !== slug.value) void routeChanged(nextSlug)
  },
)

watch(
  resumeOpen,
  async (open, wasOpen) => {
    if (open || !wasOpen || !resumeFocusPending) return
    resumeFocusPending = false
    await nextTick()
    currentReaderView()?.focusContent()
  },
)

watch(
  () => preferences.value.theme,
  (theme) => {
    document.documentElement.dataset.readerTheme = theme
  },
  { immediate: true },
)

watch(
  () => story.contentState.value.status,
  async (status) => {
    if (status !== 'not-found' && status !== 'unavailable') return
    await nextTick()
    document
      .querySelector<HTMLElement>('.reader-state-card[tabindex]')
      ?.focus({ preventScroll: true })
  },
)

onMounted(() => {
  void loadCurrentStory()
})

onBeforeUnmount(() => {
  readerGeneration += 1
  invalidateReaderPlacementWork()
  progress.pageHide()
  progress.dispose()
  clearProgressCaptureSuppressions()
  story.dispose()
  delete document.documentElement.dataset.readerTheme
  document.title = 'Panda Pages'
})
</script>

<template>
  <div
    class="reader-shell"
    :class="themeClass"
    :data-reader-preference-pending="readerPlacementQueue.preferencePending.value ? 'true' : 'false'"
  >
    <ReaderStoryState
      v-if="story.contentState.value.status !== 'ready' || !story.story.value"
      :state="story.contentState.value"
      @retry="loadCurrentStory"
      @library="goLibraryWithoutProgress"
    />

    <template v-else>
      <ReaderHeader
        :title="story.story.value.title"
        :chapter-title="chapter?.title ?? ''"
        :percent="percent"
        :status-text="progress.statusText.value"
        :retry-kind="progress.retryKind.value"
        :retry-disabled="progress.retryDisabled.value"
        :chapters-available="chapters.length > 0"
        :reader-ready="readerInitialized && !readerPlacementQueue.preferencePending.value"
        :settings-open="settingsOpen"
        :chapters-open="chaptersOpen"
        :navigating="progress.navigatingToLibrary.value"
        @library="progress.goLibrary"
        @settings="settingsOpen = true"
        @chapters="chaptersOpen = true"
        @retry="progress.retry"
      />

      <ReaderProgressStatus
        :visible="progress.leaveAfterSaveFailure.value"
        :busy="progress.saveActive.value"
        @retry="progress.goLibrary"
        @leave="progress.leaveAnyway"
      />

      <p v-if="navigationMessage" class="reader-inline-alert" role="alert">
        {{ navigationMessage }}
      </p>

      <main
        class="reader-main"
        :class="{ 'reader-main--paged': preferences.mode === 'paged' }"
      >
        <ReaderScrollView
          v-if="preferences.mode === 'scroll'"
          ref="scrollReaderView"
          :title="story.story.value.title"
          :author="story.story.value.author"
          :language="story.story.value.language"
          :segments="story.story.value.segments"
          :font-family="fontStack"
          :font-size="preferences.fontSize"
          :line-height="preferences.lineHeight"
          :content-width="preferences.contentWidth"
          :capture-enabled="progress.captureEnabled.value"
          :reduced-motion="reducedMotion === 'reduce'"
          @position="onPosition"
          @active="onActive"
        />
        <ReaderPagedView
          v-else
          ref="pagedReaderView"
          :title="story.story.value.title"
          :author="story.story.value.author"
          :language="story.story.value.language"
          :story-slug="story.story.value.slug"
          :story-version="story.story.value.version"
          :segments="story.story.value.segments"
          :font-family="fontStack"
          :font-size="preferences.fontSize"
          :line-height="preferences.lineHeight"
          :content-width="preferences.contentWidth"
          :capture-enabled="progress.captureEnabled.value"
          :reduced-motion="reducedMotion === 'reduce'"
          :keyboard-enabled="!settingsOpen && !chaptersOpen && !resumeOpen"
          @position="onPosition"
          @active="onActive"
        />
      </main>

      <ReaderSettingsDialog
        v-model:open="settingsOpen"
        :model-value="settingsPreferences"
        @update:model-value="updatePreferences"
        @mode-change="changeMode"
        @reset="resetPreferences"
      />
      <ReaderChaptersDialog
        v-model:open="chaptersOpen"
        :chapters="chapters"
        :current-chapter="chapter"
        @select="selectChapter"
      />
      <ReaderResumeDialog
        :open="resumeOpen"
        :kind="resumeKind"
        :percent="resumePercent"
        @update:open="closeResume"
        @resume="resumeCurrentVersion"
        @start-over="startCurrentVersion"
        @library="progress.goLibrary"
        @dismiss="progress.dismissDecision"
      />

      <p class="reader-sr-only" role="status" aria-live="polite" aria-atomic="true">
        {{ progress.announcement.value }}
      </p>
    </template>
  </div>
</template>
