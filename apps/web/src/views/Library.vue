<script setup lang="ts">
import { onMounted, ref, computed, onBeforeUnmount, watch, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  getLibrary,
  getContinue,
  getSettings,
  getAPIErrorStatus,
  logout as logoutSession,
  type ContinueItem,
  type LibraryItem,
  type SettingsPayload,
} from '../lib/api'
import StoryCard from '../components/StoryCard.vue'
import LibraryHeader from '../components/LibraryHeader.vue'
import { haptic } from '../lib/haptics'
import { authState } from '../lib/session'
import { navigationDidFail, runLockTransition } from '../lib/session-transitions'

type Item = LibraryItem
type RecentCard = ContinueItem & { story: Item }

type HeaderExpose = { focusSearch: () => void }

type HeaderResume = { slug: string; title: string; author?: string; percent: number } | null
type HeaderRecent = { slug: string; title: string; author?: string; percent: number }

const router = useRouter()
const route = useRoute()

const headerRef = ref<HeaderExpose | null>(null)

const items = ref<Item[]>([])
const recentRaw = ref<ContinueItem[]>([])
const loading = ref(true)
const loadError = ref('')
const locking = ref(false)
const lockError = ref('')
const sessionLeaving = ref(false)

// Search query
const q = ref('')
function setQ(v: string) {
  q.value = v
}

// Settings / Journey
const settings = ref<SettingsPayload | null>(null)
const settingsLoaded = ref(false)

const childName = computed(() => (settings.value?.child?.name || '').trim())
const childAgeMonths = computed(() => Number(settings.value?.child?.ageMonths ?? 0))

function ageLabelFromMonths(m: number) {
  const n = Number.isFinite(m) ? Math.max(0, Math.floor(m)) : 0
  if (n < 12) return `${n}m`
  const y = Math.floor(n / 12)
  const r = n % 12
  return r === 0 ? `${y}y` : `${y}y ${r}m`
}

const personalisationLabel = computed(() => {
  if (!settingsLoaded.value) return 'Loading…'
  if (!childName.value) return 'Not personalised'
  const age = ageLabelFromMonths(childAgeMonths.value)
  return `Personalised: ${childName.value}${childAgeMonths.value ? ` (${age})` : ''}`
})

const hasPersonalisation = computed(() => !!childName.value)

// Info modal state
const infoSlug = ref<string | null>(null)
const modalStory = computed<Item | null>(() => {
  if (!infoSlug.value) return null
  return items.value.find((s) => s.slug === infoSlug.value) || null
})

/* ---------------- Querystring sync ---------------- */

watch(
  () => route.query.q,
  async (v) => {
    const s = typeof v === 'string' ? v : ''
    if (s !== q.value) {
      q.value = s
      await nextTick()
      if (s) headerRef.value?.focusSearch()
    }
  },
  { immediate: true }
)

let qTimer: number | null = null
watch(
  () => q.value,
  (v) => {
    if (locking.value) return
    if (qTimer) window.clearTimeout(qTimer)
    qTimer = window.setTimeout(() => {
      const trimmed = v.trim()
      void router.replace({ path: '/library', query: trimmed ? { q: trimmed } : {} })
    }, 250)
  }
)

/* ---------------- Derived state ---------------- */

const resumeRaw = computed(() => recentRaw.value[0] || null)

const resumeItem = computed<Item | null>(() => {
  const slug = resumeRaw.value?.slug || ''
  if (!slug) return null
  return items.value.find((x) => x.slug === slug) || null
})

const recentCards = computed<RecentCard[]>(() => {
  const list = recentRaw.value.slice(1, 4)
  const out: RecentCard[] = []
  for (const r of list) {
    const story = items.value.find((s) => s.slug === r.slug)
    if (story) out.push({ ...r, story })
  }
  return out
})

const filtered = computed<Item[]>(() => {
  const list = items.value
  const s = q.value.trim().toLowerCase()
  if (!s) return list
  return list.filter(
    (x) =>
      (x.title || '').toLowerCase().includes(s) ||
      (x.author || '').toLowerCase().includes(s) ||
      (x.slug || '').toLowerCase().includes(s)
  )
})

const resultsLabel = computed(() => {
  if (loading.value) return ''
  const n = filtered.value.length
  if (!q.value.trim()) return `${n} ${n === 1 ? 'story' : 'stories'}`
  return `${n} result${n === 1 ? '' : 's'}`
})

function pct(p: number) {
  const v = Number.isFinite(p) ? p : 0
  return Math.max(0, Math.min(100, Math.round(v * 100)))
}

// These shapes match LibraryHeader expectations (title + author included)
const headerResume = computed<HeaderResume>(() => {
  if (!resumeItem.value || !resumeRaw.value) return null
  return {
    slug: resumeItem.value.slug,
    title: resumeItem.value.title,
    author: resumeItem.value.author ?? undefined,
    percent: pct(resumeRaw.value.percent),
  }
})

const headerRecent = computed<HeaderRecent[]>(() => {
  return recentCards.value.map((r) => ({
    slug: r.slug,
    title: r.story.title,
    author: r.story.author ?? undefined,
    percent: pct(r.percent),
  }))
})

/* ---------------- Actions ---------------- */

function goStory(slug: string) {
  void router.push(`/read/${encodeURIComponent(slug)}`)
}

function goAdmin() {
  void router.push('/admin/upload')
}

function goJourney() {
  void router.push('/journey')
}

function goRandom() {
  const list = filtered.value
  if (!list.length) return
  haptic('light')
  const pick = list[Math.floor(Math.random() * list.length)]
  goStory(pick.slug)
}

function clearSearch() {
  q.value = ''
  void router.replace({ path: '/library', query: {} })
  headerRef.value?.focusSearch()
}

function scrollTop() {
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function openTopResult() {
  const list = filtered.value
  if (!list.length) return
  goStory(list[0].slug)
}

function openInfo(slug: string) {
  infoSlug.value = slug
}

function closeInfo() {
  infoSlug.value = null
}

async function searchSlug(slug: string) {
  q.value = slug
  closeInfo()
  await nextTick()
  headerRef.value?.focusSearch()
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (infoSlug.value) closeInfo()
    else clearSearch()
  }
}

function clearAccountState(clearQuery: boolean) {
  sessionLeaving.value = true
  if (qTimer) {
    window.clearTimeout(qTimer)
    qTimer = null
  }
  infoSlug.value = null
  if (clearQuery) q.value = ''
  items.value = []
  recentRaw.value = []
  settings.value = null
  settingsLoaded.value = false
}

async function moveToUnlockAfterConfirmedSignOut() {
  clearAccountState(false)
  authState.confirmLocked()

  try {
    const result = await router.replace({ path: '/unlock', query: { next: '/library' } })
    if (navigationDidFail(result)) {
      lockError.value = 'The session ended, but the passcode screen could not be opened. Reload to continue.'
    }
  } catch {
    lockError.value = 'The session ended, but the passcode screen could not be opened. Reload to continue.'
  }
}

async function logout() {
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
      lockError.value = 'Panda Pages is locked, but the passcode screen could not be opened. Reload to continue.'
    }
  } catch {
    lockError.value = 'Could not lock Panda Pages. Try again.'
  } finally {
    locking.value = false
  }
}

/* ---------------- "Top" button visibility ---------------- */

const showTop = ref(false)

function onScroll() {
  showTop.value = window.scrollY > 600
}

/* ---------------- Data loading ---------------- */

async function loadSettings() {
  settingsLoaded.value = false
  try {
    const result = await getSettings()
    if (!sessionLeaving.value) settings.value = result
  } catch (error) {
    if (getAPIErrorStatus(error) === 401) {
      await moveToUnlockAfterConfirmedSignOut()
      return
    }
    settings.value = null
  } finally {
    if (!sessionLeaving.value) settingsLoaded.value = true
  }
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [cont, data] = await Promise.all([getContinue(4), getLibrary()])
    if (sessionLeaving.value) return
    recentRaw.value = Array.isArray(cont?.items) ? cont.items : []
    items.value = Array.isArray(data?.items) ? data.items : []
  } catch (error) {
    if (getAPIErrorStatus(error) === 401) {
      await moveToUnlockAfterConfirmedSignOut()
      return
    }
    loadError.value = 'Could not load the library. Try again.'
  } finally {
    loading.value = false
  }

  void loadSettings()
}

onMounted(() => {
  void load()
  window.addEventListener('keydown', onKey)
  window.addEventListener('scroll', onScroll, { passive: true })
  onScroll()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('scroll', onScroll)
  if (qTimer) window.clearTimeout(qTimer)
})
</script>

<template>
  <div class="min-h-dvh bg-[#0B1724] text-white">
    <LibraryHeader
      ref="headerRef"
      :loading="loading"
      :locking="locking"
      :q="q"
      :resultsLabel="resultsLabel"
      :resume="headerResume"
      :recent="headerRecent"
      :personalisationLabel="personalisationLabel"
      :hasPersonalisation="hasPersonalisation"
      @update:q="setQ"
      @admin="goAdmin"
      @journey="goJourney"
      @random="goRandom"
      @clear="clearSearch"
      @top="openTopResult"
      @go="goStory"
      @logout="logout"
    />

    <main class="max-w-6xl mx-auto px-4 md:px-8 py-6 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
      <div
        v-if="lockError"
        class="mb-5 rounded-2xl border border-red-300/30 bg-red-300/10 p-4 text-sm text-red-100"
        role="alert"
      >
        {{ lockError }}
      </div>

      <div
        v-if="loadError"
        class="rounded-2xl border border-amber-200/30 bg-amber-200/10 p-5 text-sm"
        role="alert"
      >
        <p>{{ loadError }}</p>
        <button type="button" class="mt-3 rounded-xl bg-white px-4 py-2 font-medium text-[#0B1724]" @click="load">
          Try again
        </button>
      </div>

      <div v-else-if="loading" class="space-y-4">
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          <div v-for="i in 6" :key="i" class="rounded-2xl border border-white/10 bg-white/5 p-5">
            <div class="flex items-start gap-4">
              <div class="h-14 w-11 rounded-lg bg-white/10 shimmer"></div>
              <div class="flex-1">
                <div class="h-4 w-2/3 rounded bg-white/10 shimmer"></div>
                <div class="mt-3 h-3 w-1/2 rounded bg-white/10 shimmer"></div>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div v-else-if="items.length === 0" class="mt-10 rounded-2xl border border-white/10 bg-white/5 p-6">
        <div class="text-lg font-semibold">No stories yet</div>
        <p class="mt-2 text-sm opacity-80">Add a few stories and you’ll have a bedtime library.</p>
        <button
          type="button"
          class="mt-4 rounded-xl bg-white text-[#0B1724] px-4 py-2 font-medium"
          @pointerdown="haptic('select')"
          @click="goAdmin"
        >
          Open Admin
        </button>
      </div>

      <div v-else-if="filtered.length === 0" class="mt-8 text-sm opacity-70">
        No stories match “{{ q }}”.
        <button
          type="button"
          class="ml-2 underline opacity-90 hover:opacity-100"
          @pointerdown="haptic('select')"
          @click="clearSearch"
        >
          Clear
        </button>
      </div>

      <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <button
          v-for="s in filtered"
          :key="s.slug"
          type="button"
          class="text-left rounded-2xl border border-white/10 bg-white/5 p-5
                 hover:bg-white/10 active:scale-[0.995] transition will-change-transform"
          @pointerdown="haptic('select')"
          @click="goStory(s.slug)"
        >
          <StoryCard
            :title="s.title"
            :author="s.author ?? undefined"
            :meta="`/read/${s.slug}`"
            :showInfo="true"
            :onInfo="() => openInfo(s.slug)"
          />
        </button>
      </div>

      <div class="sm:hidden mt-6 space-y-3">
        <button
          type="button"
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm
                 hover:bg-white/10 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="goJourney"
        >
          ✨ Personalise stories
        </button>

        <button
          type="button"
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm
                 hover:bg-white/10 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="goRandom"
          :disabled="filtered.length === 0"
          :class="filtered.length === 0 ? 'opacity-50 cursor-not-allowed' : ''"
        >
          🎲 Random story
        </button>

        <button
          type="button"
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm
                 hover:bg-white/10 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="goAdmin"
        >
          🛠 Admin
        </button>

        <button
          type="button"
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm
                 hover:bg-white/10 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="logout"
          :disabled="locking"
          :class="locking ? 'opacity-50 cursor-not-allowed' : ''"
        >
          🔒 {{ locking ? 'Locking…' : 'Lock' }}
        </button>
      </div>
    </main>

    <!-- Floating Top button (appears after scrolling) -->
    <button
      v-if="showTop"
      type="button"
      class="fixed z-30 right-4 bottom-[calc(1rem+env(safe-area-inset-bottom))]
             rounded-full border border-white/10 bg-white/10 backdrop-blur-xl
             px-4 py-2 text-sm font-medium shadow-lg
             hover:bg-white/15 active:scale-[0.98] transition"
      @pointerdown="haptic('select')"
      @click="scrollTop"
      aria-label="Back to top"
      title="Back to top"
    >
      ↑ Top
    </button>

    <!-- Info modal -->
    <div v-if="modalStory !== null" class="fixed inset-0 z-40" @click.self="closeInfo">
      <div class="absolute inset-0 bg-black/55 backdrop-blur-sm"></div>

      <div
        class="absolute inset-x-0 bottom-0 mx-auto max-w-2xl rounded-t-3xl border border-white/10 bg-[#0B1724]/95 p-5
               pb-[calc(1.25rem+env(safe-area-inset-bottom))]"
      >
        <div class="flex items-start justify-between gap-3">
          <div>
            <div class="text-xs opacity-70">Story</div>
            <div class="mt-1 text-xl font-semibold">{{ modalStory.title }}</div>
            <div v-if="modalStory.author" class="mt-1 text-sm opacity-75">{{ modalStory.author }}</div>
            <div class="mt-3 text-xs opacity-70 break-all">Slug: {{ modalStory.slug }}</div>
          </div>

          <button
            type="button"
            class="rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm
                   hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="closeInfo"
          >
            Close
          </button>
        </div>

        <div class="mt-5 flex gap-2">
          <button
            type="button"
            class="flex-1 rounded-2xl bg-white text-black py-3 font-semibold active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="goStory(modalStory.slug)"
          >
            Read now
          </button>

          <button
            type="button"
            class="flex-1 rounded-2xl border border-white/10 bg-white/5 py-3 font-semibold
                   hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="searchSlug(modalStory.slug)"
          >
            Search slug
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@media (prefers-reduced-motion: no-preference) {
  .shimmer {
    position: relative;
    overflow: hidden;
  }
  .shimmer::after {
    content: '';
    position: absolute;
    inset: 0;
    transform: translateX(-120%);
    background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.1), transparent);
    animation: shimmer 1.2s infinite;
  }
  @keyframes shimmer {
    0% {
      transform: translateX(-120%);
    }
    100% {
      transform: translateX(120%);
    }
  }
}
</style>
