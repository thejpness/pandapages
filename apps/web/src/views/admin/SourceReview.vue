<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import StoryStudioState from '@/components/admin/story-studio/StoryStudioState.vue'
import {
  adminGetSourceAcquisition,
  adminGetSourceProviderWork,
  adminListSourceAcquisitions,
  adminPersistSourceAcquisition,
  adminSearchSourceProvider,
  adminUpdateSourceAcquisitionEditorialReview,
  adminUpdateSourceAcquisitionRightsReview,
  getAPIErrorStatus,
  type AdminSourceAcquisitionDetail,
  type AdminSourceAcquisitionReviewStatus,
  type AdminSourceAcquisitionSummary,
  type AdminSourceProviderWork,
} from '@/lib/api'

type WorkspacePanel = 'find' | 'saved'
type ReviewDimension = 'rights' | 'editorial'

const provider = 'project-gutenberg' as const
const router = useRouter()
const panel = ref<WorkspacePanel>('find')
const query = ref('')
const searching = ref(false)
const searchStarted = ref(false)
const works = ref<AdminSourceProviderWork[]>([])
const selectedWork = ref<AdminSourceProviderWork | null>(null)
const selecting = ref(false)
const saving = ref(false)
const savedLoading = ref(false)
const saved = ref<AdminSourceAcquisitionSummary[]>([])
const selectedAcquisition = ref<AdminSourceAcquisitionDetail | null>(null)
const detailLoading = ref(false)
const reviewSaving = ref<ReviewDimension | null>(null)
const rightsStatus = ref<AdminSourceAcquisitionReviewStatus>('pending')
const rightsNote = ref('')
const editorialStatus = ref<AdminSourceAcquisitionReviewStatus>('pending')
const editorialNote = ref('')
const error = ref('')
const message = ref('')
let searchController: AbortController | null = null
let savedController: AbortController | null = null
let detailController: AbortController | null = null

const reviewReady = computed(() =>
  selectedAcquisition.value?.review.rights.status === 'approved' &&
  selectedAcquisition.value.review.editorial.status === 'approved',
)

function contributorText(work: { contributors: readonly { name: string; role: string }[] }) {
  return work.contributors.map(({ name, role }) => role ? `${name} (${role})` : name).join(', ') || 'No contributor metadata'
}

function statusLabel(status: AdminSourceAcquisitionReviewStatus) {
  return status[0].toUpperCase() + status.slice(1)
}

function formatSavedAt(value: string) {
  return new Intl.DateTimeFormat('en-GB', {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: 'UTC',
  }).format(new Date(value)) + ' UTC'
}

async function moveToSignIn() {
  await router.replace({ path: '/account/login', query: { next: '/admin/source-review' } })
}

async function showError(caught: unknown) {
  if (getAPIErrorStatus(caught) === 401) {
    await moveToSignIn()
    return
  }
  const code = typeof caught === 'object' && caught !== null && 'code' in caught
    ? (caught as { code?: unknown }).code
    : undefined
  const messages: Record<string, string> = {
    source_provider_query_invalid: 'Enter a more specific Project Gutenberg search.',
    source_provider_work_not_found: 'That Project Gutenberg work is no longer available.',
    source_provider_timeout: 'Project Gutenberg took too long to respond. Try again.',
    source_provider_unavailable: 'Project Gutenberg is unavailable right now. Try again later.',
    source_provider_representation_unavailable: 'This work has no supported plain-text source representation.',
    source_provider_content_too_large: 'This source is too large to save for review.',
    source_provider_content_invalid: 'This source could not be validated safely.',
    source_provider_normalisation_failed: 'This source could not be prepared safely for review.',
    source_acquisition_not_found: 'That saved source could not be found.',
    source_acquisition_review_invalid: 'Check the review status and rationale, then try again.',
  }
  error.value = typeof code === 'string' && messages[code]
    ? messages[code]
    : 'Story Studio could not complete that source review action. Try again.'
}

function clearFeedback() {
  error.value = ''
  message.value = ''
}

async function search() {
  const term = query.value.trim()
  searchStarted.value = true
  clearFeedback()
  selectedWork.value = null
  if (term.length < 2) {
    works.value = []
    error.value = 'Enter at least two characters to search Project Gutenberg.'
    return
  }
  searchController?.abort()
  searchController = new AbortController()
  searching.value = true
  try {
    const response = await adminSearchSourceProvider(provider, term, searchController.signal)
    if (!searchController.signal.aborted) works.value = response.results
  } catch (caught) {
    if (!searchController.signal.aborted) await showError(caught)
  } finally {
    if (!searchController.signal.aborted) searching.value = false
  }
}

async function selectWork(work: AdminSourceProviderWork) {
  clearFeedback()
  selectedWork.value = null
  selecting.value = true
  try {
    selectedWork.value = await adminGetSourceProviderWork(provider, work.externalId)
  } catch (caught) {
    await showError(caught)
  } finally {
    selecting.value = false
  }
}

function applyReviewDrafts(detail: AdminSourceAcquisitionDetail) {
  rightsStatus.value = detail.review.rights.status
  rightsNote.value = detail.review.rights.note ?? ''
  editorialStatus.value = detail.review.editorial.status
  editorialNote.value = detail.review.editorial.note ?? ''
}

async function loadSaved() {
  savedController?.abort()
  savedController = new AbortController()
  savedLoading.value = true
  try {
    const response = await adminListSourceAcquisitions(savedController.signal)
    if (!savedController.signal.aborted) saved.value = response.items
  } catch (caught) {
    if (!savedController.signal.aborted) await showError(caught)
  } finally {
    if (!savedController.signal.aborted) savedLoading.value = false
  }
}

async function openSaved(id: string, preserveFeedback = false) {
  if (!preserveFeedback) clearFeedback()
  detailController?.abort()
  detailController = new AbortController()
  detailLoading.value = true
  try {
    const detail = await adminGetSourceAcquisition(id, detailController.signal)
    if (detailController.signal.aborted) return
    selectedAcquisition.value = detail
    applyReviewDrafts(detail)
    panel.value = 'saved'
  } catch (caught) {
    if (!detailController.signal.aborted) await showError(caught)
  } finally {
    if (!detailController.signal.aborted) detailLoading.value = false
  }
}

async function saveForReview() {
  const work = selectedWork.value
  if (!work || saving.value) return
  clearFeedback()
  saving.value = true
  try {
    const result = await adminPersistSourceAcquisition(provider, work.externalId)
    message.value = result.outcome === 'created'
      ? 'Saved for source review.'
      : 'This exact saved source already exists. Opening it for review.'
    await loadSaved()
    await openSaved(result.acquisition.id, true)
  } catch (caught) {
    await showError(caught)
  } finally {
    saving.value = false
  }
}

function setDraftStatus(dimension: ReviewDimension, status: AdminSourceAcquisitionReviewStatus) {
  if (dimension === 'rights') {
    rightsStatus.value = status
    if (status === 'pending') rightsNote.value = ''
    return
  }
  editorialStatus.value = status
  if (status === 'pending') editorialNote.value = ''
}

async function saveReview(dimension: ReviewDimension) {
  const detail = selectedAcquisition.value
  if (!detail || reviewSaving.value) return
  const status = dimension === 'rights' ? rightsStatus.value : editorialStatus.value
  const note = dimension === 'rights' ? rightsNote.value.trim() : editorialNote.value.trim()
  clearFeedback()
  if (status !== 'pending' && !note) {
    error.value = 'Add a rationale before approving or rejecting a source.'
    return
  }
  reviewSaving.value = dimension
  try {
    const summary = dimension === 'rights'
      ? await adminUpdateSourceAcquisitionRightsReview(detail.id, { status, note })
      : await adminUpdateSourceAcquisitionEditorialReview(detail.id, { status, note })
    selectedAcquisition.value = { ...summary, sourceText: detail.sourceText }
    applyReviewDrafts(selectedAcquisition.value)
    saved.value = saved.value.map((item) => item.id === summary.id ? summary : item)
    message.value = `${dimension === 'rights' ? 'Rights' : 'Source quality'} review updated.`
  } catch (caught) {
    await showError(caught)
  } finally {
    reviewSaving.value = null
  }
}

function changePanel(next: WorkspacePanel) {
  panel.value = next
  clearFeedback()
  if (next === 'saved') void loadSaved()
}

function movePanel(event: KeyboardEvent, next: WorkspacePanel) {
  if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
  event.preventDefault()
  changePanel(next)
  requestAnimationFrame(() => document.getElementById(`source-review-tab-${next}`)?.focus())
}

onMounted(loadSaved)
onBeforeUnmount(() => {
  searchController?.abort()
  savedController?.abort()
  detailController?.abort()
})
</script>

<template>
  <div class="source-review">
    <header class="studio-page-heading">
      <div>
        <p class="studio-page-heading__eyebrow">Story Studio</p>
        <h1>Source review</h1>
        <p class="studio-page-heading__summary">
          Find external source material, save exact acquisitions, and review them before any future canonical-source promotion.
        </p>
      </div>
    </header>

    <div class="source-review__tabs" role="tablist" aria-label="Source review workspace">
      <button id="source-review-tab-find" type="button" role="tab" aria-controls="source-review-panel-find" :aria-selected="panel === 'find'" :tabindex="panel === 'find' ? 0 : -1" @click="changePanel('find')" @keydown="movePanel($event, 'saved')">Find a source</button>
      <button id="source-review-tab-saved" type="button" role="tab" aria-controls="source-review-panel-saved" :aria-selected="panel === 'saved'" :tabindex="panel === 'saved' ? 0 : -1" @click="changePanel('saved')" @keydown="movePanel($event, 'find')">Saved sources</button>
    </div>

    <p v-if="message" class="source-review__message" role="status">{{ message }}</p>
    <p v-if="error" class="source-review__error" role="alert">{{ error }}</p>

    <section v-if="panel === 'find'" id="source-review-panel-find" class="source-review__find" role="tabpanel" aria-labelledby="source-review-tab-find">
      <div class="studio-panel">
        <h2 id="find-source-title">Search Project Gutenberg</h2>
        <p class="source-review__intro">Search trusted provider metadata to identify the exact work. Saving is a separate, deliberate action.</p>
        <form class="source-review__search" @submit.prevent="search">
          <div class="studio-field">
            <label for="source-provider-search">Search Project Gutenberg</label>
            <input id="source-provider-search" v-model="query" type="search" autocomplete="off" placeholder="Title or author" :disabled="searching" />
          </div>
          <button type="submit" class="studio-button studio-button--primary" :disabled="searching">{{ searching ? 'Searching…' : 'Search' }}</button>
        </form>
      </div>

      <StoryStudioState v-if="searching" kind="loading" title="Searching Project Gutenberg" message="Finding provider works." />
      <StoryStudioState v-else-if="searchStarted && works.length === 0 && !error" kind="empty" title="No matching works" message="Try another title or author search." />
      <section v-else-if="works.length" class="source-review__results" aria-labelledby="source-results-title">
        <h2 id="source-results-title">Search results</h2>
        <ol>
          <li v-for="work in works" :key="work.externalId" class="studio-panel source-result">
            <div>
              <h3>{{ work.title }}</h3>
              <p>{{ contributorText(work) }}</p>
              <p class="source-result__meta">{{ work.languages.join(', ') || 'Language not supplied' }} · Project Gutenberg #{{ work.externalId }}</p>
            </div>
            <button type="button" class="studio-button studio-button--quiet" :disabled="selecting" @click="selectWork(work)">
              {{ selecting ? 'Opening…' : 'Select work' }}
            </button>
          </li>
        </ol>
      </section>

      <section v-if="selectedWork" class="studio-panel source-review__work" aria-labelledby="selected-work-title">
        <div class="source-review__work-heading">
          <div>
            <p class="studio-page-heading__eyebrow">Selected provider work</p>
            <h2 id="selected-work-title">{{ selectedWork.title }}</h2>
            <p>{{ contributorText(selectedWork) }}</p>
          </div>
          <a :href="selectedWork.landingUrl" target="_blank" rel="noreferrer" class="studio-button studio-button--quiet" :aria-label="`Open ${selectedWork.title} on Project Gutenberg in a new tab`">Open provider page</a>
        </div>
        <dl class="source-review__work-metadata">
          <div><dt>Provider</dt><dd>Project Gutenberg</dd></div>
          <div><dt>Work identifier</dt><dd>#{{ selectedWork.externalId }}</dd></div>
          <div><dt>Languages</dt><dd>{{ selectedWork.languages.join(', ') || 'Not supplied' }}</dd></div>
          <div><dt>Available files</dt><dd>{{ selectedWork.representations.length }} provider representations</dd></div>
        </dl>
        <p class="source-review__save-note">Saving asks Panda Pages to securely reacquire a supported source representation. It does not create a story or canonical source.</p>
        <button type="button" class="studio-button studio-button--primary" :disabled="saving" @click="saveForReview">
          {{ saving ? 'Saving for source review…' : 'Save for source review' }}
        </button>
      </section>
    </section>

    <section v-else id="source-review-panel-saved" class="source-review__saved" role="tabpanel" aria-labelledby="source-review-tab-saved">
      <div class="source-review__saved-grid">
        <div>
          <h2 id="saved-sources-title" class="studio-visually-hidden">Saved sources</h2>
          <StoryStudioState v-if="savedLoading && saved.length === 0" kind="loading" title="Loading saved sources" message="Opening durable source acquisitions." />
          <StoryStudioState v-else-if="saved.length === 0 && !error" kind="empty" title="No sources saved for review yet" message="Search Project Gutenberg to save an exact source acquisition." />
          <ol v-else class="source-review__saved-list" aria-label="Saved sources">
            <li v-for="item in saved" :key="item.id">
              <button type="button" :aria-current="selectedAcquisition?.id === item.id ? 'true' : undefined" @click="openSaved(item.id)">
                <strong>{{ item.title }}</strong>
                <span>Project Gutenberg #{{ item.externalId }} · {{ formatSavedAt(item.createdAt) }}</span>
                <span class="source-review__statuses">Rights: {{ statusLabel(item.review.rights.status) }} · Source quality: {{ statusLabel(item.review.editorial.status) }}</span>
              </button>
            </li>
          </ol>
        </div>

        <StoryStudioState v-if="detailLoading" kind="loading" title="Opening saved source" message="Loading durable source evidence." />
        <article v-else-if="selectedAcquisition" class="source-review__detail">
          <header class="studio-panel source-review__detail-heading">
            <div>
              <p class="studio-page-heading__eyebrow">Saved source</p>
              <h2>{{ selectedAcquisition.title }}</h2>
              <p>{{ contributorText(selectedAcquisition) }} · Project Gutenberg #{{ selectedAcquisition.externalId }}</p>
            </div>
            <a :href="selectedAcquisition.landingUrl" target="_blank" rel="noreferrer" class="studio-button studio-button--quiet" :aria-label="`Open ${selectedAcquisition.title} on Project Gutenberg in a new tab`">Provider page</a>
          </header>

          <section v-if="reviewReady" class="source-review__ready" aria-labelledby="ready-promotion-title">
            <h3 id="ready-promotion-title">Ready for canonical-source promotion</h3>
            <p>This source has completed review. Canonical-source promotion is not available yet.</p>
          </section>

          <section class="studio-panel source-review__rights" aria-labelledby="rights-review-title">
            <h3 id="rights-review-title">Rights review</h3>
            <p>Provider rights information is evidence only. It does not constitute Panda Pages approval.</p>
            <blockquote v-if="selectedAcquisition.providerRights">{{ selectedAcquisition.providerRights }}</blockquote>
            <p v-else class="source-review__muted">The provider did not supply a rights statement.</p>
            <div class="source-review__review-fields">
              <div class="studio-field"><label :for="`rights-status-${selectedAcquisition.id}`">Rights status</label><select :id="`rights-status-${selectedAcquisition.id}`" :value="rightsStatus" @change="setDraftStatus('rights', ($event.target as HTMLSelectElement).value as AdminSourceAcquisitionReviewStatus)"><option value="pending">Pending</option><option value="approved">Approved</option><option value="rejected">Rejected</option></select></div>
              <div v-if="rightsStatus !== 'pending'" class="studio-field"><label :for="`rights-note-${selectedAcquisition.id}`">Rationale</label><textarea :id="`rights-note-${selectedAcquisition.id}`" v-model="rightsNote" rows="3" /></div>
            </div>
            <button type="button" class="studio-button studio-button--quiet" :disabled="reviewSaving !== null" @click="saveReview('rights')">{{ reviewSaving === 'rights' ? 'Saving rights review…' : 'Save rights review' }}</button>
          </section>

          <section class="studio-panel source-review__quality" aria-labelledby="quality-review-title">
            <h3 id="quality-review-title">Source quality review</h3>
            <p>Confirm that this is the intended work, sufficiently complete, usable as source material, and textually sound. This does not assess child suitability or reading level.</p>
            <div class="source-review__review-fields">
              <div class="studio-field"><label :for="`quality-status-${selectedAcquisition.id}`">Source quality status</label><select :id="`quality-status-${selectedAcquisition.id}`" :value="editorialStatus" @change="setDraftStatus('editorial', ($event.target as HTMLSelectElement).value as AdminSourceAcquisitionReviewStatus)"><option value="pending">Pending</option><option value="approved">Approved</option><option value="rejected">Rejected</option></select></div>
              <div v-if="editorialStatus !== 'pending'" class="studio-field"><label :for="`quality-note-${selectedAcquisition.id}`">Rationale</label><textarea :id="`quality-note-${selectedAcquisition.id}`" v-model="editorialNote" rows="3" /></div>
            </div>
            <button type="button" class="studio-button studio-button--quiet" :disabled="reviewSaving !== null" @click="saveReview('editorial')">{{ reviewSaving === 'editorial' ? 'Saving source quality review…' : 'Save source quality review' }}</button>
          </section>

          <details class="studio-panel source-review__provenance">
            <summary>Source provenance</summary>
            <dl>
              <div><dt>Languages</dt><dd>{{ selectedAcquisition.languages.join(', ') || 'Not supplied' }}</dd></div>
              <div><dt>Representation</dt><dd>{{ selectedAcquisition.selectedRepresentation.mediaType }}<span v-if="selectedAcquisition.selectedRepresentation.label"> · {{ selectedAcquisition.selectedRepresentation.label }}</span></dd></div>
              <div><dt>Normalisation</dt><dd>{{ selectedAcquisition.normalisationVersion }}</dd></div>
              <div><dt>Retrieved content hash</dt><dd><code>{{ selectedAcquisition.retrievedContentHash }}</code></dd></div>
              <div><dt>Normalised content hash</dt><dd><code>{{ selectedAcquisition.normalisedContentHash }}</code></dd></div>
              <div><dt>Snapshot hash</dt><dd><code>{{ selectedAcquisition.snapshotHash }}</code></dd></div>
              <div><dt>Saved</dt><dd>{{ formatSavedAt(selectedAcquisition.createdAt) }}</dd></div>
            </dl>
          </details>

          <section class="studio-panel source-review__text" aria-labelledby="saved-source-text-title">
            <h3 id="saved-source-text-title">Saved source text</h3>
            <p>This read-only text is the exact durable source acquisition evidence. It is not a Panda Pages canonical source.</p>
            <pre tabindex="0" aria-label="Saved source text">{{ selectedAcquisition.sourceText }}</pre>
          </section>
        </article>
        <StoryStudioState v-else-if="saved.length" kind="empty" title="Select a saved source" message="Choose a source acquisition to inspect its evidence and review state." />
      </div>
    </section>
  </div>
</template>

<style scoped>
.source-review__tabs{display:flex;gap:.45rem;margin-bottom:1.25rem;border-bottom:1px solid var(--studio-line-strong)}.source-review__tabs button{min-height:2.75rem;border-bottom:3px solid transparent;padding:.65rem .9rem;color:var(--studio-muted);font-weight:720}.source-review__tabs button[aria-selected='true']{border-color:var(--panda-ink);color:var(--studio-ink)}.source-review__message,.source-review__error,.source-review__ready{margin-bottom:1rem;border-radius:var(--panda-radius-compact);padding:.85rem 1rem}.source-review__message,.source-review__ready{border:1px solid var(--panda-success);background:var(--panda-success-surface);color:var(--panda-success)}.source-review__error{border:1px solid var(--panda-danger);background:var(--panda-danger-surface);color:var(--panda-danger)}.source-review__intro,.source-review__save-note,.source-review__detail-heading p,.source-review__rights>p,.source-review__quality>p,.source-review__text>p{margin-top:.4rem;color:var(--studio-muted);line-height:1.55}.source-review__search{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:.8rem;margin-top:1rem}.source-review__results{margin-top:1.4rem}.source-review__results>h2{font-family:var(--panda-serif);font-size:1.3rem}.source-review__results ol,.source-review__saved-list{display:grid;gap:.75rem;margin-top:.75rem}.source-result{display:flex;align-items:center;justify-content:space-between;gap:1rem}.source-result h3,.source-review__work h2,.source-review__detail h2{font-family:var(--panda-serif);font-size:1.25rem;font-weight:650}.source-result p{margin-top:.25rem;color:var(--studio-muted)}.source-result__meta,.source-review__muted{font-size:.84rem}.source-review__work{margin-top:1.4rem}.source-review__work-heading,.source-review__detail-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.source-review__work-metadata{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.75rem;margin:1rem 0}.source-review__work-metadata dt,.source-review__provenance dt{color:var(--studio-muted);font-size:.75rem;font-weight:720;letter-spacing:.05em;text-transform:uppercase}.source-review__work-metadata dd,.source-review__provenance dd{overflow-wrap:anywhere;margin-top:.2rem}.source-review__saved-grid{display:grid;grid-template-columns:minmax(16rem,.42fr) minmax(0,1fr);align-items:start;gap:1rem}.source-review__saved-list button{display:grid;width:100%;gap:.25rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);background:var(--studio-card);padding:.85rem;text-align:left;box-shadow:var(--studio-shadow-soft)}.source-review__saved-list button:hover,.source-review__saved-list button[aria-current='true']{border-color:var(--studio-line-strong);background:var(--panda-mist)}.source-review__saved-list span{color:var(--studio-muted);font-size:.8rem;line-height:1.45}.source-review__statuses{font-weight:650}.source-review__detail{display:grid;gap:1rem;min-width:0}.source-review__rights h3,.source-review__quality h3,.source-review__text h3{font-family:var(--panda-serif);font-size:1.2rem}.source-review__rights blockquote{margin-top:.8rem;border-left:3px solid var(--studio-line-strong);padding-left:.8rem;color:var(--studio-muted);font-style:italic;line-height:1.5}.source-review__review-fields{display:grid;gap:.8rem;margin:1rem 0}.source-review__review-fields textarea{display:block;width:100%;resize:vertical;margin-top:.4rem;border:1px solid var(--studio-line-strong);border-radius:var(--panda-radius-compact);background:var(--panda-white);padding:.65rem .75rem;color:var(--studio-ink);font:inherit;line-height:1.45}.source-review__provenance summary{cursor:pointer;font-weight:720}.source-review__provenance dl{display:grid;gap:.8rem;margin-top:1rem}.source-review__text pre{max-height:32rem;overflow:auto;margin-top:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);background:var(--panda-mist);padding:1rem;color:var(--studio-ink);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.82rem;line-height:1.55;white-space:pre-wrap;overflow-wrap:anywhere}@media(max-width:760px){.source-review__search,.source-review__saved-grid{grid-template-columns:1fr}.source-review__search .studio-button{width:100%}.source-result,.source-review__work-heading,.source-review__detail-heading{align-items:stretch;flex-direction:column}.source-result .studio-button,.source-review__work-heading .studio-button,.source-review__detail-heading .studio-button{width:100%}.source-review__work-metadata{grid-template-columns:1fr}.source-review__text pre{max-height:22rem}}
</style>
