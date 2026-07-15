<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref, nextTick, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  getStory,
  getStorySegments,
  saveProgress,
  getProgress,
  getAPIErrorStatus,
  isJsonObject,
  type JsonObject,
  type ProgressState,
  type StorySegment,
} from '../lib/api'
import { loadPrefs, savePrefs, type ReaderPrefs } from '../lib/prefs'
import { haptic } from '../lib/haptics'
import { authState } from '../lib/session'
import { safeNextPath } from '../lib/session-navigation'
import { navigationDidFail } from '../lib/session-transitions'
import {
  createProgressSaveCoordinator,
  type ProgressSaveCoordinator,
  type ProgressSaveState,
  type ProgressSnapshot,
} from '../lib/progress-save-coordinator'

type Page = {
  index: number
  startOrdinal: number
  endOrdinal: number
  html: string
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

function asLocator(value: unknown): JsonObject {
  if (typeof value !== 'string') {
    return isJsonObject(value) ? value : {}
  }

  try {
    const parsed: unknown = JSON.parse(value)
    return isJsonObject(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function calcPercentScroll(): number {
  const el = document.documentElement
  const scrollTop = el.scrollTop || document.body.scrollTop
  const scrollHeight = el.scrollHeight - el.clientHeight
  if (scrollHeight <= 0) return 0
  return clamp01(scrollTop / scrollHeight)
}

// Simple v1 paging rule: 1–2 segments per page
function buildPages(segments: StorySegment[]): Page[] {
  const out: Page[] = []
  let buf: StorySegment[] = []

  const flush = () => {
    const first = buf[0]
    const last = buf.at(-1)
    if (!first || !last) return

    out.push({
      index: out.length,
      startOrdinal: first.ordinal,
      endOrdinal: last.ordinal,
      html: buf.map((segment) => segment.renderedHtml).join(''),
    })
    buf = []
  }

  for (const s of segments) {
    buf.push(s)
    if (buf.length >= 2) flush()
  }
  flush()

  if (!out.length) out.push({ index: 0, startOrdinal: 0, endOrdinal: 0, html: '<p>No content.</p>' })
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
const html = ref('')
const version = ref(1)
const storyLoading = ref(false)
const loadError = ref('')

const segments = ref<StorySegment[] | null>(null)
const pages = computed<Page[]>(() => (segments.value ? buildPages(segments.value) : []))

// Chapters derived from segments (no backend change required)
const chapters = computed<Chapter[]>(() => {
  if (!segments.value?.length || !pages.value.length) return []

  const out: Chapter[] = []
  for (const s of segments.value) {
    const loc = asLocator(s.locator)
    const isH2 =
      (loc.type === 'heading' && Number(loc.h) === 2) ||
      /^\s*<h2[\s>]/i.test(s.renderedHtml)

    if (!isH2) continue

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
const leaveAfterSaveFailure = ref(false)
const navigatingToLibrary = ref(false)

const prefs = ref<ReaderPrefs>(loadPrefs())
watch(prefs, (p) => savePrefs(p), { deep: true })

const showControls = ref(false)
const showChapters = ref(false)

const resumeToast = ref<{ mode: 'scroll' | 'paged'; y?: number; page?: number; percent: number } | null>(null)

const themeBg = computed(() => (prefs.value.theme === 'warm' ? '#0F1413' : '#0B1724'))
const themeText = computed(() => 'rgba(255,255,255,0.92)')

let progressCoordinator: ProgressSaveCoordinator | null = null
let unsubscribeProgress: (() => void) | null = null
let storyGeneration = 0
let handlingProgressSessionLoss = false

const saveStatusText = computed(() => {
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

function updatePercent() {
  if (prefs.value.mode === 'paged') {
    const n = pages.value.length
    if (n <= 1) percent.value = 0
    else percent.value = clamp01(currentPage.value / (n - 1))
  } else {
    percent.value = calcPercentScroll()
  }
}

function captureProgressSnapshot(storySlug = slug): ProgressSnapshot | null {
  if (prefs.value.mode === 'paged') {
    const n = pages.value.length
    const p = n <= 1 ? 0 : clamp01(currentPage.value / (n - 1))
    const page = pages.value[currentPage.value]
    if (!page) return null
    return {
      slug: storySlug,
      version: version.value,
      locator: {
        mode: 'paged',
        page: currentPage.value,
        startOrdinal: page.startOrdinal,
        endOrdinal: page.endOrdinal,
      },
      percent: p,
    }
  }

  const y = window.scrollY || 0
  const p = calcPercentScroll()
  return {
    slug: storySlug,
    version: version.value,
    locator: { mode: 'scroll', scrollY: y },
    percent: p,
  }
}

function scheduleSave() {
  if (resumeToast.value) return
  updatePercent()
  const snapshot = captureProgressSnapshot()
  if (snapshot) progressCoordinator?.update(snapshot)
}

function disposeProgressCoordinator() {
  unsubscribeProgress?.()
  unsubscribeProgress = null
  progressCoordinator?.dispose()
  progressCoordinator = null
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

function doResume() {
  const t = resumeToast.value
  resumeToast.value = null
  if (!t) return

  if (t.mode === 'paged') {
    const idx = Math.max(0, Math.min(pages.value.length - 1, Number(t.page ?? 0)))
    currentPage.value = idx
    requestAnimationFrame(() => {
      const el = pagedRef.value
      if (!el) return
      el.scrollTo({ left: idx * el.clientWidth, behavior: 'auto' })
      requestAnimationFrame(() => el.scrollTo({ left: idx * el.clientWidth, behavior: 'auto' }))
    })
    return
  }

  const y = t.y || 0
  requestAnimationFrame(() => {
    scrollToY(y)
    requestAnimationFrame(() => scrollToY(y))
  })
}

async function startOver() {
  resumeToast.value = null
  if (prefs.value.mode === 'paged') {
    currentPage.value = 0
    percent.value = 0
    const el = pagedRef.value
    el?.scrollTo({ left: 0, behavior: 'auto' })
  } else {
    scrollToY(0)
    percent.value = 0
  }

  const snapshot = captureProgressSnapshot()
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

function setMode(mode: 'scroll' | 'paged') {
  if (prefs.value.mode === mode) return
  prefs.value.mode = mode
  showChapters.value = false

  if (mode === 'paged') {
    currentPage.value = 0
    percent.value = 0
    void nextTick(() => {
      pagedRef.value?.scrollTo({ left: 0, behavior: 'auto' })
      scheduleSave()
    })
  } else {
    void nextTick(() => {
      scrollToY(0)
      scheduleSave()
    })
  }
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

type ProgressLoadResult = {
  loaded: boolean
  progress: ProgressState | null
}

function progressSnapshotFromServer(
  progress: ProgressState | null,
  storySlug: string
): ProgressSnapshot | null {
  if (!progress || progress.version !== version.value) return null
  const locator = asLocator(progress.locator)
  if (!Object.keys(locator).length) return null
  return {
    slug: storySlug,
    version: progress.version,
    locator,
    percent: clamp01(progress.percent),
  }
}

async function checkResumeOffer(
  generation: number,
  storySlug: string
): Promise<ProgressLoadResult> {
  try {
    const p = await getProgress(storySlug)
    if (generation !== storyGeneration) {
      return { loaded: false, progress: null }
    }
    if (!p || !p.locator || p.version !== version.value) {
      return { loaded: true, progress: p ?? null }
    }

    const loc = asLocator(p.locator)
    const mode = loc.mode === 'paged' ? 'paged' : 'scroll'

    if (mode === 'paged' && prefs.value.mode === 'paged' && pages.value.length) {
      const page = typeof loc.page === 'number' ? loc.page : 0
      const count = pages.value.length
      const safePage = Math.max(
        0,
        Math.min(count - 1, Number.isFinite(page) ? page : 0),
      )
      const savedPercent = clamp01(p.percent)

      if (safePage > 0 || savedPercent > 0.02) {
        resumeToast.value = {
          mode: 'paged',
          page: safePage,
          percent: savedPercent,
        }
      }
      return { loaded: true, progress: p }
    }

    const scrollY = typeof loc.scrollY === 'number' ? loc.scrollY : 0
    if (!Number.isFinite(scrollY) || scrollY <= 64) {
      return { loaded: true, progress: p }
    }

    resumeToast.value = {
      mode: 'scroll',
      y: scrollY,
      percent: clamp01(p.percent),
    }
    return { loaded: true, progress: p }
  } catch (error) {
    if (getAPIErrorStatus(error) === 401) {
      void moveToUnlockAfterProgressSessionLoss(generation, storySlug)
    }
    return { loaded: false, progress: null }
  }
}

async function load() {
  if (storyLoading.value) return
  const generation = storyGeneration
  const storySlug = slug
  storyLoading.value = true
  loadError.value = ''

  try {
    const s = await getStory(storySlug)
    if (generation !== storyGeneration) return
    title.value = s.title
    author.value = s.author || ''
    html.value = s.renderedHtml
    version.value = s.version

    if (prefs.value.mode === 'paged') {
      const seg = await getStorySegments(storySlug)
      if (generation !== storyGeneration) return
      segments.value = Array.isArray(seg.segments) ? seg.segments : []
      currentPage.value = 0
    } else {
      segments.value = null
    }

    await nextTick()
    if (generation !== storyGeneration) return
    const progressResult = await checkResumeOffer(generation, storySlug)
    if (generation !== storyGeneration) return
    if (handlingProgressSessionLoss) return
    updatePercent()
    const coordinator = createProgressCoordinator(generation, storySlug)
    const confirmed = progressSnapshotFromServer(
      progressResult.progress,
      storySlug
    )
    const current = captureProgressSnapshot(storySlug)
    if (confirmed) {
      coordinator.initialize(confirmed)
    } else if (progressResult.loaded) {
      coordinator.initialize(current)
    } else {
      coordinator.initialize(null, current)
    }
  } catch (error) {
    if (generation !== storyGeneration) return
    if (getAPIErrorStatus(error) === 401) {
      title.value = ''
      author.value = ''
      html.value = ''
      segments.value = null
      await moveToUnlockAfterProgressSessionLoss(generation, storySlug)
      return
    }

    loadError.value = 'Could not load this story. Try again.'
  } finally {
    if (generation === storyGeneration) storyLoading.value = false
  }
}

function onPageHide() {
  if (handlingProgressSessionLoss || resumeToast.value) return
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
  disposeProgressCoordinator()
})

watch(
  () => String(route.params.slug),
  (nextSlug) => {
    if (nextSlug === slug) return
    onPageHide()
    disposeProgressCoordinator()
    storyGeneration += 1
    slug = nextSlug
    handlingProgressSessionLoss = false
    storyLoading.value = false
    loadError.value = ''
    title.value = ''
    author.value = ''
    html.value = ''
    segments.value = null
    currentPage.value = 0
    resumeToast.value = null
    leaveAfterSaveFailure.value = false
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
          {{ navigatingToLibrary ? 'Saving…' : '← Library' }}
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
              {{ saveStatusText }}
            </span>
            <button
              v-if="progressSaveState.status === 'error'"
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
      v-if="leaveAfterSaveFailure"
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
        <article class="reader" v-html="html"></article>
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
          <article class="reader" v-html="p.html"></article>
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
