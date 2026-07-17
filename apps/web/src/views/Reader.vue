<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, nextTick, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  getReaderStory,
  saveProgress,
  getProgress,
  getAPIErrorStatus,
  type ProgressState,
} from '../lib/api'
import {
  capturePagedReaderLocator,
  captureScrollReaderLocator,
  createReaderLocatorV2,
  findReaderSegment,
  settleProgrammaticReaderRestore,
  type ReaderLocatorV2,
  type ReaderSegmentLayout,
  type ReaderStorySegment,
} from '../lib/reader-locator-v2'
import { loadPrefs, savePrefs, type ReaderPrefs } from '../lib/prefs'
import { haptic } from '../lib/haptics'
import { authState } from '../lib/session'
import { safeNextPath } from '../lib/session-navigation'
import { navigationDidFail } from '../lib/session-transitions'
import {
  createProgressBaselineController,
  planProgressBaselineCoordinatorRecovery,
  type ProgressBaselineController,
  type ProgressBaselineState,
} from '../lib/progress-baseline-controller'
import {
  createProgressSaveCoordinator,
  progressSnapshotsDiffer,
  type ProgressSaveCoordinator,
  type ProgressSaveState,
  type ProgressSnapshot,
} from '../lib/progress-save-coordinator'

type Page = {
  index: number
  startOrdinal: number
  endOrdinal: number
  segments: ReaderStorySegment[]
}

type Chapter = {
  title: string
  segmentOrdinal: number
  pageIndex: number
}

function clamp01(n: number) {
  if (!Number.isFinite(n)) return 0
  return Math.max(0, Math.min(1, n))
}

function pct(n: number) {
  return Math.max(0, Math.min(100, Math.round(clamp01(n) * 100)))
}

function scrollToY(y: number) {
  window.scrollTo(0, Math.max(0, y || 0))
}

function calcPercentScroll(): number {
  const el = document.documentElement
  const scrollTop = el.scrollTop || document.body.scrollTop
  const scrollHeight = el.scrollHeight - el.clientHeight
  if (scrollHeight <= 0) return 0
  return clamp01(scrollTop / scrollHeight)
}

// Temporary page construction remains intentionally simple in this backend
// contract PR. It uses the already-loaded coherent segment payload.
function buildPages(segments: ReaderStorySegment[]): Page[] {
  const out: Page[] = []
  let buf: ReaderStorySegment[] = []

  const flush = () => {
    const first = buf[0]
    const last = buf.at(-1)
    if (!first || !last) return

    out.push({
      index: out.length,
      startOrdinal: first.ordinal,
      endOrdinal: last.ordinal,
      segments: [...buf],
    })
    buf = []
  }

  for (const s of segments) {
    buf.push(s)
    if (buf.length >= 2) flush()
  }
  flush()

  if (!out.length) {
    out.push({ index: 0, startOrdinal: 0, endOrdinal: 0, segments: [] })
  }
  return out.map((p, idx) => ({ ...p, index: idx }))
}

function extractHeadingTextFromHTML(renderedHtml: string): string {
  try {
    const parsed = new DOMParser().parseFromString(renderedHtml, 'text/html')
    const h2 = parsed.querySelector('h2')
    return (h2?.textContent ?? '').trim()
  } catch {
    return ''
  }
}

const route = useRoute()
const router = useRouter()
let slug = String(route.params.slug)

const title = ref('')
const author = ref('')
const version = ref(1)
const storyLoading = ref(false)
const loadError = ref('')

const segments = ref<ReaderStorySegment[]>([])
const pages = computed<Page[]>(() => buildPages(segments.value))

const chapters = computed<Chapter[]>(() => {
  if (!segments.value.length || !pages.value.length) return []

  const out: Chapter[] = []
  for (const s of segments.value) {
    if (s.kind !== 'heading' || s.headingLevel !== 2) continue

    const chapterTitle = extractHeadingTextFromHTML(s.renderedHtml) || 'Chapter'
    const segmentOrdinal = s.ordinal

    const pageIndex = pages.value.findIndex(
      (page) =>
        segmentOrdinal >= page.startOrdinal &&
        segmentOrdinal <= page.endOrdinal,
    )

    out.push({
      title: chapterTitle,
      segmentOrdinal,
      pageIndex: Math.max(0, pageIndex),
    })
  }
  // de-dupe consecutive duplicates (some sources repeat headings)
  const dedup: Chapter[] = []
  for (const c of out) {
    const last = dedup[dedup.length - 1]
    if (last && last.pageIndex === c.pageIndex && last.title === c.title) continue
    dedup.push(c)
  }
  return dedup
})

const pagedRef = ref<HTMLElement | null>(null)
const currentPage = ref(0)

const percent = ref(0)
const progressSaveState = ref<ProgressSaveState>({
  status: 'idle',
  desired: null,
  confirmed: null,
  error: null,
})
const progressBaselineState = ref<ProgressBaselineState<ProgressState | null>>({
  status: 'loading',
  value: null,
  error: null,
  attempt: 0,
})
const leaveAfterSaveFailure = ref(false)
const navigatingToLibrary = ref(false)

const prefs = ref<ReaderPrefs>(loadPrefs())
watch(prefs, (p) => savePrefs(p), { deep: true })

const showControls = ref(false)
const showChapters = ref(false)

const resumeToast = ref<{
  locator: ReaderLocatorV2
  percent: number
} | null>(null)

const themeBg = computed(() => (prefs.value.theme === 'warm' ? '#0F1413' : '#0B1724'))
const themeText = computed(() => 'rgba(255,255,255,0.92)')

let progressCoordinator: ProgressSaveCoordinator | null = null
let unsubscribeProgress: (() => void) | null = null
let progressBaselineController: ProgressBaselineController<
  ProgressState | null
> | null = null
let unsubscribeProgressBaseline: (() => void) | null = null
let progressBaselineOrigin: ProgressSnapshot | null = null
let movedWhileProgressUnavailable = false
let storyGeneration = 0
let handlingProgressSessionLoss = false
let suppressProgressCapture = false

const saveStatusText = computed(() => {
  if (progressBaselineState.value.status !== 'ready') return ''
  switch (progressSaveState.value.status) {
    case 'dirty':
      return 'Unsaved'
    case 'saving':
      return 'Saving…'
    case 'saved':
      return 'Saved'
    case 'error':
      return 'Save failed'
    default:
      return ''
  }
})
const progressSaveActive = computed(
  () => progressSaveState.value.status === 'saving'
)
const progressBaselineChecking = computed(
  () =>
    progressBaselineState.value.status === 'loading' &&
    progressBaselineState.value.attempt > 1
)
const progressStatusText = computed(() => {
  if (progressBaselineChecking.value) return 'Checking progress…'
  if (progressBaselineState.value.status === 'unavailable') {
    return 'Progress unavailable'
  }
  return saveStatusText.value
})
const libraryButtonText = computed(() =>
  navigatingToLibrary.value && progressBaselineState.value.status === 'ready'
    ? 'Saving…'
    : '← Library'
)

function updatePercent() {
  if (prefs.value.mode === 'paged') {
    const n = pages.value.length
    if (n <= 1) percent.value = 0
    else percent.value = clamp01(currentPage.value / (n - 1))
  } else {
    percent.value = calcPercentScroll()
  }
}

function scrollSegmentLayouts(): ReaderSegmentLayout[] {
  return Array.from(
    document.querySelectorAll<HTMLElement>('[data-reader-scroll-segment]'),
  ).flatMap((element) => {
    const ordinal = Number(element.dataset.readerSegmentOrdinal)
    if (!Number.isInteger(ordinal) || ordinal < 1) return []
    const rect = element.getBoundingClientRect()
    return [{ ordinal, top: rect.top, bottom: rect.bottom }]
  })
}

function captureProgressSnapshot(storySlug = slug): ProgressSnapshot | null {
  if (prefs.value.mode === 'paged') {
    const n = pages.value.length
    const p = n <= 1 ? 0 : clamp01(currentPage.value / (n - 1))
    const page = pages.value[currentPage.value]
    if (!page) return null
    const locator = capturePagedReaderLocator(
      segments.value,
      page.startOrdinal,
    )
    if (!locator) return null
    return {
      slug: storySlug,
      version: version.value,
      locator,
      percent: p,
    }
  }

  const locator = captureScrollReaderLocator(
    segments.value,
    scrollSegmentLayouts(),
    window.innerHeight,
  )
  if (!locator) return null
  const p = calcPercentScroll()
  return {
    slug: storySlug,
    version: version.value,
    locator,
    percent: p,
  }
}

function scheduleSave() {
  if (suppressProgressCapture) return
  updatePercent()
  const snapshot = captureProgressSnapshot()
  if (progressBaselineState.value.status !== 'ready') {
    if (progressSnapshotsDiffer(snapshot, progressBaselineOrigin)) {
      movedWhileProgressUnavailable = true
    }
    return
  }
  if (resumeToast.value) return
  if (snapshot) progressCoordinator?.update(snapshot)
}

function disposeProgressCoordinator() {
  unsubscribeProgress?.()
  unsubscribeProgress = null
  progressCoordinator?.dispose()
  progressCoordinator = null
}

function disposeProgressBaselineController() {
  unsubscribeProgressBaseline?.()
  unsubscribeProgressBaseline = null
  progressBaselineController?.dispose()
  progressBaselineController = null
}

async function moveToUnlockAfterProgressSessionLoss(
  generation: number,
  storySlug: string
) {
  if (handlingProgressSessionLoss || generation !== storyGeneration) return
  handlingProgressSessionLoss = true
  authState.confirmLocked()
  leaveAfterSaveFailure.value = false

  const next = safeNextPath(`/read/${encodeURIComponent(storySlug)}`)
  try {
    const result = await router.replace({ path: '/unlock', query: { next } })
    if (navigationDidFail(result) && generation === storyGeneration) {
      loadError.value =
        'The session ended, but the passcode screen could not be opened. Reload to continue.'
    }
  } catch {
    if (generation === storyGeneration) {
      loadError.value =
        'The session ended, but the passcode screen could not be opened. Reload to continue.'
    }
  }
}

function createProgressCoordinator(generation: number, storySlug: string) {
  disposeProgressCoordinator()
  const coordinator = createProgressSaveCoordinator({
    persist: (snapshot, options) =>
      saveProgress(
        snapshot.slug,
        snapshot.version,
        snapshot.locator,
        snapshot.percent,
        options
      ),
    debounceMs: 450,
    setTimer: (callback, delayMs) => window.setTimeout(callback, delayMs),
    clearTimer: (handle) => window.clearTimeout(handle),
  })
  progressCoordinator = coordinator
  unsubscribeProgress = coordinator.subscribe((state) => {
    if (generation !== storyGeneration) return
    progressSaveState.value = state
    if (
      state.status === 'error' &&
      getAPIErrorStatus(state.error) === 401
    ) {
      void moveToUnlockAfterProgressSessionLoss(generation, storySlug)
    }
  })
  return coordinator
}

async function retryProgressSave() {
  try {
    await progressCoordinator?.retry()
  } catch {
    // The coordinator keeps the latest snapshot and exposes the error state.
  }
}

function restoreReaderLocator(locator: ReaderLocatorV2) {
  const segment = findReaderSegment(segments.value, locator)
  if (!segment) return

  if (prefs.value.mode === 'paged') {
    const pageIndex = pages.value.findIndex(
      (page) =>
        segment.ordinal >= page.startOrdinal &&
        segment.ordinal <= page.endOrdinal,
    )
    const safePage = Math.max(0, pageIndex)
    currentPage.value = safePage
    const element = pagedRef.value
    element?.scrollTo({
      left: safePage * element.clientWidth,
      behavior: 'auto',
    })
    return
  }

  const element = document.querySelector<HTMLElement>(
    `[data-reader-scroll-segment][data-reader-segment-ordinal="${segment.ordinal}"]`,
  )
  if (!element) return
  const rect = element.getBoundingClientRect()
  const readingLine = window.innerHeight * 0.35
  const target =
    window.scrollY +
    rect.top +
    rect.height * locator.segment.offset -
    readingLine
  scrollToY(target)
}

function doResume() {
  const t = resumeToast.value
  resumeToast.value = null
  if (!t) return

  requestAnimationFrame(() => {
    restoreReaderLocator(t.locator)
    requestAnimationFrame(() => restoreReaderLocator(t.locator))
  })
}

async function startOver() {
  resumeToast.value = null
  const firstSegment = segments.value[0]
  if (!firstSegment) return
  const locator = createReaderLocatorV2(firstSegment, 0)

  if (prefs.value.mode === 'paged') {
    currentPage.value = 0
    percent.value = 0
    const el = pagedRef.value
    el?.scrollTo({ left: 0, behavior: 'auto' })
  } else {
    scrollToY(0)
    percent.value = 0
  }

  const snapshot: ProgressSnapshot = {
    slug,
    version: version.value,
    locator,
    percent: 0,
  }
  if (progressBaselineState.value.status !== 'ready') {
    movedWhileProgressUnavailable = true
    return
  }
  if (!snapshot || !progressCoordinator) return
  progressCoordinator.update(snapshot, { force: true, debounce: false })
  try {
    await progressCoordinator.flush()
  } catch {
    // The normal save-error state remains visible and retryable.
  }
}

function findAgain() {
  resumeToast.value = null
  void router.push({ path: '/library', query: { q: slug } })
}

function dismissResume() {
  resumeToast.value = null
}

async function goLibrary() {
  if (navigatingToLibrary.value) return

  leaveAfterSaveFailure.value = false
  if (progressBaselineState.value.status !== 'ready') {
    navigatingToLibrary.value = true
    try {
      await router.push('/library')
    } catch {
      loadError.value = 'The Library could not be opened. Try again.'
    } finally {
      navigatingToLibrary.value = false
    }
    return
  }

  const coordinator = progressCoordinator
  const snapshot = resumeToast.value ? null : captureProgressSnapshot()
  if (coordinator && snapshot) {
    coordinator.update(snapshot, { debounce: false })
  }

  navigatingToLibrary.value = true
  try {
    await coordinator?.flush()
    await router.push('/library')
  } catch (error) {
    if (getAPIErrorStatus(error) !== 401) {
      leaveAfterSaveFailure.value = true
    }
  } finally {
    navigatingToLibrary.value = false
  }
}

function leaveReaderAnyway() {
  leaveAfterSaveFailure.value = false
  void router.push('/library')
}

function toggleControls() {
  showControls.value = !showControls.value
}

function closeControls() {
  showControls.value = false
}

function toggleChapters() {
  if (prefs.value.mode !== 'paged') return
  if (!chapters.value.length) return
  showChapters.value = !showChapters.value
}

function closeChapters() {
  showChapters.value = false
}

function jumpToChapter(c: Chapter) {
  closeChapters()
  const idx = Math.max(0, Math.min(pages.value.length - 1, c.pageIndex))
  currentPage.value = idx
  requestAnimationFrame(() => {
    const el = pagedRef.value
    if (!el) return
    el.scrollTo({ left: idx * el.clientWidth, behavior: 'auto' })
    requestAnimationFrame(() => el.scrollTo({ left: idx * el.clientWidth, behavior: 'auto' }))
  })
  scheduleSave()
}

async function setMode(mode: 'scroll' | 'paged') {
  if (prefs.value.mode === mode) return
  const anchor = captureProgressSnapshot()?.locator ?? null

  suppressProgressCapture = true
  showChapters.value = false
  prefs.value.mode = mode
  try {
    await nextTick()
    if (anchor) {
      // Programmatic scroll events from the second restore can be delivered on
      // the following frame. Keep capture suppressed until they have settled,
      // then publish the preserved anchor exactly once below.
      await settleProgrammaticReaderRestore(() => restoreReaderLocator(anchor))
    }
    updatePercent()
  } finally {
    suppressProgressCapture = false
  }

  if (!anchor || resumeToast.value) return
  const snapshot: ProgressSnapshot = {
    slug,
    version: version.value,
    locator: anchor,
    percent: percent.value,
  }
  if (progressBaselineState.value.status !== 'ready') {
    if (progressSnapshotsDiffer(snapshot, progressBaselineOrigin)) {
      movedWhileProgressUnavailable = true
    }
    return
  }
  progressCoordinator?.update(snapshot)
}

function setTheme(theme: 'night' | 'warm') {
  if (prefs.value.theme === theme) return
  prefs.value.theme = theme
}

function onPagedScroll() {
  const el = pagedRef.value
  if (!el) return
  const w = el.clientWidth || 1
  const idx = Math.round(el.scrollLeft / w)
  const clamped = Math.max(0, Math.min(pages.value.length - 1, idx))
  if (clamped !== currentPage.value) currentPage.value = clamped
  scheduleSave()
}

function progressSnapshotFromServer(
  progress: ProgressState | null,
  storySlug: string
): ProgressSnapshot | null {
  if (!progress || progress.version !== version.value) return null
  return {
    slug: storySlug,
    version: progress.version,
    locator: progress.locator,
    percent: clamp01(progress.percent),
  }
}

function showResumeOffer(progress: ProgressState | null) {
  resumeToast.value = null
  if (!progress || progress.version !== version.value) return
  if (!findReaderSegment(segments.value, progress.locator)) return
  const savedPercent = clamp01(progress.percent)
  if (
    progress.locator.segment.ordinal === 1 &&
    progress.locator.segment.offset <= 0.02 &&
    savedPercent <= 0.02
  ) {
    return
  }
  resumeToast.value = {
    locator: progress.locator,
    percent: savedPercent,
  }
}

function initializeProgressFromBaseline(
  progress: ProgressState | null,
  attempt: number,
  generation: number,
  storySlug: string
): void {
  if (
    generation !== storyGeneration ||
    handlingProgressSessionLoss ||
    progressCoordinator
  ) {
    return
  }

  const isRetry = attempt > 1
  if (!isRetry || !movedWhileProgressUnavailable) {
    showResumeOffer(progress)
  } else {
    resumeToast.value = null
  }

  updatePercent()
  const current = captureProgressSnapshot(storySlug)
  const confirmed = progressSnapshotFromServer(progress, storySlug)
  const coordinator = createProgressCoordinator(generation, storySlug)

  const recovery = planProgressBaselineCoordinatorRecovery({
    confirmed,
    current,
    retriedAfterUnavailableMovement: isRetry && movedWhileProgressUnavailable,
  })
  coordinator.initialize(recovery.initialConfirmed, recovery.initialDesired)

  if (recovery.updateDesired) {
    coordinator.update(
      recovery.updateDesired,
      recovery.forceUpdate ? { force: true } : undefined
    )
  }
  movedWhileProgressUnavailable = false
}

function beginProgressBaselineLoad(generation: number, storySlug: string) {
  disposeProgressBaselineController()
  progressBaselineOrigin = captureProgressSnapshot(storySlug)
  movedWhileProgressUnavailable = false

  const controller = createProgressBaselineController({
    load: async () => (await getProgress(storySlug)).progress,
  })
  progressBaselineController = controller
  unsubscribeProgressBaseline = controller.subscribe((state) => {
    if (generation !== storyGeneration) return
    progressBaselineState.value = state

    if (state.status === 'ready') {
      initializeProgressFromBaseline(
        state.value,
        state.attempt,
        generation,
        storySlug
      )
      return
    }

    if (
      state.status === 'unavailable' &&
      getAPIErrorStatus(state.error) === 401
    ) {
      void moveToUnlockAfterProgressSessionLoss(generation, storySlug)
    }
  })
  void controller.load()
}

function retryProgressBaseline() {
  void progressBaselineController?.retry()
}

async function load() {
  if (storyLoading.value) return
  const generation = storyGeneration
  const storySlug = slug
  storyLoading.value = true
  loadError.value = ''

  try {
    const s = await getReaderStory(storySlug)
    if (generation !== storyGeneration) return
    title.value = s.title
    author.value = s.author || ''
    version.value = s.version
    segments.value = s.segments
    currentPage.value = 0

    await nextTick()
    if (generation !== storyGeneration) return
    updatePercent()
    beginProgressBaselineLoad(generation, storySlug)
  } catch (error) {
    if (generation !== storyGeneration) return
    if (getAPIErrorStatus(error) === 401) {
      title.value = ''
      author.value = ''
      segments.value = []
      await moveToUnlockAfterProgressSessionLoss(generation, storySlug)
      return
    }

    loadError.value = 'Could not load this story. Try again.'
  } finally {
    if (generation === storyGeneration) storyLoading.value = false
  }
}

function onPageHide() {
  if (
    progressBaselineState.value.status !== 'ready' ||
    handlingProgressSessionLoss ||
    resumeToast.value
  ) {
    return
  }
  const snapshot = captureProgressSnapshot()
  if (snapshot) {
    progressCoordinator?.update(snapshot, { debounce: false })
  }
  void progressCoordinator?.bestEffortKeepaliveFlush().catch(() => {
    // Browsers may terminate before a keepalive response is observed.
  })
}

function onVisibilityChange() {
  if (document.visibilityState === 'hidden') onPageHide()
}

onMounted(() => {
  void load()
  window.addEventListener('scroll', scheduleSave, { passive: true })
  window.addEventListener('pagehide', onPageHide)
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  onPageHide()
  window.removeEventListener('scroll', scheduleSave)
  window.removeEventListener('pagehide', onPageHide)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  disposeProgressBaselineController()
  disposeProgressCoordinator()
})

watch(
  () => String(route.params.slug),
  (nextSlug) => {
    if (nextSlug === slug) return
    onPageHide()
    disposeProgressBaselineController()
    disposeProgressCoordinator()
    storyGeneration += 1
    slug = nextSlug
    handlingProgressSessionLoss = false
    storyLoading.value = false
    loadError.value = ''
    title.value = ''
    author.value = ''
    segments.value = []
    currentPage.value = 0
    resumeToast.value = null
    leaveAfterSaveFailure.value = false
    progressBaselineOrigin = null
    movedWhileProgressUnavailable = false
    progressBaselineState.value = {
      status: 'loading',
      value: null,
      error: null,
      attempt: 0,
    }
    progressSaveState.value = {
      status: 'idle',
      desired: null,
      confirmed: null,
      error: null,
    }
    void load()
  }
)
</script>

<template>
  <div class="min-h-dvh" :style="{ background: themeBg, color: themeText }">
    <!-- top progress bar -->
    <div class="fixed top-0 left-0 right-0 z-20 h-0.5 bg-white/10">
      <div class="h-0.5 bg-white/60" :style="{ width: `${Math.round(percent * 100)}%` }"></div>
    </div>

    <header class="sticky top-0 z-10 backdrop-blur border-b border-white/10 bg-black/35">
      <div
        class="max-w-5xl mx-auto px-4 py-3 pt-[calc(0.75rem+env(safe-area-inset-top))] flex items-center justify-between gap-3"
      >
        <button
          class="text-sm opacity-85 hover:opacity-100 disabled:opacity-50"
          :disabled="navigatingToLibrary"
          @pointerdown="haptic('select')"
          @click="goLibrary"
        >
          {{ libraryButtonText }}
        </button>

        <div class="flex items-center gap-3">
          <div class="text-xs opacity-70">
            <span v-if="prefs.mode === 'paged' && pages.length">{{ currentPage + 1 }}/{{ pages.length }}</span>
            <span v-else>{{ Math.round(percent * 100) }}%</span>
          </div>

          <div
            class="flex min-w-20 items-center justify-end gap-2 text-xs opacity-80"
          >
            <span role="status" aria-live="polite" aria-atomic="true">
              {{ progressStatusText }}
            </span>
            <button
              v-if="progressBaselineState.status === 'unavailable' || progressBaselineChecking"
              type="button"
              class="rounded-md border border-white/20 px-2 py-1 font-medium opacity-100"
              :disabled="progressBaselineChecking"
              @click="retryProgressBaseline"
            >
              Retry
            </button>
            <button
              v-else-if="progressBaselineState.status === 'ready' && progressSaveState.status === 'error'"
              type="button"
              class="rounded-md border border-white/20 px-2 py-1 font-medium opacity-100"
              :disabled="progressSaveActive"
              @click="retryProgressSave"
            >
              Retry
            </button>
          </div>

          <button
            v-if="prefs.mode === 'paged' && chapters.length"
            class="text-sm rounded-lg border border-white/15 bg-white/5 px-3 py-1.5 hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="toggleChapters"
          >
            Chapters
          </button>

          <button
            class="text-sm rounded-lg border border-white/15 bg-white/5 px-3 py-1.5 hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="toggleControls"
          >
            Aa
          </button>
        </div>
      </div>
    </header>

    <aside
      v-if="progressBaselineState.status === 'ready' && leaveAfterSaveFailure"
      class="mx-auto mt-3 flex max-w-3xl items-center justify-between gap-3 rounded-xl border border-amber-200/30 bg-amber-200/10 px-4 py-3 text-sm"
      role="alert"
    >
      <p>Progress could not be saved.</p>
      <div class="flex shrink-0 gap-2">
        <button
          type="button"
          class="rounded-lg bg-white px-3 py-2 font-medium text-[#0B1724] disabled:opacity-60"
          :disabled="navigatingToLibrary || progressSaveState.status === 'saving'"
          @click="goLibrary"
        >
          Retry
        </button>
        <button
          type="button"
          class="rounded-lg border border-white/20 px-3 py-2"
          @click="leaveReaderAnyway"
        >
          Leave anyway
        </button>
      </div>
    </aside>

    <!-- Chapter drawer -->
    <div v-if="showChapters" class="fixed inset-0 z-30" @click.self="closeChapters">
      <div class="absolute inset-x-0 bottom-0 rounded-t-2xl border border-white/10 bg-[#0b1724]/95 p-5 backdrop-blur
                  pb-[calc(1.25rem+env(safe-area-inset-bottom))]">
        <div class="flex items-center justify-between">
          <div class="text-sm font-semibold">Chapters</div>
          <button class="text-sm opacity-80" @pointerdown="haptic('select')" @click="closeChapters">
            Close
          </button>
        </div>

        <div class="mt-3 max-h-[50vh] overflow-auto pr-1">
          <button
            v-for="c in chapters"
            :key="`${c.segmentOrdinal}-${c.pageIndex}`"
            class="w-full text-left rounded-xl border border-white/10 bg-white/5 px-4 py-3 mb-2 hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="jumpToChapter(c)"
          >
            <div class="text-sm font-medium">{{ c.title }}</div>
            <div class="text-xs opacity-70">Page {{ c.pageIndex + 1 }}</div>
          </button>
        </div>
      </div>
    </div>

    <!-- Resume toast -->
    <div v-if="resumeToast" class="fixed left-0 right-0 bottom-4 z-40 px-4 pb-[env(safe-area-inset-bottom)]">
      <div class="mx-auto max-w-xl rounded-2xl border border-white/10 bg-black/70 backdrop-blur p-4">
        <div class="flex items-center justify-between gap-3">
          <div class="text-sm font-medium">Resume at {{ pct(resumeToast.percent) }}%?</div>
          <div class="text-xs opacity-70 truncate">{{ title }}</div>
        </div>

        <div class="mt-3 flex gap-2">
          <button
            class="flex-1 rounded-xl bg-white text-black py-2 font-medium active:scale-[0.99] transition"
            @pointerdown="haptic('medium')"
            @click="doResume"
          >
            Resume
          </button>

          <button
            class="flex-1 rounded-xl bg-white/10 border border-white/15 py-2 active:scale-[0.99] transition"
            @pointerdown="haptic('heavy')"
            @click="startOver"
          >
            Start over
          </button>
        </div>

        <div class="mt-2 flex gap-2">
          <button
            class="flex-1 rounded-xl bg-white/5 border border-white/10 py-2 text-sm active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="findAgain"
          >
            Find it again
          </button>

          <button
            class="px-4 rounded-xl bg-white/5 border border-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="dismissResume"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>

    <!-- Reader container -->
    <main class="mx-auto px-4 py-8" :style="{ maxWidth: `${prefs.widthPx}px` }">
      <div
        v-if="loadError"
        class="mb-6 rounded-2xl border border-red-300/30 bg-red-300/10 p-4 text-sm"
        role="alert"
      >
        <p>{{ loadError }}</p>
        <button
          type="button"
          class="mt-3 rounded-xl bg-white px-4 py-2 font-medium text-[#0B1724] disabled:opacity-60"
          :disabled="storyLoading"
          @click="load"
        >
          {{ storyLoading ? 'Loading…' : 'Try again' }}
        </button>
      </div>
      <p v-else-if="storyLoading && !title" class="text-sm opacity-75" aria-live="polite">
        Loading story…
      </p>

      <h1 class="text-3xl md:text-4xl font-semibold leading-tight">{{ title }}</h1>
      <p v-if="author" class="mt-2 opacity-75">{{ author }}</p>

      <!-- Scroll mode -->
      <section
        v-if="prefs.mode !== 'paged'"
        class="mt-8"
        :style="{ fontSize: `${prefs.fontPx}px`, lineHeight: String(prefs.lineHeight) }"
      >
        <article class="reader">
          <div
            v-for="segment in segments"
            :key="`${segment.contentKey}-${segment.contentOccurrence}`"
            data-reader-scroll-segment
            :data-reader-segment-ordinal="segment.ordinal"
            :data-reader-content-key="segment.contentKey"
            :data-reader-content-occurrence="segment.contentOccurrence"
            v-html="segment.renderedHtml"
          ></div>
        </article>
      </section>

      <!-- Paged mode -->
      <section
        v-else
        ref="pagedRef"
        class="mt-8 paged"
        :style="{ fontSize: `${prefs.fontPx}px`, lineHeight: String(prefs.lineHeight) }"
        @scroll.passive="onPagedScroll"
      >
        <div v-for="p in pages" :key="p.index" class="page">
          <article class="reader">
            <div
              v-for="segment in p.segments"
              :key="`${segment.contentKey}-${segment.contentOccurrence}`"
              :data-reader-segment-ordinal="segment.ordinal"
              :data-reader-content-key="segment.contentKey"
              :data-reader-content-occurrence="segment.contentOccurrence"
              v-html="segment.renderedHtml"
            ></div>
            <p v-if="!p.segments.length">No content.</p>
          </article>
        </div>

        <div v-if="!pages.length" class="page">
          <article class="reader"><p>Loading pages…</p></article>
        </div>
      </section>
    </main>

    <!-- Controls drawer -->
    <div v-if="showControls" class="fixed inset-0 z-30" @click.self="closeControls">
      <div
        class="absolute inset-x-0 bottom-0 rounded-t-2xl border border-white/10 bg-[#0b1724]/95 p-5 backdrop-blur
               pb-[calc(1.25rem+env(safe-area-inset-bottom))]"
      >
        <div class="flex items-center justify-between">
          <div class="text-sm font-semibold">Reading settings</div>
          <button class="text-sm opacity-80" @pointerdown="haptic('select')" @click="closeControls">
            Close
          </button>
        </div>

        <div class="mt-4 grid grid-cols-2 md:grid-cols-4 gap-3">
          <label class="text-xs opacity-80">Text size
            <input class="mt-2 w-full" type="range" min="16" max="28" step="1" v-model.number="prefs.fontPx" />
          </label>

          <label class="text-xs opacity-80">Line height
            <input class="mt-2 w-full" type="range" min="1.4" max="2.0" step="0.05" v-model.number="prefs.lineHeight" />
          </label>

          <label class="text-xs opacity-80">Width
            <input class="mt-2 w-full" type="range" min="520" max="920" step="20" v-model.number="prefs.widthPx" />
          </label>

          <div class="text-xs opacity-80">
            Mode
            <div class="mt-2 flex gap-2">
              <button
                class="flex-1 rounded-lg border border-white/15 px-3 py-2 active:scale-[0.99] transition"
                :class="prefs.mode==='scroll' ? 'bg-white text-black' : 'bg-white/5'"
                @pointerdown="haptic('select')"
                @click="setMode('scroll')"
              >
                Scroll
              </button>
              <button
                class="flex-1 rounded-lg border border-white/15 px-3 py-2 active:scale-[0.99] transition"
                :class="prefs.mode==='paged' ? 'bg-white text-black' : 'bg-white/5'"
                @pointerdown="haptic('select')"
                @click="setMode('paged')"
              >
                Paged
              </button>
            </div>
            <div class="mt-2 text-[11px] opacity-70">
              Paged uses story segments (true page-swipe + progress saving).
            </div>
          </div>

          <div class="col-span-2 md:col-span-4 text-xs opacity-80">
            Theme
            <div class="mt-2 flex gap-2">
              <button
                class="flex-1 rounded-lg border border-white/15 px-3 py-2 active:scale-[0.99] transition"
                :class="prefs.theme==='night' ? 'bg-white text-black' : 'bg-white/5'"
                @pointerdown="haptic('select')"
                @click="setTheme('night')"
              >
                Night
              </button>
              <button
                class="flex-1 rounded-lg border border-white/15 px-3 py-2 active:scale-[0.99] transition"
                :class="prefs.theme==='warm' ? 'bg-white text-black' : 'bg-white/5'"
                @pointerdown="haptic('select')"
                @click="setTheme('warm')"
              >
                Warm
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

  </div>
</template>

<style scoped>
.reader :deep(h1) { font-size: 1.6em; margin: 1.2em 0 0.6em; }
.reader :deep(h2) { font-size: 1.25em; margin: 1.2em 0 0.6em; }
.reader :deep(p)  { margin: 0.9em 0; opacity: 0.94; }
.reader :deep(strong) { opacity: 1; }
.reader :deep(ul), .reader :deep(ol) { margin: 0.8em 0 0.8em 1.2em; }
.reader :deep(li) { margin: 0.35em 0; }

.paged {
  display: flex;
  overflow-x: auto;
  overflow-y: hidden;
  scroll-snap-type: x mandatory;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
  gap: 0;
}
.paged::-webkit-scrollbar { display: none; }

.page {
  scroll-snap-align: start;
  flex: 0 0 100%;
  padding-right: 16px;
}
</style>
