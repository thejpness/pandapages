<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LibraryAppHeader from '../components/library/LibraryAppHeader.vue'
import ContinueReadingHero from '../components/library/ContinueReadingHero.vue'
import BookshelfGrid from '../components/library/BookshelfGrid.vue'
import StoryDetailsDialog from '../components/library/StoryDetailsDialog.vue'
import LibraryEmptyState from '../components/library/LibraryEmptyState.vue'
import {
  getAPIErrorStatus,
  getLibrary,
  isInvalidLibraryResponseError,
  logout as logoutSession,
} from '../lib/api'
import {
  selectLibraryHero,
  type LibraryStory,
} from '../lib/library-read-model'
import {
  defaultLibrarySort,
  filterLibraryStories,
  parseLibrarySortPreference,
  readLibrarySortPreference,
  selectSurpriseStory,
  sortLibraryStories,
  writeLibrarySortPreference,
  type LibrarySort,
} from '../lib/library-sorting'
import { authState } from '../lib/session'
import { navigationDidFail, runLockTransition } from '../lib/session-transitions'

type HeaderExpose = { focusSearch: () => void }
type LoadErrorKind = 'server-error' | 'malformed' | null

const router = useRouter()
const route = useRoute()
const headerRef = ref<HeaderExpose | null>(null)

const stories = ref<LibraryStory[]>([])
const loading = ref(true)
const loadError = ref<LoadErrorKind>(null)
const sessionLeaving = ref(false)
const locking = ref(false)
const lockError = ref('')
const selectedStory = ref<LibraryStory | null>(null)
const detailsOpen = ref(false)

const q = ref('')
const sort = ref<LibrarySort>('title')
let sortWasChosen = false
let queryTimer: number | null = null
let loadGeneration = 0

watch(
  () => route.query.q,
  async (value) => {
    const nextQuery = typeof value === 'string' ? value : ''
    if (nextQuery === q.value) return
    q.value = nextQuery
    await nextTick()
    if (nextQuery) headerRef.value?.focusSearch()
  },
  { immediate: true },
)

watch(q, (value) => {
  if (sessionLeaving.value) return
  if (queryTimer !== null) window.clearTimeout(queryTimer)
  queryTimer = window.setTimeout(() => {
    queryTimer = null
    const trimmed = value.trim()
    void router.replace({
      path: '/library',
      query: trimmed ? { q: trimmed } : {},
    })
  }, 180)
})

const matchingStories = computed(() =>
  filterLibraryStories(stories.value, q.value),
)
const visibleStories = computed(() =>
  sortLibraryStories(matchingStories.value, sort.value),
)
const heroStory = computed(() =>
  q.value.trim() ? null : selectLibraryHero(stories.value),
)
const resultLabel = computed(() => {
  if (loading.value || sessionLeaving.value) return ''
  const count = visibleStories.value.length
  if (!q.value.trim()) return `${count} ${count === 1 ? 'story' : 'stories'}`
  return `${count} ${count === 1 ? 'result' : 'results'}`
})

function setQuery(value: string) {
  q.value = value
}

function setSort(value: LibrarySort) {
  const valid = parseLibrarySortPreference(value)
  if (valid === null) return
  sort.value = valid
  sortWasChosen = true
  writeLibrarySortPreference(valid)
}

function clearSearch() {
  if (queryTimer !== null) {
    window.clearTimeout(queryTimer)
    queryTimer = null
  }
  q.value = ''
  void router.replace({ path: '/library', query: {} })
}

function goStory(story: LibraryStory) {
  void router.push(`/read/${encodeURIComponent(story.slug)}`)
}

function goSurprise() {
  const story = selectSurpriseStory(visibleStories.value)
  if (story !== null) goStory(story)
}

function openDetails(story: LibraryStory) {
  selectedStory.value = story
  detailsOpen.value = true
}

function updateDetailsOpen(open: boolean) {
  detailsOpen.value = open
  if (!open) {
    window.setTimeout(() => {
      if (!detailsOpen.value) selectedStory.value = null
    }, 0)
  }
}

function clearAccountState(clearQuery: boolean) {
  sessionLeaving.value = true
  loadGeneration += 1
  if (queryTimer !== null) {
    window.clearTimeout(queryTimer)
    queryTimer = null
  }
  stories.value = []
  selectedStory.value = null
  detailsOpen.value = false
  loadError.value = null
  if (clearQuery) q.value = ''
}

async function moveToUnlockAfterConfirmedSignOut() {
  clearAccountState(false)
  authState.confirmLocked()

  try {
    const result = await router.replace({
      path: '/unlock',
      query: { next: '/library' },
    })
    if (navigationDidFail(result)) {
      lockError.value =
        'Your session ended, but the Unlock screen could not be opened. Reload to continue.'
    }
  } catch {
    lockError.value =
      'Your session ended, but the Unlock screen could not be opened. Reload to continue.'
  }
}

async function lockLibrary() {
  if (locking.value) return
  locking.value = true
  lockError.value = ''

  try {
    const result = await runLockTransition({
      requestLogout: logoutSession,
      clearAccountState: () => clearAccountState(true),
      markLocked: authState.confirmLocked,
      navigateToUnlock: () => router.replace('/unlock'),
    })
    if (result === 'navigation-failed') {
      lockError.value =
        'Panda Pages is locked, but the Unlock screen could not be opened. Reload to continue.'
    }
  } catch {
    lockError.value = 'Could not lock Panda Pages. Your library is still open. Try again.'
  } finally {
    locking.value = false
  }
}

async function loadLibrary() {
  const generation = ++loadGeneration
  loading.value = true
  loadError.value = null

  try {
    const response = await getLibrary()
    if (generation !== loadGeneration || sessionLeaving.value) return
    stories.value = response.items
    if (!sortWasChosen) sort.value = defaultLibrarySort(response.items)
  } catch (error) {
    if (generation !== loadGeneration || sessionLeaving.value) return
    if (getAPIErrorStatus(error) === 401) {
      await moveToUnlockAfterConfirmedSignOut()
      return
    }
    loadError.value = isInvalidLibraryResponseError(error)
      ? 'malformed'
      : 'server-error'
  } finally {
    if (generation === loadGeneration && !sessionLeaving.value) {
      loading.value = false
    }
  }
}

onMounted(() => {
  const savedSort = readLibrarySortPreference()
  if (savedSort !== null) {
    sort.value = savedSort
    sortWasChosen = true
  }
  void loadLibrary()
})

onBeforeUnmount(() => {
  loadGeneration += 1
  if (queryTimer !== null) window.clearTimeout(queryTimer)
})
</script>

<template>
  <div class="library-page">
    <a class="library-skip-link" href="#library-main">Skip to the bookshelf</a>

    <LibraryAppHeader
      ref="headerRef"
      :q="q"
      :sort="sort"
      :result-label="resultLabel"
      :locking="locking"
      :surprise-disabled="visibleStories.length === 0 || loading || sessionLeaving"
      @update:q="setQuery"
      @update:sort="setSort"
      @clear="clearSearch"
      @surprise="goSurprise"
      @journey="router.push('/journey')"
      @admin="router.push('/admin/upload')"
      @lock="lockLibrary"
    />

    <main id="library-main" class="library-main" tabindex="-1">
      <h1 class="library-sr-only">Panda Pages story library</h1>

      <div v-if="lockError" class="library-alert" role="alert">
        <strong>Lock not completed</strong>
        <span>{{ lockError }}</span>
      </div>

      <LibraryEmptyState
        v-if="sessionLeaving"
        kind="session-ended"
      />

      <LibraryEmptyState
        v-else-if="loadError"
        :kind="loadError"
        :retrying="loading"
        @retry="loadLibrary"
      />

      <section v-else-if="loading" class="library-loading" aria-label="Loading library" role="status">
        <span class="library-sr-only">Loading your story library</span>
        <div class="library-loading__hero library-shimmer"></div>
        <div class="library-loading__heading library-shimmer"></div>
        <div class="library-loading__grid">
          <div v-for="index in 6" :key="index" class="library-loading__card">
            <span class="library-shimmer"></span>
            <i class="library-shimmer"></i>
            <i class="library-shimmer"></i>
          </div>
        </div>
      </section>

      <LibraryEmptyState
        v-else-if="stories.length === 0"
        kind="empty"
        @admin="router.push('/admin/upload')"
      />

      <LibraryEmptyState
        v-else-if="visibleStories.length === 0"
        kind="search"
        :query="q"
        @clear="clearSearch"
      />

      <template v-else>
        <ContinueReadingHero v-if="heroStory" :story="heroStory" />
        <BookshelfGrid :stories="visibleStories" @details="openDetails" />
      </template>
    </main>

    <StoryDetailsDialog
      :open="detailsOpen"
      :story="selectedStory"
      @update:open="updateDetailsOpen"
    />
  </div>
</template>

<style scoped>
.library-page,
.library-page *,
.library-page *::before,
.library-page *::after {
  box-sizing: border-box;
}

.library-page {
  --library-ink: #11110f;
  --library-paper: #f4f1e9;
  --library-white: #fffefa;
  --library-mist: #e7e3d9;
  --library-muted: #625f58;
  --library-line: rgba(17, 17, 15, 0.14);
  --library-line-strong: rgba(17, 17, 15, 0.24);
  --library-accent: #f2c75c;
  --library-accent-soft: #d9e5d2;
  --library-serif: "Literata Variable", Georgia, serif;
  --library-sans: "Atkinson Hyperlegible Next Variable", ui-sans-serif, sans-serif;
  --reader-focus: #1b6754;
  min-height: 100dvh;
  overflow-x: clip;
  background:
    radial-gradient(circle at 4% 4%, rgba(255, 255, 255, 0.92), transparent 22rem),
    radial-gradient(circle at 96% 32%, rgba(214, 228, 207, 0.7), transparent 24rem),
    var(--library-paper);
  color: var(--library-ink);
  color-scheme: light;
  font-family: var(--library-sans);
  -webkit-font-smoothing: antialiased;
}

.library-skip-link {
  position: fixed;
  z-index: 100;
  top: max(0.6rem, env(safe-area-inset-top));
  left: max(0.6rem, env(safe-area-inset-left));
  transform: translateY(-180%);
  border: 2px solid var(--library-ink);
  padding: 0.65rem 0.9rem;
  background: var(--library-white);
  color: var(--library-ink);
  font-weight: 850;
  text-decoration: none;
}

.library-skip-link:focus {
  transform: none;
}

.library-main {
  width: min(80rem, 100%);
  min-width: 0;
  margin-inline: auto;
  padding: clamp(1.3rem, 4vw, 3.2rem) max(1rem, env(safe-area-inset-right)) max(3rem, calc(2rem + env(safe-area-inset-bottom))) max(1rem, env(safe-area-inset-left));
}

.library-main:focus-visible {
  outline: 3px solid var(--reader-focus);
  outline-offset: -3px;
}

.library-alert {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem 0.7rem;
  margin-bottom: 1rem;
  border: 1px solid #9b4e45;
  border-radius: 0.9rem;
  padding: 0.8rem 1rem;
  background: #fff0ec;
  color: #6e2a22;
  font-size: 0.84rem;
}

.library-alert strong {
  font-weight: 900;
}

.library-loading__hero {
  height: clamp(11rem, 25vw, 15rem);
  border-radius: 2rem;
}

.library-loading__heading {
  width: min(22rem, 70%);
  height: 2.5rem;
  margin: 3rem 0 1.3rem;
  border-radius: 0.7rem;
}

.library-loading__grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1.2rem;
}

.library-loading__card {
  display: grid;
  grid-template-columns: 40% 1fr;
  grid-template-rows: auto auto;
  gap: 0.8rem;
  min-height: 14rem;
  border: 1px solid var(--library-line);
  border-radius: 1.35rem;
  padding: 1rem;
  background: var(--library-white);
}

.library-loading__card span {
  grid-row: 1 / -1;
  border-radius: 0.5rem;
}

.library-loading__card i {
  height: 1.1rem;
  border-radius: 0.4rem;
}

.library-shimmer {
  background: var(--library-mist);
}

.library-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}

@media (max-width: 74.99rem) {
  .library-loading__grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 45rem) {
  .library-loading__grid {
    grid-template-columns: 1fr;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .library-shimmer {
    position: relative;
    overflow: hidden;
  }

  .library-shimmer::after {
    content: "";
    position: absolute;
    inset: 0;
    transform: translateX(-110%);
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.7), transparent);
    animation: library-shimmer 1.35s infinite;
  }

  @keyframes library-shimmer {
    to { transform: translateX(110%); }
  }
}
</style>
