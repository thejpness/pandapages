<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  adminPreview,
  adminDraftUpsertStory,
  adminPublishStory,
  type AdminPreviewResponse,
  type AdminDraftUpsertResponse,
} from '@/lib/api'

function slugify(input: string): string {
  return input
    .toLowerCase()
    .trim()
    .replace(/['"]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80)
}

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

// Auto-slug from title until user edits slug manually
const slugTouched = ref(false)
watch(title, (t) => {
  if (!slugTouched.value) slug.value = slugify(t)
})

function onSlugInput(v: string) {
  slugTouched.value = true
  slug.value = v
}

const canPreview = computed(() => markdown.value.trim().length > 0)
const canSave = computed(() => {
  return (
    title.value.trim().length > 0 &&
    slug.value.trim().length > 0 &&
    markdown.value.trim().length > 0
  )
})

function toMsg(e: unknown, fallback: string) {
  const anyErr = e as any
  return anyErr?.message || fallback
}

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
    preview.value = await adminPreview({ markdown: markdown.value })
    message.value = `Preview ok (${preview.value.segments.length} segments)`
  } catch (e) {
    error.value = toMsg(e, 'Preview failed')
  } finally {
    previewing.value = false
  }
}

async function submit() {
  error.value = null
  message.value = null

  if (!canSave.value) {
    error.value = 'Title, slug, and markdown are required.'
    return
  }

  saving.value = true
  try {
    // 1) Upsert draft
    const res = await adminDraftUpsertStory({
      slug: slug.value.trim(),
      title: title.value.trim(),
      author: author.value.trim() || null,
      markdown: markdown.value,
      sourceUrl: sourceUrl.value.trim() || null,
    })

    lastDraft.value = res
    message.value = `Draft saved: v${res.version} (${res.segmentsCount} segments)`

    // keep preview in sync with stored draft
    preview.value = { renderedHtml: res.renderedHtml, segments: [] }

    // 2) Optional publish
    if (publish.value) {
      await adminPublishStory(res.slug, res.storyVersionId)
      message.value = `Saved & published: v${res.version}`
    }
  } catch (e) {
    error.value = toMsg(e, 'Save failed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="rounded-2xl border border-white/10 bg-white/5 p-5">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h2 class="text-lg font-semibold">Upload story</h2>
        <p class="mt-1 text-sm opacity-75">
          Paste markdown (frontmatter optional). Save as draft, preview, then publish.
        </p>
      </div>

      <div class="flex items-center gap-2">
        <button
          type="button"
          class="rounded-xl bg-white/10 px-4 py-2 text-sm font-medium disabled:opacity-60"
          :disabled="previewing || !canPreview"
          @click="runPreview"
        >
          {{ previewing ? 'Previewing…' : 'Preview' }}
        </button>

        <button
          type="button"
          class="rounded-xl bg-white text-black px-4 py-2 text-sm font-medium disabled:opacity-60"
          :disabled="saving || !canSave"
          @click="submit"
        >
          {{ saving ? 'Saving…' : (publish ? 'Save & Publish' : 'Save Draft') }}
        </button>
      </div>
    </div>

    <div class="mt-3 flex items-center gap-2 text-xs opacity-80">
      <input id="publish" type="checkbox" v-model="publish" class="h-4 w-4 accent-white" />
      <label for="publish">Publish immediately</label>
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
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-2 text-sm outline-none focus:border-white/25"
          placeholder="The Gruffalo"
        />
      </label>

      <label class="text-xs opacity-80">
        Author
        <input
          v-model="author"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-2 text-sm outline-none focus:border-white/25"
          placeholder="Julia Donaldson"
        />
      </label>

      <label class="text-xs opacity-80">
        Slug
        <input
          :value="slug"
          @input="onSlugInput(($event.target as HTMLInputElement).value)"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-2 text-sm outline-none focus:border-white/25"
          placeholder="the-gruffalo"
        />
      </label>

      <label class="text-xs opacity-80">
        Source URL (optional)
        <input
          v-model="sourceUrl"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-2 text-sm outline-none focus:border-white/25"
          placeholder="https://…"
        />
      </label>
    </div>

    <label class="block mt-4 text-xs opacity-80">
      Markdown
      <textarea
        v-model="markdown"
        class="mt-2 w-full min-h-80 rounded-2xl bg-black/20 border border-white/10 px-4 py-3 text-sm outline-none focus:border-white/25"
        placeholder="# Title&#10;&#10;Once upon a time..."
      />
    </label>

    <div v-if="preview" class="mt-6 rounded-2xl border border-white/10 bg-black/20 p-4">
      <div class="flex items-center justify-between gap-3">
        <div class="text-xs opacity-70">Preview</div>
        <div v-if="preview.segments?.length" class="text-xs opacity-70">
          {{ preview.segments.length }} segments
        </div>
        <div v-else-if="lastDraft" class="text-xs opacity-70">
          {{ lastDraft.segmentsCount }} segments
        </div>
      </div>

      <div class="prose prose-invert max-w-none mt-2" v-html="preview.renderedHtml"></div>
    </div>
  </section>
</template>
