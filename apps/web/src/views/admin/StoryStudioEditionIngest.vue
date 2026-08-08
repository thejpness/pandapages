<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import StoryMetadataForm from '@/components/admin/story-studio/StoryMetadataForm.vue'
import StoryStudioState from '@/components/admin/story-studio/StoryStudioState.vue'
import StoryValidationSummary from '@/components/admin/story-studio/StoryValidationSummary.vue'
import { adminGetStory, adminIngestEditionBundle, getAdminValidationIssues, type AdminValidationIssue } from '@/lib/api'
import { editionBundleDefinitions, editionBundleInputs, inferEditionBundleTitle, parseEditionBundleFiles, type EditionBundleSelection } from '@/lib/story-edition-bundle'
import { createBlankStoryForm, followedStorySlug, normaliseStoryForm, storyFormFingerprint, storyFormFromStory, type StoryStudioForm } from '@/lib/story-studio-form'
import { projectStoryStudioError, type StoryStudioError } from '@/lib/story-studio-navigation'

const emit = defineEmits<{ 'studio-dirty': [dirty: boolean] }>()
const route = useRoute()
const router = useRouter()
const form = ref<StoryStudioForm>(createBlankStoryForm())
const baselineFingerprint = ref(storyFormFingerprint(form.value, 'classic'))
const bundle = ref<EditionBundleSelection | null>(null)
const slugTouched = ref(false)
const loading = ref(false)
const loaded = ref(false)
const loadError = ref<StoryStudioError | null>(null)
const actionError = ref<StoryStudioError | null>(null)
const validationIssues = ref<AdminValidationIssue[]>([])
const fileError = ref('')
const saving = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
let generation = 0
let controller: AbortController | null = null
let instanceAlive = true

const isExisting = computed(() => route.name === 'admin-story-ingest-existing')
const metadataFingerprint = computed(() => storyFormFingerprint({ ...form.value, markdown: '' }, 'classic'))
const dirty = computed(() => loaded.value && (bundle.value !== null || metadataFingerprint.value !== baselineFingerprint.value))
const selectedByKey = computed(() => new Map((bundle.value?.items ?? []).map((item) => [item.editionKey, item])))
const issuesByField = computed<Record<string, string[]>>(() => {
  const grouped: Record<string, string[]> = {}
  for (const issue of validationIssues.value) {
    if (issue.field.startsWith('editions.')) continue
    grouped[issue.field] ??= []
    grouped[issue.field].push(issue.message)
  }
  return grouped
})

watch(dirty, (value) => emit('studio-dirty', value), { immediate: true })
watch(() => form.value.title, (title) => {
  if (isExisting.value || slugTouched.value) return
  form.value = { ...form.value, slug: followedStorySlug(title, form.value.slug, slugTouched.value) }
})

async function moveToSignIn() {
  await router.replace({ path: '/account/login', query: { next: route.fullPath } })
}

function resetNewIngest() {
  const blank = createBlankStoryForm()
  form.value = blank
  baselineFingerprint.value = storyFormFingerprint({ ...blank, markdown: '' }, 'classic')
  bundle.value = null
  slugTouched.value = false
  fileError.value = ''
  validationIssues.value = []
  actionError.value = null
  loadError.value = null
  loaded.value = true
}

async function loadIngest() {
  controller?.abort()
  controller = new AbortController()
  const activeGeneration = ++generation
  loading.value = true
  loaded.value = false
  loadError.value = null
  actionError.value = null
  validationIssues.value = []
  bundle.value = null
  emit('studio-dirty', false)
  if (!isExisting.value) {
    resetNewIngest()
    loading.value = false
    return
  }
  try {
    const story = await adminGetStory(String(route.params.slug ?? ''), controller.signal)
    if (activeGeneration !== generation || controller.signal.aborted) return
    const loadedForm = storyFormFromStory(story)
    form.value = loadedForm
    baselineFingerprint.value = storyFormFingerprint({ ...loadedForm, markdown: '' }, 'classic')
    slugTouched.value = true
    fileError.value = ''
    loaded.value = true
  } catch (caught) {
    if (controller.signal.aborted || activeGeneration !== generation) return
    const projected = projectStoryStudioError(caught)
    loadError.value = projected
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    if (activeGeneration === generation) loading.value = false
  }
}

function chooseFiles() { fileInput.value?.click() }

async function filesChosen(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length === 0) return
  fileError.value = ''
  validationIssues.value = []
  try {
    const localFiles = await Promise.all(files.map(async (file) => ({ name: file.name, text: await file.text() })))
    const selection = parseEditionBundleFiles(localFiles)
    bundle.value = selection
    if (!isExisting.value && !form.value.title.trim()) {
      const inferredTitle = inferEditionBundleTitle(selection)
      if (inferredTitle) {
        form.value = { ...form.value, title: inferredTitle, slug: followedStorySlug(inferredTitle, form.value.slug, slugTouched.value) }
      }
    }
  } catch (caught) {
    bundle.value = null
    fileError.value = caught instanceof Error ? caught.message : 'The five edition files could not be read.'
  }
}

function focusIssue(field: string) {
  if (field.startsWith('editions.')) { document.getElementById('edition-bundle-choose')?.focus(); return }
  const ids: Record<string, string> = { title: 'story-title', author: 'story-author', slug: 'story-slug', language: 'story-language', rights: 'story-rights', sourceUrl: 'story-source-url', editions: 'edition-bundle-choose' }
  document.getElementById(ids[field] ?? 'edition-bundle-choose')?.focus()
}

async function ingestBundle() {
  if (!bundle.value || saving.value) return
  saving.value = true
  actionError.value = null
  validationIssues.value = []
  fileError.value = ''
  try {
    const metadata = normaliseStoryForm({ ...form.value, markdown: '' }, 'classic')
    const result = await adminIngestEditionBundle({
      slug: metadata.slug, title: metadata.title, author: metadata.author,
      language: metadata.language, sourceUrl: metadata.sourceUrl, rights: metadata.rights,
      editions: editionBundleInputs(bundle.value),
    })
    if (!instanceAlive) return
    const created = result.results.filter((item) => item.outcome === 'created').length
    const reused = result.results.length - created
    baselineFingerprint.value = metadataFingerprint.value
    bundle.value = null
    emit('studio-dirty', false)
    await router.replace({ name: 'admin-story-detail', params: { slug: result.slug }, query: { ingested: '1', created: String(created), reused: String(reused) } })
  } catch (caught) {
    if (!instanceAlive) return
    validationIssues.value = getAdminValidationIssues(caught) ?? []
    const projected = projectStoryStudioError(caught)
    actionError.value = projected
    if (projected.kind === 'session') await moveToSignIn()
    if (validationIssues.value.length) {
      await nextTick()
      document.querySelector<HTMLElement>('.validation-summary')?.focus()
    }
  } finally {
    if (instanceAlive) saving.value = false
  }
}

watch(() => route.fullPath, () => void loadIngest(), { immediate: true })
onBeforeUnmount(() => { instanceAlive = false; generation += 1; controller?.abort(); emit('studio-dirty', false) })
</script>

<template>
  <div>
    <StoryStudioState v-if="loading && !loaded" kind="loading" title="Opening five-edition ingest" message="Preparing the Story Studio bundle workspace." />
    <StoryStudioState v-else-if="loadError" :kind="loadError.kind === 'repair' ? 'repair' : loadError.kind === 'forbidden' ? 'forbidden' : 'error'" :title="loadError.title" :message="loadError.message" :action-label="loadError.retryable ? 'Try again' : 'Return to stories'" @action="loadError.retryable ? loadIngest() : router.push('/admin/stories')" />
    <template v-else-if="loaded">
      <header class="studio-page-heading ingest-heading">
        <div><p class="studio-page-heading__eyebrow">Five-edition ingest</p><h1>{{ isExisting ? `Import editions for ${form.title}` : 'Import five reading editions' }}</h1><p class="studio-page-heading__summary">Select the five Markdown files produced by the Panda Pages story-generation workflow. The bundle is saved atomically as five independent drafts. Nothing is published.</p></div>
        <button type="button" class="studio-button studio-button--primary" :disabled="!bundle || saving" @click="ingestBundle">{{ saving ? 'Ingesting…' : 'Ingest five drafts' }}</button>
      </header>
      <p v-if="dirty" class="ingest-dirty">Unsaved bundle</p>
      <div v-if="actionError" class="ingest-error" role="alert"><strong>{{ actionError.title }}</strong><p>{{ actionError.message }}</p></div>
      <StoryValidationSummary :issues="validationIssues" @focus="focusIssue" />
      <section class="studio-panel ingest-metadata"><StoryMetadataForm v-model="form" :fixed-slug="isExisting" :issues-by-field="issuesByField" @slug-input="slugTouched = true" /></section>
      <section class="studio-panel bundle-panel" aria-labelledby="bundle-files-title">
        <div class="bundle-panel__heading"><div><h2 id="bundle-files-title">Five edition files</h2><p>Filenames are part of the ingest contract. Select all five together; no file is uploaded until you confirm ingest.</p></div><input ref="fileInput" id="edition-bundle-files" class="studio-visually-hidden" type="file" multiple accept=".md,text/markdown,text/plain" aria-label="Choose five edition Markdown files" @change="filesChosen" /><button id="edition-bundle-choose" type="button" class="studio-button studio-button--quiet" @click="chooseFiles">Choose five Markdown files</button></div>
        <p v-if="fileError" class="studio-field__error bundle-panel__error" role="alert">{{ fileError }}</p>
        <ol class="bundle-files">
          <li v-for="definition in editionBundleDefinitions" :key="definition.editionKey" class="bundle-file" :class="{ 'bundle-file--ready': selectedByKey.has(definition.editionKey) }">
            <div><strong>{{ definition.label }}</strong><code>{{ definition.filename }}</code></div>
            <span v-if="selectedByKey.get(definition.editionKey)">{{ selectedByKey.get(definition.editionKey)?.characterCount.toLocaleString('en-GB') }} characters</span><span v-else>Waiting for file</span>
          </li>
        </ol>
        <div class="bundle-panel__boundary"><strong>Atomic draft ingest</strong><p>If any edition fails validation, the transaction rolls back and none of the five draft pointers or versions change. Canonical source material remains a separate Story Studio lifecycle.</p></div>
      </section>
      <footer class="ingest-footer"><button type="button" class="studio-button studio-button--quiet" @click="router.push(isExisting ? `/admin/stories/${encodeURIComponent(String(route.params.slug))}` : '/admin/stories')">Cancel</button><button type="button" class="studio-button studio-button--primary" :disabled="!bundle || saving" @click="ingestBundle">{{ saving ? 'Ingesting…' : 'Ingest five drafts' }}</button></footer>
    </template>
  </div>
</template>

<style scoped>
.ingest-heading{align-items:center}.ingest-dirty{width:fit-content;margin:-.5rem 0 1rem;border:1px solid var(--panda-warning);border-radius:var(--panda-radius-pill);background:var(--panda-warning-surface);color:var(--panda-warning);padding:.35rem .7rem;font-size:.78rem;font-weight:720}.ingest-error{margin-bottom:1rem;border:1px solid var(--panda-danger);border-radius:var(--panda-radius-card);background:var(--panda-danger-surface);color:var(--panda-danger);padding:.9rem 1rem}.ingest-error p{margin-top:.3rem}.bundle-panel{margin-top:1rem}.bundle-panel__heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.bundle-panel h2{font-family:var(--panda-serif);font-size:1.2rem;font-weight:650}.bundle-panel__heading p{max-width:50rem;margin-top:.3rem;color:var(--studio-muted);line-height:1.5}.bundle-panel__error{margin-top:.8rem}.bundle-files{display:grid;grid-template-columns:repeat(5,minmax(10rem,1fr));gap:.65rem;margin-top:1rem;overflow-x:auto;padding-bottom:.2rem}.bundle-file{display:flex;min-width:0;flex-direction:column;justify-content:space-between;gap:.8rem;border:1px solid var(--studio-line-strong);border-radius:var(--panda-radius-card);background:var(--panda-paper);padding:.8rem}.bundle-file--ready{border-color:var(--panda-success);background:var(--panda-success-surface)}.bundle-file strong,.bundle-file code{display:block}.bundle-file code{overflow-wrap:anywhere;margin-top:.35rem;color:var(--studio-muted);font-size:.72rem}.bundle-file>span{color:var(--studio-muted);font-size:.75rem}.bundle-panel__boundary{margin-top:1rem;border-left:3px solid var(--panda-line-strong);padding-left:.8rem}.bundle-panel__boundary p{margin-top:.25rem;color:var(--studio-muted);line-height:1.5}.ingest-footer{display:flex;justify-content:flex-end;gap:.6rem;margin-top:1rem;border-top:1px solid var(--studio-line);padding-top:1rem}@media(max-width:900px){.bundle-files{grid-template-columns:repeat(2,minmax(14rem,1fr))}}@media(max-width:620px){.ingest-heading,.bundle-panel__heading{align-items:stretch;flex-direction:column}.bundle-files{grid-template-columns:1fr;overflow:visible}.ingest-footer{display:grid;grid-template-columns:1fr}}
</style>
