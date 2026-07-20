<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
} from 'vue'
import { useRoute, useRouter } from 'vue-router'
import StoryMarkdownEditor from '@/components/admin/story-studio/StoryMarkdownEditor.vue'
import StoryMetadataForm from '@/components/admin/story-studio/StoryMetadataForm.vue'
import StoryPreviewPane from '@/components/admin/story-studio/StoryPreviewPane.vue'
import StoryStudioDialog from '@/components/admin/story-studio/StoryStudioDialog.vue'
import StoryStudioState from '@/components/admin/story-studio/StoryStudioState.vue'
import StoryValidationSummary from '@/components/admin/story-studio/StoryValidationSummary.vue'
import {
  adminDraftUpsertStory,
  adminGetStory,
  adminGetVersionSource,
  adminPreview,
  getAdminValidationIssues,
  type AdminPreviewResponse,
  type AdminValidationIssue,
  type AdminVersionSource,
} from '@/lib/api'
import { authState } from '@/lib/session'
import {
  convertImportedStoryFile,
  createBlankStoryForm,
  followedStorySlug,
  importWouldReplaceSubstantialMarkdown,
  normaliseStoryForm,
  storyFormFingerprint,
  storyFormFromVersion,
  storyFormIsDirty,
  type ImportedStory,
  type StoryStudioForm,
} from '@/lib/story-studio-form'
import {
  previewIsOutdated,
  projectStoryStudioError,
  type StoryStudioError,
} from '@/lib/story-studio-navigation'

const emit = defineEmits<{ 'studio-dirty': [dirty: boolean] }>()
const route = useRoute()
const router = useRouter()

const form = ref<StoryStudioForm>(createBlankStoryForm())
const baselineFingerprint = ref(storyFormFingerprint(form.value))
const slugTouched = ref(false)
const loading = ref(false)
const loaded = ref(false)
const loadError = ref<StoryStudioError | null>(null)
const actionError = ref<StoryStudioError | null>(null)
const validationIssues = ref<AdminValidationIssue[]>([])
const preview = ref<AdminPreviewResponse | null>(null)
const previewFingerprint = ref<string | null>(null)
const previewing = ref(false)
const saving = ref(false)
const sourceVersion = ref<AdminVersionSource | null>(null)
const sourceFilename = ref<string | null>(null)
const importError = ref<string | null>(null)
const pendingImport = ref<ImportedStory | null>(null)
const importDialogOpen = ref(false)
let loadGeneration = 0
let loadController: AbortController | null = null
let previewGeneration = 0
let previewController: AbortController | null = null
let instanceAlive = true

const isNew = computed(() => route.name === 'admin-story-new')
const fingerprint = computed(() => storyFormFingerprint(form.value))
const dirty = computed(() =>
  loaded.value
    ? storyFormIsDirty(form.value, baselineFingerprint.value)
    : false,
)
const previewOutdated = computed(() =>
  previewIsOutdated(previewFingerprint.value, fingerprint.value),
)
const issuesByField = computed<Record<string, string[]>>(() => {
  const grouped: Record<string, string[]> = {}
  for (const issue of validationIssues.value) {
    const field = issue.field === 'content' ? 'markdown' : issue.field
    grouped[field] ??= []
    grouped[field].push(issue.message)
  }
  return grouped
})

watch(dirty, (value) => emit('studio-dirty', value), { immediate: true })
watch(
  () => form.value.title,
  (title) => {
    if (!isNew.value || slugTouched.value) return
    form.value = {
      ...form.value,
      slug: followedStorySlug(title, form.value.slug, slugTouched.value),
    }
  },
)

async function moveToUnlock() {
  authState.confirmLocked()
  await router.replace({
    path: '/unlock',
    query: { next: route.path },
  })
}

async function applyRequestError(caught: unknown) {
  const projected = projectStoryStudioError(caught)
  actionError.value = projected
  if (projected.kind === 'session') await moveToUnlock()
}

function resetNewEditor() {
  const blank = createBlankStoryForm()
  form.value = blank
  baselineFingerprint.value = storyFormFingerprint(blank)
  slugTouched.value = false
  sourceVersion.value = null
  sourceFilename.value = null
  preview.value = null
  previewFingerprint.value = null
  validationIssues.value = []
  actionError.value = null
  loadError.value = null
  loaded.value = true
}

function localLoadError(kind: 'not-found' | 'repair'): StoryStudioError {
  return kind === 'repair'
    ? {
        kind: 'repair',
        title: 'Source version needs attention',
        message:
          'This version cannot safely be opened as an editing source. Choose a healthy version from story details.',
        retryable: false,
      }
    : {
        kind: 'not-found',
        title: 'Version unavailable',
        message: 'The selected source version does not belong to this story.',
        retryable: false,
      }
}

async function loadEditor() {
  loadController?.abort()
  previewController?.abort()
  const controller = new AbortController()
  loadController = controller
  const generation = ++loadGeneration
  loaded.value = false
  loading.value = true
  loadError.value = null
  actionError.value = null
  emit('studio-dirty', false)

  if (isNew.value) {
    resetNewEditor()
    loading.value = false
    return
  }

  const slug = String(route.params.slug ?? '')
  try {
    const detail = await adminGetStory(slug, controller.signal)
    if (generation !== loadGeneration || controller.signal.aborted) return
    const requestedId =
      typeof route.query.fromVersion === 'string'
        ? route.query.fromVersion
        : null
    const selected = requestedId
      ? detail.versions.find((version) => version.versionId === requestedId)
      : detail.versions.find(
          (version) =>
            version.versionId === detail.draftVersion?.versionId &&
            version.health === 'ready',
        ) ??
        detail.versions.find(
          (version) =>
            version.versionId === detail.publishedVersion?.versionId &&
            version.health === 'ready',
        ) ??
        detail.versions.find((version) => version.health === 'ready')

    if (!selected) {
      loadError.value = localLoadError('not-found')
      return
    }
    if (selected.health !== 'ready') {
      loadError.value = localLoadError('repair')
      return
    }

    const source = await adminGetVersionSource(
      slug,
      selected.versionId,
      controller.signal,
    )
    if (generation !== loadGeneration || controller.signal.aborted) return
    if (source.health !== 'ready') {
      loadError.value = localLoadError('repair')
      return
    }

    const loadedForm = storyFormFromVersion(source)
    form.value = loadedForm
    baselineFingerprint.value = storyFormFingerprint(loadedForm)
    sourceVersion.value = source
    slugTouched.value = true
    sourceFilename.value = null
    preview.value = null
    previewFingerprint.value = null
    validationIssues.value = []
    loaded.value = true
  } catch (caught) {
    if (controller.signal.aborted || generation !== loadGeneration) return
    const projected = projectStoryStudioError(caught)
    loadError.value = projected
    if (projected.kind === 'session') await moveToUnlock()
  } finally {
    if (generation === loadGeneration) loading.value = false
  }
}

function focusIssue(field: string) {
  const ids: Record<string, string> = {
    title: 'story-title',
    author: 'story-author',
    slug: 'story-slug',
    language: 'story-language',
    rights: 'story-rights',
    sourceUrl: 'story-source-url',
    markdown: 'story-markdown',
    content: 'story-markdown',
  }
  const target = document.getElementById(ids[field] ?? 'story-markdown')
  target?.focus({ preventScroll: false })
}

async function runPreview() {
  if (previewing.value) return
  previewController?.abort()
  const controller = new AbortController()
  previewController = controller
  const generation = ++previewGeneration
  const requestFingerprint = fingerprint.value
  previewing.value = true
  actionError.value = null
  validationIssues.value = []
  try {
    const result = await adminPreview(normaliseStoryForm(form.value), controller.signal)
    if (generation !== previewGeneration || controller.signal.aborted) return
    if (requestFingerprint !== fingerprint.value) return
    preview.value = result
    previewFingerprint.value = requestFingerprint
  } catch (caught) {
    if (controller.signal.aborted || generation !== previewGeneration) return
    validationIssues.value = getAdminValidationIssues(caught) ?? []
    await applyRequestError(caught)
    if (validationIssues.value.length) {
      await nextTick()
      document.querySelector<HTMLElement>('.validation-summary')?.focus()
    }
  } finally {
    if (generation === previewGeneration) previewing.value = false
  }
}

async function saveDraft() {
  if (saving.value) return
  saving.value = true
  actionError.value = null
  validationIssues.value = []
  try {
    const result = await adminDraftUpsertStory(normaliseStoryForm(form.value))
    if (!instanceAlive) return
    baselineFingerprint.value = fingerprint.value
    emit('studio-dirty', false)
    await router.replace({
      name: 'admin-story-detail',
      params: { slug: result.slug },
      query: { saved: result.outcome, version: String(result.version) },
    })
  } catch (caught) {
    if (!instanceAlive) return
    validationIssues.value = getAdminValidationIssues(caught) ?? []
    await applyRequestError(caught)
    if (validationIssues.value.length) {
      await nextTick()
      document.querySelector<HTMLElement>('.validation-summary')?.focus()
    }
  } finally {
    if (instanceAlive) saving.value = false
  }
}

function applyImport(imported: ImportedStory) {
  const title = form.value.title.trim() ? form.value.title : imported.title
  const author = form.value.author.trim() ? form.value.author : imported.author
  form.value = {
    ...form.value,
    title,
    author,
    slug: followedStorySlug(title, form.value.slug, slugTouched.value),
    markdown: imported.markdown,
  }
  sourceFilename.value = imported.filename
  previewFingerprint.value = preview.value ? previewFingerprint.value : null
  importError.value = null
  pendingImport.value = null
  importDialogOpen.value = false
}

async function importFile(file: File) {
  importError.value = null
  try {
    const imported = convertImportedStoryFile({
      filename: file.name,
      mediaType: file.type,
      text: await file.text(),
    })
    if (
      importWouldReplaceSubstantialMarkdown(
        form.value.markdown,
        imported.markdown,
      )
    ) {
      pendingImport.value = imported
      importDialogOpen.value = true
      return
    }
    applyImport(imported)
  } catch {
    importError.value =
      'That file could not be imported. Choose a readable text, Markdown or HTML file.'
  }
}

function confirmImport() {
  if (pendingImport.value) applyImport(pendingImport.value)
}

watch(
  () => route.fullPath,
  () => void loadEditor(),
  { immediate: true },
)

onBeforeUnmount(() => {
  instanceAlive = false
  loadGeneration += 1
  previewGeneration += 1
  loadController?.abort()
  previewController?.abort()
  emit('studio-dirty', false)
})
</script>

<template>
  <div>
    <StoryStudioState
      v-if="loading && !loaded"
      kind="loading"
      title="Opening the editor"
      message="Loading the selected immutable source version."
    />
    <StoryStudioState
      v-else-if="loadError"
      :kind="loadError.kind === 'repair' ? 'repair' : loadError.kind === 'forbidden' ? 'forbidden' : 'error'"
      :title="loadError.title"
      :message="loadError.message"
      :action-label="loadError.retryable ? 'Try again' : 'Return to story'"
      @action="loadError.retryable ? loadEditor() : router.push(isNew ? '/admin/stories' : `/admin/stories/${encodeURIComponent(String(route.params.slug))}`)"
    />
    <template v-else-if="loaded">
      <header class="studio-page-heading editor-heading">
        <div>
          <p class="studio-page-heading__eyebrow">{{ isNew ? 'New story' : 'New immutable draft' }}</p>
          <h1>{{ isNew ? 'Create a story' : `Edit ${form.title || 'story'}` }}</h1>
          <p class="studio-page-heading__summary">
            <template v-if="sourceVersion">Starting from version {{ sourceVersion.version }}. </template>
            Saving creates a new version. Existing published and historical versions remain unchanged.
          </p>
        </div>
        <div class="editor-heading__actions">
          <button type="button" class="studio-button studio-button--quiet" :disabled="previewing || saving" @click="runPreview">
            {{ previewing ? 'Previewing…' : 'Preview' }}
          </button>
          <button type="button" class="studio-button studio-button--primary" :disabled="saving || previewing" @click="saveDraft">
            {{ saving ? 'Saving…' : 'Save draft' }}
          </button>
        </div>
      </header>

      <p v-if="dirty" class="editor-dirty">Unsaved changes</p>
      <div v-if="actionError" class="editor-error" role="alert">
        <strong>{{ actionError.title }}</strong>
        <p>{{ actionError.message }}</p>
        <p v-if="actionError.kind === 'repair'">Open story details and choose a healthy source, or create a fresh story version.</p>
      </div>
      <StoryValidationSummary :issues="validationIssues" @focus="focusIssue" />

      <section class="studio-panel editor-metadata">
        <StoryMetadataForm
          v-model="form"
          :fixed-slug="!isNew"
          :issues-by-field="issuesByField"
          @slug-input="slugTouched = true"
        />
      </section>

      <div class="editor-workspace">
        <section class="studio-panel">
          <StoryMarkdownEditor
            :model-value="form.markdown"
            :source-filename="sourceFilename"
            :error="importError"
            @update:model-value="form = { ...form, markdown: $event }"
            @import="importFile"
          />
        </section>
        <section class="studio-panel editor-preview">
          <StoryPreviewPane
            :preview="preview"
            :loading="previewing"
            :outdated="previewOutdated"
            @focus="focusIssue"
          />
        </section>
      </div>

      <footer class="editor-footer">
        <p>Saving does not publish or open the Reader.</p>
        <div>
          <button type="button" class="studio-button studio-button--quiet" :disabled="previewing || saving" @click="runPreview">Preview</button>
          <button type="button" class="studio-button studio-button--primary" :disabled="saving || previewing" @click="saveDraft">{{ saving ? 'Saving…' : 'Save draft' }}</button>
        </div>
      </footer>

      <StoryStudioDialog
        :open="importDialogOpen"
        title="Replace the current Markdown?"
        description="This import will replace substantial unsaved Markdown in the editor."
        confirm-label="Replace Markdown"
        danger
        @confirm="confirmImport"
        @cancel="importDialogOpen = false; pendingImport = null"
      >
        <p>The file is only read in this browser and will not be saved until you choose Save draft.</p>
      </StoryStudioDialog>
    </template>
  </div>
</template>

<style scoped>
.editor-heading { align-items: center; }
.editor-heading__actions { display: flex; flex-wrap: wrap; gap: 0.6rem; }
.editor-dirty { width: fit-content; margin: -0.5rem 0 1rem; border: 1px solid var(--panda-warning); border-radius: var(--panda-radius-pill); background: var(--panda-warning-surface); color: var(--panda-warning); padding: 0.35rem 0.7rem; font-size: 0.78rem; font-weight: 720; }
.editor-error { margin-bottom: 1rem; border: 1px solid var(--panda-danger); border-radius: var(--panda-radius-compact); background: var(--panda-danger-surface); padding: 0.9rem 1rem; color: var(--panda-danger); }
.editor-error p { margin-top: 0.25rem; line-height: 1.5; }
.editor-metadata { margin-top: 1rem; }
.editor-workspace { display: grid; grid-template-columns: minmax(0, 1.08fr) minmax(20rem, 0.92fr); align-items: start; gap: 1rem; margin-top: 1rem; }
.editor-preview { position: sticky; top: 6rem; }
.editor-footer { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-top: 1rem; border-top: 1px solid var(--studio-line); padding-top: 1rem; }
.editor-footer p { color: var(--studio-muted); font-size: 0.85rem; }
.editor-footer > div { display: flex; gap: 0.6rem; }

@media (max-width: 900px) {
  .editor-workspace { grid-template-columns: 1fr; }
  .editor-preview { position: static; }
}

@media (max-width: 640px) {
  .editor-heading__actions,
  .editor-heading__actions .studio-button,
  .editor-footer,
  .editor-footer > div,
  .editor-footer .studio-button { width: 100%; }
  .editor-footer { align-items: stretch; flex-direction: column; }
}
</style>
