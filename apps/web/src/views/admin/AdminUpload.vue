<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  adminListStories,
  adminPreview,
  adminDraftUpsertStory,
  adminPublishStory,
  getLibrary,
  type AdminPreviewResponse,
  type AdminDraftUpsertResponse,
  type LibraryItem,
  type AdminStoryListItem,
} from '@/lib/api'

const router = useRouter()

function slugify(input: string): string {
  return input
    .toLowerCase()
    .trim()
    .replace(/['"]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80)
}

function clamp01(n: number) {
  if (!Number.isFinite(n)) return 0
  return Math.max(0, Math.min(1, n))
}

function normaliseNewlines(s: string) {
  return s.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
}

function stripGutenbergBoilerplate(text: string) {
  const t = normaliseNewlines(text)
  const startRe = /\*\*\*\s*START OF (?:THE|THIS) PROJECT GUTENBERG EBOOK[\s\S]*?\*\*\*/i
  const endRe = /\*\*\*\s*END OF (?:THE|THIS) PROJECT GUTENBERG EBOOK[\s\S]*?\*\*\*/i

  const startIdx = t.search(startRe)
  let body = t
  if (startIdx >= 0) {
    const m = startRe.exec(t)
    const consumed = m?.[0]?.length ?? 0
    body = t.slice(startIdx + consumed)
  }

  const endIdx = body.search(endRe)
  if (endIdx >= 0) body = body.slice(0, endIdx)

  return body.trim()
}

function promoteChaptersToMarkdown(text: string) {
  const lines = normaliseNewlines(text).split('\n')
  const out: string[] = []
  const chapterLike = /^(chapter|book|letter|part)\s+([0-9ivxlcdm]+)\b\.?\s*(.*)$/i

  for (const raw of lines) {
    const line = raw.trimEnd()
    const m = chapterLike.exec(line.trim())
    if (m) {
      const head = `${m[1]!.toUpperCase()} ${m[2]!.toUpperCase()}${m[3] ? ` — ${m[3].trim()}` : ''}`
      out.push(`## ${head}`)
      out.push('')
      continue
    }
    out.push(line)
  }
  return out.join('\n').trim()
}

function ensureH1(h1: string, md: string) {
  const t = md.trimStart()
  if (t.startsWith('# ')) return md
  const safe = (h1 || 'Untitled').trim() || 'Untitled'
  return `# ${safe}\n\n${md.trim()}\n`
}

function parseTitleAuthorFromFilename(name: string): { title: string; author: string } {
  const base = name.replace(/\.[^.]+$/, '').trim()
  const parts = base.split(' - ').map((s) => s.trim()).filter(Boolean)
  if (parts.length >= 2) {
    return { title: parts[0]!, author: parts.slice(1).join(' - ') }
  }
  return { title: base, author: '' }
}

function toMsg(e: unknown, fallback: string) {
  const anyErr = e as any
  return anyErr?.message || fallback
}

function isDuplicateContentErr(e: unknown): boolean {
  const msg = String((e as any)?.message || '').toLowerCase()
  return (
    msg.includes('content_hash') ||
    msg.includes('story_versions_story_id_content_hash_key') ||
    msg.includes('sqlstate 23505') ||
    msg.includes('duplicate key value')
  )
}

function resolveReaderPath(storySlug: string): string {
  const routes = router.getRoutes()
  const pick =
    routes.find((r) => String(r.name || '').toLowerCase().includes('reader')) ||
    routes.find((r) => r.path.toLowerCase().includes('read') && r.path.includes(':slug')) ||
    routes.find((r) => r.path.toLowerCase().includes('story') && r.path.includes(':slug')) ||
    routes.find((r) => r.path.includes(':slug'))

  if (pick?.path) return pick.path.replace(':slug', encodeURIComponent(storySlug))
  return `/read/${encodeURIComponent(storySlug)}`
}

/* ----------------------------- state ----------------------------- */

const title = ref('')
const author = ref('')
const slug = ref('')
const markdown = ref('')
const sourceUrl = ref('')

const publish = ref(true)

const saving = ref(false)
const previewing = ref(false)

const message = ref<string | null>(null)
const error = ref<string | null>(null)

const preview = ref<AdminPreviewResponse | null>(null)
const lastDraft = ref<AdminDraftUpsertResponse | null>(null)

const recent = ref<Array<{ slug: string; title: string; version: number; published: boolean }>>([])

type SuccessInfo = { slug: string; title: string; version: number; published: boolean }
const showSuccess = ref(false)
const successInfo = ref<SuccessInfo | null>(null)

const library = ref<LibraryItem[]>([])
const loadingLibrary = ref(false)

const allStories = ref<AdminStoryListItem[]>([])
const loadingAllStories = ref(false)

/* ------------------------- auto-route UX ------------------------- */
/**
 * Premium iOS feel:
 * - show modal immediately
 * - auto-open reader after 600ms
 * - cancel if user interacts with modal/backdrop/buttons
 */
let autoRouteTimer: number | null = null
const autoRouteEnabled = ref(true)

function clearAutoRoute() {
  if (autoRouteTimer !== null) {
    window.clearTimeout(autoRouteTimer)
    autoRouteTimer = null
  }
}

function cancelAutoRoute() {
  autoRouteEnabled.value = false
  clearAutoRoute()
}

function scheduleAutoRoute(storySlug: string) {
  clearAutoRoute()
  autoRouteEnabled.value = true
  autoRouteTimer = window.setTimeout(() => {
    // Only route if modal is still open and user hasn't interacted
    if (!showSuccess.value) return
    if (!autoRouteEnabled.value) return
    openReader(storySlug)
  }, 600)
}

// iOS: prevent background scroll when modal is open
function setBodyScrollLocked(locked: boolean) {
  const body = document.body
  if (locked) {
    body.style.overflow = 'hidden'
    body.style.touchAction = 'none'
  } else {
    body.style.overflow = ''
    body.style.touchAction = ''
  }
}

watch(showSuccess, (v) => {
  setBodyScrollLocked(v)
  if (!v) cancelAutoRoute()
})

onBeforeUnmount(() => {
  clearAutoRoute()
  setBodyScrollLocked(false)
})

/* ------------------------- slug behaviour ------------------------ */

const slugTouched = ref(false)
watch(title, (t) => {
  if (!slugTouched.value) slug.value = slugify(t)
})

function onSlugInput(v: string) {
  slugTouched.value = true
  slug.value = v
}

/* --------------------------- computed ---------------------------- */

const canPreview = computed(() => markdown.value.trim().length > 0)
const canSave = computed(() => {
  return (
    title.value.trim().length > 0 &&
    slug.value.trim().length > 0 &&
    markdown.value.trim().length > 0
  )
})

const previewSegmentsCount = computed(() => {
  if (preview.value?.segments?.length) return preview.value.segments.length
  if (lastDraft.value) return lastDraft.value.segmentsCount
  return 0
})

/* --------------------------- actions ----------------------------- */

async function refreshLibrary() {
  loadingLibrary.value = true
  try {
    const res = await getLibrary()
    library.value = res.items
  } catch {
    // ignore in v1
  } finally {
    loadingLibrary.value = false
  }
}

async function refreshAllStories() {
  loadingAllStories.value = true
  try {
    const res = await adminListStories()
    allStories.value = res.items
  } catch {
    // ignore in v1
  } finally {
    loadingAllStories.value = false
  }
}

function openReader(storySlug: string) {
  router.push(resolveReaderPath(storySlug))
}

async function copyReaderLink(storySlug: string) {
  const path = resolveReaderPath(storySlug)
  const url = `${window.location.origin}${path}`
  try {
    await navigator.clipboard.writeText(url)
    message.value = 'Link copied.'
  } catch {
    message.value = url
  }
}

function resetForm(keepAuthor = true) {
  title.value = ''
  slug.value = ''
  slugTouched.value = false
  markdown.value = ''
  sourceUrl.value = ''
  preview.value = null
  lastDraft.value = null
  error.value = null
  message.value = null
  if (!keepAuthor) author.value = ''
}

/* ---------- modal handlers (avoid template nullable TS issues) ---------- */

function closeSuccessModal() {
  cancelAutoRoute()
  showSuccess.value = false
}

function onBackdropClick() {
  closeSuccessModal()
}

function readNowFromModal() {
  cancelAutoRoute()
  const s = successInfo.value?.slug
  if (s) openReader(s)
}

async function copyLinkFromModal() {
  cancelAutoRoute()
  const s = successInfo.value?.slug
  if (s) await copyReaderLink(s)
}

function uploadAnotherFromModal() {
  cancelAutoRoute()
  showSuccess.value = false
  resetForm(true)
}

/* ----------------------------- preview ---------------------------- */

async function runPreview() {
  error.value = null
  message.value = null
  preview.value = null

  if (!canPreview.value) {
    error.value = 'Paste some markdown first.'
    return
  }

  previewing.value = true
  try {
    const out = await adminPreview({ markdown: markdown.value })
    preview.value = out
    message.value = `Preview ready (${out.segments.length} segments)`
  } catch (e) {
    error.value = toMsg(e, 'Preview failed')
  } finally {
    previewing.value = false
  }
}

/* ------------------------------ save ------------------------------ */

async function submit() {
  error.value = null
  message.value = null

  if (!canSave.value) {
    error.value = 'Title, slug, and markdown are required.'
    return
  }

  saving.value = true
  try {
    const draft = await adminDraftUpsertStory({
      slug: slug.value.trim(),
      title: title.value.trim(),
      author: author.value.trim() || null,
      markdown: markdown.value,
      sourceUrl: sourceUrl.value.trim() || null,
    })

    lastDraft.value = draft
    preview.value = { renderedHtml: draft.renderedHtml, segments: [] }

    let didPublish = false
    if (publish.value) {
      await adminPublishStory(draft.slug, draft.storyVersionId)
      didPublish = true
    }

    const info: SuccessInfo = {
      slug: draft.slug,
      title: title.value.trim() || draft.slug,
      version: draft.version,
      published: didPublish,
    }

    const key = `${info.slug}@${info.version}`
    if (!recent.value.some((x) => `${x.slug}@${x.version}` === key)) {
      recent.value.unshift(info)
      recent.value = recent.value.slice(0, 10)
    }

    successInfo.value = info
    showSuccess.value = true

    message.value = didPublish ? `Saved & published: v${draft.version}` : `Draft saved: v${draft.version}`

    // Refresh lists so the admin feels "done"
    await Promise.all([refreshLibrary(), refreshAllStories()])

    // Premium: auto-route unless user interacts
    scheduleAutoRoute(info.slug)
  } catch (e) {
    if (isDuplicateContentErr(e)) {
      const s = slug.value.trim()
      message.value = 'No changes detected — that exact content was already uploaded.'
      error.value = null

      if (s) {
        successInfo.value = {
          slug: s,
          title: title.value.trim() || s,
          version: lastDraft.value?.version ?? 1,
          published: publish.value,
        }
        showSuccess.value = true
        await Promise.all([refreshLibrary(), refreshAllStories()])
        scheduleAutoRoute(s)
      }
    } else {
      error.value = toMsg(e, 'Save failed')
    }
  } finally {
    saving.value = false
  }
}

/* ---------------------------- import ----------------------------- */

const fileInputRef = ref<HTMLInputElement | null>(null)

function pickFile() {
  fileInputRef.value?.click()
}

async function onFilePicked(ev: Event) {
  const input = ev.target as HTMLInputElement | null
  const file = input?.files?.[0]
  if (!file) return
  if (input) input.value = ''

  error.value = null
  message.value = null

  const inferred = parseTitleAuthorFromFilename(file.name)
  if (!title.value.trim()) title.value = inferred.title
  if (!author.value.trim() && inferred.author) author.value = inferred.author

  let text = await file.text()

  const ext = (file.name.split('.').pop() || '').toLowerCase()
  if (file.type.includes('html') || ext === 'html' || ext === 'htm') {
    const parser = new DOMParser()
    const doc = parser.parseFromString(text, 'text/html')
    text = (doc.body?.innerText ?? '').trim()
  }

  text = stripGutenbergBoilerplate(text)
  text = promoteChaptersToMarkdown(text)
  text = ensureH1(title.value, text)

  markdown.value = text
  message.value = `Imported: ${file.name}`
}

/* ------------------------- tiny UX helpers ------------------------ */

const saveButtonLabel = computed(() => {
  if (saving.value) return 'Saving…'
  return publish.value ? 'Save & Publish' : 'Save Draft'
})

const previewButtonLabel = computed(() => {
  if (previewing.value) return 'Previewing…'
  return 'Preview'
})

const saveDisabled = computed(() => saving.value || !canSave.value)
const previewDisabled = computed(() => previewing.value || !canPreview.value)

const progressHint = computed(() => {
  const chars = markdown.value.length
  const approx = clamp01(chars / 8000)
  return Math.round(approx * 100)
})

onMounted(async () => {
  await Promise.all([refreshLibrary(), refreshAllStories()])
})
</script>

<template>
  <section class="rounded-2xl border border-white/10 bg-white/5 p-5">
    <!-- Success modal -->
    <div
      v-if="showSuccess && successInfo"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      tabindex="0"
      @click.self="onBackdropClick"
      @pointerdown="cancelAutoRoute"
      @touchstart.passive="cancelAutoRoute"
      @keydown.esc="closeSuccessModal"
    >
      <div
        class="w-full max-w-lg rounded-2xl border border-white/10 bg-[#0B1724] p-5 shadow-xl"
        style="
          padding-bottom: max(1.25rem, env(safe-area-inset-bottom));
          padding-top: max(1.25rem, env(safe-area-inset-top));
        "
        @pointerdown="cancelAutoRoute"
        @touchstart.passive="cancelAutoRoute"
        role="dialog"
        aria-modal="true"
      >
        <div class="flex items-start justify-between gap-3">
          <div>
            <div class="text-sm opacity-80">Done</div>
            <h3 class="mt-1 text-lg font-semibold">
              {{ successInfo.published ? 'Published' : 'Saved' }} — v{{ successInfo.version }}
            </h3>
            <p class="mt-1 text-sm opacity-80">
              <span class="font-medium">{{ successInfo.title }}</span>
              <span class="opacity-70"> · </span>
              <span class="opacity-70">{{ successInfo.slug }}</span>
            </p>
            <p class="mt-2 text-xs opacity-60">Opening reader…</p>
          </div>

          <button
            class="rounded-xl bg-white/10 px-4 py-2 text-sm font-medium hover:bg-white/15"
            @click="closeSuccessModal"
          >
            Close
          </button>
        </div>

        <div class="mt-4 flex flex-wrap gap-2">
          <button
            class="rounded-xl bg-white px-5 py-3 text-sm font-semibold text-black"
            @click="readNowFromModal"
          >
            Read now
          </button>

          <button
            class="rounded-xl bg-white/10 px-5 py-3 text-sm font-medium hover:bg-white/15"
            @click="copyLinkFromModal"
          >
            Copy link
          </button>

          <button
            class="rounded-xl bg-white/10 px-5 py-3 text-sm font-medium hover:bg-white/15"
            @click="uploadAnotherFromModal"
          >
            Upload another
          </button>
        </div>

        <div class="mt-4 text-xs opacity-70">
          Tap any button to stay here. Otherwise we’ll open the reader automatically.
        </div>
      </div>
    </div>

    <!-- header -->
    <div class="flex items-start justify-between gap-4">
      <div>
        <h2 class="text-lg font-semibold">Upload story</h2>
        <p class="mt-1 text-sm opacity-75">
          Import a file (Gutenberg “Plain Text UTF-8” recommended) or paste markdown.
        </p>
      </div>

      <div class="flex flex-wrap items-center justify-end gap-2">
        <input
          ref="fileInputRef"
          type="file"
          class="hidden"
          accept=".txt,.md,.markdown,.html,.htm,text/plain,text/markdown,text/html"
          @change="onFilePicked"
        />

        <button
          type="button"
          class="rounded-xl bg-white/10 px-4 py-3 text-sm font-medium hover:bg-white/15"
          @click="pickFile"
        >
          Import file
        </button>

        <button
          type="button"
          class="rounded-xl bg-white/10 px-4 py-3 text-sm font-medium disabled:opacity-60"
          :disabled="previewDisabled"
          @click="runPreview"
        >
          {{ previewButtonLabel }}
        </button>

        <button
          type="button"
          class="rounded-xl bg-white px-4 py-3 text-sm font-semibold text-black disabled:opacity-60"
          :disabled="saveDisabled"
          @click="submit"
        >
          {{ saveButtonLabel }}
        </button>
      </div>
    </div>

    <div class="mt-3 flex items-center gap-2 text-xs opacity-80">
      <input id="publish" type="checkbox" v-model="publish" class="h-4 w-4 accent-white" />
      <label for="publish">Publish immediately</label>

      <div class="ml-auto flex items-center gap-2 opacity-70">
        <div class="h-1.5 w-24 overflow-hidden rounded-full bg-white/10">
          <div class="h-full bg-white/40" :style="{ width: `${progressHint}%` }"></div>
        </div>
        <span>{{ progressHint }}%</span>
      </div>
    </div>

    <div v-if="error" class="mt-4 rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm">
      {{ error }}
    </div>
    <div v-else-if="message" class="mt-4 rounded-xl border border-white/10 bg-black/20 px-4 py-3 text-sm">
      {{ message }}
    </div>

    <div class="mt-5 grid grid-cols-1 md:grid-cols-2 gap-3">
      <label class="text-xs opacity-80">
        Title
        <input
          v-model="title"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-3 text-base outline-none focus:border-white/25"
          placeholder="The Gruffalo"
        />
      </label>

      <label class="text-xs opacity-80">
        Author
        <input
          v-model="author"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-3 text-base outline-none focus:border-white/25"
          placeholder="Julia Donaldson"
        />
      </label>

      <label class="text-xs opacity-80">
        Slug
        <input
          :value="slug"
          @input="onSlugInput(($event.target as HTMLInputElement).value)"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-3 text-base outline-none focus:border-white/25"
          placeholder="the-gruffalo"
        />
      </label>

      <label class="text-xs opacity-80">
        Source URL (optional)
        <input
          v-model="sourceUrl"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-3 text-base outline-none focus:border-white/25"
          placeholder="https://…"
        />
      </label>
    </div>

    <label class="block mt-4 text-xs opacity-80">
      Markdown
      <textarea
        v-model="markdown"
        class="mt-2 w-full min-h-80 rounded-2xl bg-black/20 border border-white/10 px-4 py-3 text-base outline-none focus:border-white/25"
        placeholder="# Title&#10;&#10;Once upon a time..."
      />
    </label>

    <!-- Preview -->
    <div v-if="preview" class="mt-6 rounded-2xl border border-white/10 bg-black/20 p-4">
      <div class="flex items-center justify-between gap-3">
        <div class="text-xs opacity-70">Preview</div>
        <div class="text-xs opacity-70">{{ previewSegmentsCount }} segments</div>
      </div>
      <div class="prose prose-invert max-w-none mt-2" v-html="preview.renderedHtml"></div>
    </div>

    <!-- Published library -->
    <div class="mt-6 rounded-2xl border border-white/10 bg-black/20 p-4">
      <div class="flex items-center justify-between gap-3">
        <div class="text-xs opacity-70">Published library</div>
      </div>

      <div v-if="!library.length" class="mt-3 text-sm opacity-70">No published items loaded yet.</div>

      <div v-else class="mt-3 grid gap-2">
        <button
          v-for="it in library"
          :key="it.slug"
          type="button"
          class="flex w-full items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/5 px-3 py-3 text-left hover:bg-white/10"
          @click="openReader(it.slug)"
        >
          <div class="min-w-0">
            <div class="truncate text-base font-medium">{{ it.title }}</div>
            <div class="truncate text-xs opacity-70">
              {{ it.slug }}<span v-if="it.author"> · {{ it.author }}</span>
            </div>
          </div>
          <div class="text-xs opacity-70">Read</div>
        </button>
      </div>
    </div>

    <!-- Admin list (drafts + published) -->
    <div class="mt-6 rounded-2xl border border-white/10 bg-black/20 p-4">
      <div class="flex items-center justify-between gap-3">
        <div class="text-xs opacity-70">All stories (admin)</div>
      </div>

      <div v-if="!allStories.length" class="mt-3 text-sm opacity-70">No stories found.</div>

      <div v-else class="mt-3 grid gap-2">
        <button
          v-for="it in allStories"
          :key="it.slug"
          type="button"
          class="flex w-full items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/5 px-3 py-3 text-left hover:bg-white/10"
          @click="openReader(it.slug)"
        >
          <div class="min-w-0">
            <div class="truncate text-base font-medium">{{ it.title }}</div>
            <div class="truncate text-xs opacity-70">
              {{ it.slug }}
              <span v-if="it.author"> · {{ it.author }}</span>
              · {{ it.isPublished ? 'published' : 'draft' }}
            </div>
          </div>
          <div class="text-xs opacity-70">Open</div>
        </button>
      </div>
    </div>
  </section>
</template>
