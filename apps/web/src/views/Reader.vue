<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
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
  validateReaderPreferencesV2,
  type ReaderMode,
  type ReaderPreferencesV2,
} from '../lib/reader-preferences-v2'
import { authState } from '../lib/session'
import { safeNextPath } from '../lib/session-navigation'
import { navigationDidFail } from '../lib/session-transitions'

type ReaderView = {
  capture: () => ReaderCapturedPosition | null
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

const route = useRoute()
const router = useRouter()
const reducedMotion = usePreferredReducedMotion()
const slug = ref(String(route.params.slug))
const readerView = ref<ReaderView | null>(null)
const settingsOpen = ref(false)
const chaptersOpen = ref(false)
const activeOrdinal = ref(1)
const percent = ref(0)
const navigationMessage = ref('')
let preferenceGeneration = 0
let readerGeneration = 0

const {
  preferences,
  fontStack,
  reset: resetStoredPreferences,
} = useReaderPreferences()

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

function captureCurrent(): ReaderCapturedPosition | null {
  return readerView.value?.capture() ?? null
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

const story = useReaderStory({
  onSessionEnded: moveToUnlock,
  onReady: (loaded) => {
    activeOrdinal.value = loaded.segments[0]?.ordinal ?? 1
    percent.value = captureCurrent()?.percent ?? 0
    readerView.value?.focusContent()
    progress.begin(loaded.slug, loaded.version, loaded.segments)
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
const resumeOpen = computed(() => progress.decision.value !== null)
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
  const restored = await readerView.value?.restore(locator)
  if (activeGeneration !== readerGeneration) return false
  const current = captureCurrent()
  if (current) {
    percent.value = current.percent
    activeOrdinal.value = current.locator.segment.ordinal
  }
  return restored ?? false
}

async function moveToBeginning(): Promise<ReaderCapturedPosition | null> {
  const activeGeneration = readerGeneration
  const first = story.story.value?.segments[0]
  if (!first) return null
  const position = await readerView.value?.moveToOrdinal(
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

async function applyPreferences(candidate: ReaderPreferencesV2) {
  const validated = validateReaderPreferencesV2(candidate)
  if (!validated) return
  const activeGeneration = readerGeneration
  const transition = planReaderModeTransition(captureCurrent())
  const anchor = transition.anchor?.locator ?? null
  const operation = ++preferenceGeneration
  progress.captureSuppressed.value = true
  preferences.value = validated
  try {
    await nextTick()
    if (activeGeneration !== readerGeneration) return
    if (anchor) {
      await readerView.value?.restore(anchor, { allowMotion: false })
      if (activeGeneration !== readerGeneration) return
    }
    const current = captureCurrent()
    if (current) {
      percent.value = current.percent
      activeOrdinal.value = current.locator.segment.ordinal
    }
  } finally {
    if (
      operation === preferenceGeneration &&
      activeGeneration === readerGeneration
    ) {
      progress.captureSuppressed.value = false
    }
  }
}

function updatePreferences(candidate: ReaderPreferencesV2) {
  void applyPreferences(candidate)
}

function changeMode(mode: ReaderMode) {
  if (preferences.value.mode === mode) return
  void applyPreferences({ ...preferences.value, mode })
}

function resetPreferences() {
  const defaults = resetStoredPreferences()
  void applyPreferences(defaults)
}

async function selectChapter(selected: ReaderChapter) {
  const activeGeneration = readerGeneration
  chaptersOpen.value = false
  await nextTick()
  await new Promise<void>((resolve) => {
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => resolve()))
  })
  if (activeGeneration !== readerGeneration) return
  progress.captureSuppressed.value = true
  try {
    const position = await readerView.value?.moveToOrdinal(
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
    if (activeGeneration === readerGeneration) {
      progress.captureSuppressed.value = false
    }
  }
}

async function startCurrentVersion() {
  await progress.startCurrentVersion(moveToBeginning)
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
  progress.pageHide()
  progress.dispose()
  story.dispose()
  settingsOpen.value = false
  chaptersOpen.value = false
  navigationMessage.value = ''
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
  progress.pageHide()
  progress.dispose()
  story.dispose()
  delete document.documentElement.dataset.readerTheme
  document.title = 'Panda Pages'
})
</script>

<template>
  <div class="reader-shell" :class="themeClass">
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

      <main class="reader-main">
        <ReaderScrollView
          v-if="preferences.mode === 'scroll'"
          ref="readerView"
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
          ref="readerView"
          :title="story.story.value.title"
          :author="story.story.value.author"
          :language="story.story.value.language"
          :segments="story.story.value.segments"
          :font-family="fontStack"
          :font-size="preferences.fontSize"
          :line-height="preferences.lineHeight"
          :content-width="preferences.contentWidth"
          :capture-enabled="progress.captureEnabled.value"
          @position="onPosition"
          @active="onActive"
        />
      </main>

      <ReaderSettingsDialog
        v-model:open="settingsOpen"
        :model-value="preferences"
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
        @resume="progress.resume(restore)"
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
