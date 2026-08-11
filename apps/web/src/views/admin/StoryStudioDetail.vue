<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import StoryCanonicalSourcePanel from '@/components/admin/story-studio/StoryCanonicalSourcePanel.vue'
import StoryEditionWorkspace from '@/components/admin/story-studio/StoryEditionWorkspace.vue'
import StoryReleasePanel from '@/components/admin/story-studio/StoryReleasePanel.vue'
import StoryStatusBadge from '@/components/admin/story-studio/StoryStatusBadge.vue'
import StoryStudioState from '@/components/admin/story-studio/StoryStudioState.vue'
import StoryUnpublishDialog from '@/components/admin/story-studio/StoryUnpublishDialog.vue'
import StoryVersionHistory from '@/components/admin/story-studio/StoryVersionHistory.vue'
import {
  adminCreateRelease,
  adminGetStory,
  adminUnpublishStory,
  type AdminEditionDetail,
  type AdminReleaseEditionRequest,
  type AdminStoryDetail,
  type AdminStoryEditionKey,
} from '@/lib/api'
import {
  draftOutcomeMessage,
  editionPreferredVersion,
  parseStoryEditionKey,
  projectStoryStudioError,
  sourceOutcomeMessage,
  storyEditionLabel,
  storyRightsSummary,
  type StoryStudioError,
} from '@/lib/story-studio-navigation'

const route = useRoute()
const router = useRouter()
const story = ref<AdminStoryDetail | null>(null)
const loading = ref(true)
const error = ref<StoryStudioError | null>(null)
const actionMessage = ref('')
const activeEditionKey = ref<AdminStoryEditionKey>('classic')
const unpublishDialogOpen = ref(false)
const publishing = ref(false)
const unpublishing = ref(false)
let generation = 0
let controller: AbortController | null = null

const slug = computed(() => String(route.params.slug ?? ''))
const activeEdition = computed<AdminEditionDetail | null>(
  () => story.value?.editions.find((edition) => edition.editionKey === activeEditionKey.value) ?? null,
)
const activeEditionLabel = computed(() => storyEditionLabel(activeEditionKey.value))

function selectEdition(key: AdminStoryEditionKey) {
  activeEditionKey.value = key
}
async function moveToSignIn() {
  await router.replace({ path: '/account/login', query: { next: `/admin/stories/${encodeURIComponent(slug.value)}` } })
}
async function loadStory(preserve = false) {
  controller?.abort()
  controller = new AbortController()
  const activeGeneration = ++generation
  if (!preserve) loading.value = true
  error.value = null
  try {
    const detail = await adminGetStory(slug.value, controller.signal)
    if (activeGeneration !== generation) return
    story.value = detail
    const requested = parseStoryEditionKey(route.query.edition)
    if (requested) activeEditionKey.value = requested
    if (!detail.editions.some((edition) => edition.editionKey === activeEditionKey.value)) activeEditionKey.value = 'classic'
  } catch (caught) {
    if (controller.signal.aborted || activeGeneration !== generation) return
    const projected = projectStoryStudioError(caught)
    error.value = projected
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    if (activeGeneration === generation) loading.value = false
  }
}
function editEdition(key: AdminStoryEditionKey) {
  const detail = story.value
  const edition = detail?.editions.find((item) => item.editionKey === key)
  if (!detail || !edition) return
  const preferred = editionPreferredVersion(edition)
  void router.push({
    name: 'admin-story-edit',
    params: { slug: detail.slug },
    query: { edition: key, ...(preferred ? { fromVersion: preferred.versionId } : {}) },
  })
}
function editVersion(versionId: string) {
  void router.push({
    name: 'admin-story-edit',
    params: { slug: slug.value },
    query: { edition: activeEditionKey.value, fromVersion: versionId },
  })
}
function editSource() {
  void router.push({ name: 'admin-story-source', params: { slug: slug.value } })
}
function ingestEditions() {
  void router.push({ name: 'admin-story-ingest-existing', params: { slug: slug.value } })
}
async function publishRelease(editions: AdminReleaseEditionRequest[]) {
  const detail = story.value
  if (!detail || publishing.value || editions.length < 1) return
  publishing.value = true
  error.value = null
  try {
    const result = await adminCreateRelease(detail.slug, { editions })
    const count = result.release.editions.length
    actionMessage.value = result.outcome === 'reused_current'
      ? `Release ${result.release.release} already matches this edition set.`
      : `Release ${result.release.release} published with ${count} ${count === 1 ? 'edition' : 'editions'}.`
    await loadStory(true)
  } catch (caught) {
    const projected = projectStoryStudioError(caught)
    error.value = projected
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    publishing.value = false
  }
}
async function unpublish() {
  const detail = story.value
  if (!detail?.currentRelease || unpublishing.value) return
  unpublishing.value = true
  error.value = null
  try {
    await adminUnpublishStory(detail.slug)
    actionMessage.value = 'Story unpublished. Release history, drafts, versions and reading progress were retained.'
    unpublishDialogOpen.value = false
    await loadStory(true)
  } catch (caught) {
    const projected = projectStoryStudioError(caught)
    error.value = projected
    unpublishDialogOpen.value = false
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    unpublishing.value = false
  }
}

watch(
  () => route.fullPath,
  () => {
    actionMessage.value = ''
    const savedEdition = parseStoryEditionKey(route.query.edition) ?? 'classic'
    const outcome = route.query.saved
    const version = Number(route.query.version)
    if (
      (outcome === 'created_story' || outcome === 'created_version' || outcome === 'reused') &&
      Number.isSafeInteger(version) &&
      version > 0
    ) {
      actionMessage.value = draftOutcomeMessage(outcome, version, savedEdition)
      activeEditionKey.value = savedEdition
    }
    const sourceOutcome = route.query.sourceSaved
    const sourceVersion = Number(route.query.sourceVersion)
    if (
      (sourceOutcome === 'created_source' || sourceOutcome === 'created_version' || sourceOutcome === 'reused') &&
      Number.isSafeInteger(sourceVersion) &&
      sourceVersion > 0
    ) actionMessage.value = sourceOutcomeMessage(sourceOutcome, sourceVersion)
    const ingested = route.query.ingested === '1'
    const created = Number(route.query.created)
    const reused = Number(route.query.reused)
    if (
      ingested &&
      Number.isSafeInteger(created) &&
      created >= 0 &&
      Number.isSafeInteger(reused) &&
      reused >= 0 &&
      created + reused === 5
    ) actionMessage.value = `Five reading editions ingested as drafts - ${created} created, ${reused} reused. Nothing was published.`
    void loadStory()
  },
  { immediate: true },
)
onBeforeUnmount(() => {
  generation += 1
  controller?.abort()
})
</script>

<template>
  <div>
    <StoryStudioState v-if="loading && !story" kind="loading" title="Opening story" message="Loading canonical source, reading editions and release history." />
    <StoryStudioState
      v-else-if="error && !story"
      :kind="error.kind === 'repair' ? 'repair' : error.kind === 'forbidden' ? 'forbidden' : 'error'"
      :title="error.title"
      :message="error.message"
      :action-label="error.retryable ? 'Try again' : 'Return to stories'"
      @action="error.retryable ? loadStory() : router.push('/admin/stories')"
    />
    <template v-else-if="story">
      <header class="studio-page-heading">
        <div>
          <p class="studio-page-heading__eyebrow">Story workspace</p>
          <h1>{{ story.title }}</h1>
          <p class="studio-page-heading__summary"><span v-if="story.author">{{ story.author }} · </span>{{ story.slug }}</p>
        </div>
        <StoryStatusBadge :status="story.status" />
      </header>

      <p v-if="actionMessage" class="detail-message" role="status">{{ actionMessage }}</p>
      <div v-if="error" class="detail-error" role="alert">
        <div><strong>{{ error.title }}</strong><p>{{ error.message }}</p></div>
        <button v-if="error.retryable" type="button" class="studio-button studio-button--quiet" @click="loadStory(true)">Try again</button>
      </div>

      <section
        v-if="story.status === 'repair_required' || story.source.status === 'repair_required' || story.editions.some((edition) => edition.status === 'repair_required')"
        class="repair-banner"
        aria-labelledby="repair-title"
      >
        <div aria-hidden="true">!</div>
        <span>
          <h2 id="repair-title">Needs attention</h2>
          <p>One stored source, edition or release projection cannot be reused safely. Healthy immutable versions remain available independently.</p>
        </span>
      </section>

      <div class="detail-top-grid">
        <section class="studio-panel detail-overview" aria-labelledby="story-overview-title">
          <div class="detail-overview__heading"><h2 id="story-overview-title">Story identity</h2><code>{{ story.slug }}</code></div>
          <dl>
            <div><dt>Language</dt><dd>{{ story.language }}</dd></div>
            <div><dt>Rights</dt><dd>{{ storyRightsSummary(story.rights) }}</dd></div>
            <div><dt>Current release</dt><dd>{{ story.currentRelease ? `Release ${story.currentRelease.release} · ${story.currentRelease.editions.length} live` : 'Not published' }}</dd></div>
            <div><dt>Release history</dt><dd>{{ story.releaseCount }}</dd></div>
            <div><dt>Reading editions</dt><dd>5 canonical slots</dd></div>
            <div v-if="story.sourceUrl"><dt>Source reference</dt><dd><a :href="story.sourceUrl" rel="noreferrer" target="_blank">Open reference</a></dd></div>
          </dl>
          <div class="detail-overview__links">
            <a v-if="story.currentRelease" class="studio-button studio-button--quiet" :href="`/read/${encodeURIComponent(story.slug)}`">Open published story</a>
            <button type="button" class="studio-button studio-button--quiet" @click="ingestEditions">Import five editions</button>
            <button type="button" class="studio-button studio-button--quiet" @click="router.push('/admin/stories')">← Return to stories</button>
          </div>
        </section>
        <StoryCanonicalSourcePanel :source="story.source" @edit="editSource" />
      </div>

      <StoryReleasePanel
        :story="story"
        :busy="publishing || unpublishing"
        @publish="publishRelease"
        @unpublish="unpublishDialogOpen = true"
      />

      <div class="studio-panel detail-editions">
        <StoryEditionWorkspace :editions="story.editions" :active-key="activeEditionKey" @select="selectEdition" @edit="editEdition" />
      </div>

      <section v-if="activeEdition" class="studio-panel active-edition" aria-labelledby="active-edition-title">
        <div class="active-edition__heading">
          <div>
            <p class="studio-page-heading__eyebrow">Selected edition</p>
            <h2 id="active-edition-title">{{ activeEditionLabel }}</h2>
            <p>{{ activeEdition.versionCount }} immutable {{ activeEdition.versionCount === 1 ? 'version' : 'versions' }}.</p>
          </div>
          <div class="active-edition__actions">
            <StoryStatusBadge :edition-status="activeEdition.status" />
            <button type="button" class="studio-button studio-button--primary" @click="editEdition(activeEdition.editionKey)">
              {{ activeEdition.status === 'empty' ? 'Create this edition' : 'Edit as new draft' }}
            </button>
          </div>
        </div>

        <p class="release-managed">
          Publication is story-wide. Include any healthy version of this edition when reviewing the next release.
        </p>

        <div v-if="activeEdition.versions.length" class="detail-history">
          <StoryVersionHistory :versions="activeEdition.versions" :edition-label="activeEditionLabel" @edit="editVersion" />
        </div>
        <div v-else class="active-edition__empty">
          <strong>{{ activeEditionLabel }} has not been authored yet.</strong>
          <p>This is valid. A story release only needs the reading editions that belong in that story.</p>
        </div>
      </section>

      <StoryUnpublishDialog
        :open="unpublishDialogOpen"
        :title="story.title"
        :busy="unpublishing"
        @confirm="unpublish"
        @cancel="unpublishDialogOpen = false"
      />
    </template>
  </div>
</template>

<style scoped>
.detail-message,.detail-error,.repair-banner{margin-bottom:1rem;border-radius:.9rem;padding:.9rem 1rem}.detail-message{border:1px solid var(--panda-success);background:var(--panda-success-surface);color:var(--panda-success)}.detail-error{display:flex;align-items:center;justify-content:space-between;gap:1rem;border:1px solid var(--panda-danger);background:var(--panda-danger-surface);color:var(--panda-danger)}.detail-error p{margin-top:.2rem}.repair-banner{display:flex;gap:.9rem;border:1px solid var(--panda-warning);background:var(--panda-warning-surface);color:var(--panda-warning)}.repair-banner>div{display:grid;flex:0 0 2.5rem;height:2.5rem;place-items:center;border:1px solid currentColor;border-radius:var(--panda-radius-compact);background:var(--panda-paper-raised);font-weight:800}.repair-banner h2{font-weight:780}.repair-banner p{margin-top:.25rem;line-height:1.5}.detail-top-grid{display:grid;grid-template-columns:minmax(0,1.35fr) minmax(18rem,.65fr);gap:1rem}.detail-overview__heading{display:flex;align-items:center;justify-content:space-between;gap:1rem}.detail-overview h2{font-family:var(--panda-serif);font-size:1.2rem;font-weight:650}.detail-overview code{overflow-wrap:anywhere;color:var(--studio-muted);font-size:.75rem}.detail-overview dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));margin-top:1rem;gap:1rem}.detail-overview dt{color:var(--studio-muted);font-size:.75rem;text-transform:uppercase;letter-spacing:.06em}.detail-overview dd{overflow-wrap:anywhere;margin-top:.25rem;font-weight:650}.detail-overview a{color:var(--panda-ink);font-weight:700;text-decoration:underline;text-underline-offset:.2em}.detail-overview__links{display:flex;flex-wrap:wrap;gap:.6rem;margin-top:1rem}.detail-editions,.active-edition{margin-top:1rem}.active-edition__heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.active-edition__heading h2{font-family:var(--panda-serif);font-size:1.45rem;font-weight:650}.active-edition__heading p:last-child{margin-top:.25rem;color:var(--studio-muted)}.active-edition__actions{display:flex;align-items:center;flex-wrap:wrap;justify-content:flex-end;gap:.6rem}.release-managed{margin-top:1rem;border-left:3px solid var(--panda-line-strong);padding-left:.8rem;color:var(--studio-muted);line-height:1.5}.detail-history{margin-top:1rem}.active-edition__empty{margin-top:1rem;border:1px dashed var(--studio-line-strong);border-radius:var(--panda-radius-card);padding:1rem}.active-edition__empty p{margin-top:.3rem;color:var(--studio-muted)}@media(max-width:780px){.detail-top-grid{grid-template-columns:1fr}.detail-overview dl{grid-template-columns:1fr}.detail-error,.active-edition__heading{align-items:stretch;flex-direction:column}.active-edition__actions{justify-content:flex-start}}
</style>
