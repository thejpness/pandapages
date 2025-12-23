<script setup lang="ts">
import { onMounted, ref, computed, onBeforeUnmount, watch, nextTick } from 'vue'
import {
  getLibrary,
  getContinue,
  getSettings,
  type ContinueItem,
  type LibraryItem,
  type SettingsPayload,
} from '../lib/api'
import { useRouter, useRoute } from 'vue-router'
import StoryCard from '../components/StoryCard.vue'
import LibraryHeader from '../components/LibraryHeader.vue'
import { haptic } from '../lib/haptics'

type Item = LibraryItem
type RecentCard = ContinueItem & { story: Item }

type HeaderExpose = {
  focusSearch: () => void
}

const router = useRouter()
const route = useRoute()

const headerRef = ref<HeaderExpose | null>(null)

const items = ref<Item[]>([])
const recent = ref<ContinueItem[]>([])
const loading = ref(true)
const q = ref('')

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
  return items.value.find(s => s.slug === infoSlug.value) || null
})

// --- Querystring sync (prefill + debounced write-back) ---
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
    if (qTimer) window.clearTimeout(qTimer)
    qTimer = window.setTimeout(() => {
      const trimmed = v.trim()
      router.replace({
        path: '/library',
        query: trimmed ? { q: trimmed } : {},
      })
    }, 250)
  }
)

// --- Derived state ---
const resume = computed(() => recent.value[0] || null)

const resumeItem = computed<Item | null>(() => {
  const slug = resume.value?.slug || ''
  if (!slug) return null
  return items.value.find(x => x.slug === slug) || null
})

const recentCards = computed<RecentCard[]>(() => {
  const list = recent.value.slice(1, 4)
  const out: RecentCard[] = []
  for (const r of list) {
    const story = items.value.find(s => s.slug === r.slug)
    if (story) out.push({ ...r, story })
  }
  return out
})

const filtered = computed<Item[]>(() => {
  const list = items.value
  const s = q.value.trim().toLowerCase()
  if (!s) return list
  return list.filter(x =>
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

const headerResume = computed(() => {
  if (!resumeItem.value || !resume.value) return null
  return {
    slug: resumeItem.value.slug,
    title: resumeItem.value.title,
    author: resumeItem.value.author ?? undefined,
    percent: pct(resume.value.percent),
  }
})

const headerRecent = computed(() => {
  return recentCards.value.map(r => ({
    slug: r.slug,
    title: r.story.title,
    author: r.story.author ?? undefined,
    percent: pct(r.percent),
  }))
})

// --- Actions ---
function go(slug: string) {
  router.push(`/read/${slug}`)
}

function openAdmin() {
  router.push('/admin')
}

function openJourney() {
  router.push('/journey')
}

function randomStory() {
  const list = filtered.value
  if (!list.length) return
  haptic('light')
  const pick = list[Math.floor(Math.random() * list.length)]!
  go(pick.slug)
}

function clearSearch() {
  q.value = ''
  router.replace({ path: '/library', query: {} })
  headerRef.value?.focusSearch()
}

function openTopResult() {
  const list = filtered.value
  if (!list.length) return
  go(list[0]!.slug)
}

// Called by StoryCard (StoryCard already does haptic('select'))
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

async function loadSettings() {
  settingsLoaded.value = false
  try {
    const s = await getSettings()
    settings.value = s
  } catch {
    // If settings endpoint isn't ready yet, don't punish the user.
    settings.value = null
  } finally {
    settingsLoaded.value = true
  }
}

async function load() {
  loading.value = true
  try {
    const [cont, data] = await Promise.all([getContinue(4), getLibrary()])
    recent.value = Array.isArray(cont?.items) ? cont.items : []
    items.value = Array.isArray(data?.items) ? data.items : []
  } catch {
    recent.value = []
    items.value = []
    router.replace('/unlock')
  } finally {
    loading.value = false
  }

  // Settings load happens after; errors shouldn't bounce to /unlock
  loadSettings()
}

onMounted(() => {
  load()
  window.addEventListener('keydown', onKey)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  if (qTimer) window.clearTimeout(qTimer)
})
</script>

<template>
  <div class="min-h-dvh bg-[#0B1724] text-white">
    <LibraryHeader
      ref="headerRef"
      :loading="loading"
      :q="q"
      :resultsLabel="resultsLabel"
      :resume="headerResume"
      :recent="headerRecent"
      :personalisationLabel="personalisationLabel"
      :hasPersonalisation="hasPersonalisation"
      @update:q="(v: string) => (q = v)"
      @admin="openAdmin"
      @journey="openJourney"
      @random="randomStory"
      @clear="clearSearch"
      @top="openTopResult"
      @go="(slug: string) => go(slug)"
    />

    <main class="max-w-6xl mx-auto px-4 md:px-8 py-6 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
      <div v-if="loading" class="space-y-4">
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
          @click="openAdmin"
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
          class="text-left rounded-2xl border border-white/10 bg-white/5 p-5 hover:bg-white/10 active:scale-[0.995] transition will-change-transform"
          @pointerdown="haptic('select')"
          @click="go(s.slug)"
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
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm hover:bg-white/10 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="openJourney"
        >
          ✨ Personalise stories
        </button>

        <button
          type="button"
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm hover:bg-white/10 active:scale-[0.995] transition"
          @click="randomStory"
          :disabled="filtered.length === 0"
          :class="filtered.length === 0 ? 'opacity-50 cursor-not-allowed' : ''"
        >
          🎲 Random story
        </button>

        <button
          type="button"
          class="w-full rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm hover:bg-white/10 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="openAdmin"
        >
          🛠 Admin
        </button>
      </div>
    </main>

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
            class="rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm hover:bg-white/10 active:scale-[0.99] transition"
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
            @click="go(modalStory.slug)"
          >
            Read now
          </button>

          <button
            type="button"
            class="flex-1 rounded-2xl border border-white/10 bg-white/5 py-3 font-semibold hover:bg-white/10 active:scale-[0.99] transition"
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
  .shimmer { position: relative; overflow: hidden; }
  .shimmer::after {
    content: "";
    position: absolute;
    inset: 0;
    transform: translateX(-120%);
    background: linear-gradient(90deg, transparent, rgba(255,255,255,0.10), transparent);
    animation: shimmer 1.2s infinite;
  }
  @keyframes shimmer {
    0% { transform: translateX(-120%); }
    100% { transform: translateX(120%); }
  }
}
</style>
