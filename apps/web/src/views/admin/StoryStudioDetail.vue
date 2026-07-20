<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import StoryPublishDialog from '@/components/admin/story-studio/StoryPublishDialog.vue'
import StoryStatusBadge from '@/components/admin/story-studio/StoryStatusBadge.vue'
import StoryStudioState from '@/components/admin/story-studio/StoryStudioState.vue'
import StoryUnpublishDialog from '@/components/admin/story-studio/StoryUnpublishDialog.vue'
import StoryVersionHistory from '@/components/admin/story-studio/StoryVersionHistory.vue'
import {
  adminGetStory,
  adminPublishStory,
  adminUnpublishStory,
  type AdminStoryDetail,
} from '@/lib/api'
import { authState } from '@/lib/session'
import {
  draftOutcomeMessage,
  projectStoryStudioError,
  storyCanUnpublish,
  storyRightsSummary,
  versionCanPublish,
  type StoryStudioError,
} from '@/lib/story-studio-navigation'

const route = useRoute()
const router = useRouter()
const story = ref<AdminStoryDetail | null>(null)
const loading = ref(true)
const error = ref<StoryStudioError | null>(null)
const actionMessage = ref('')
const selectedVersionId = ref<string | null>(null)
const publishDialogOpen = ref(false)
const unpublishDialogOpen = ref(false)
const publishing = ref(false)
const unpublishing = ref(false)
let generation = 0
let controller: AbortController | null = null

const slug = computed(() => String(route.params.slug ?? ''))
const selectedVersion = computed(
  () =>
    story.value?.versions.find(
      (version) => version.versionId === selectedVersionId.value,
    ) ?? null,
)

function selectDefaultVersion(detail: AdminStoryDetail) {
  const preferred = [detail.draftVersion?.versionId, ...detail.versions.map((item) => item.versionId)]
  selectedVersionId.value =
    preferred.find((id) => {
      const version = detail.versions.find((item) => item.versionId === id)
      return version ? versionCanPublish(version) : false
    }) ?? null
}

async function moveToUnlock() {
  authState.confirmLocked()
  await router.replace({
    path: '/unlock',
    query: { next: `/admin/stories/${encodeURIComponent(slug.value)}` },
  })
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
    selectDefaultVersion(detail)
  } catch (caught) {
    if (controller.signal.aborted || activeGeneration !== generation) return
    const projected = projectStoryStudioError(caught)
    error.value = projected
    if (projected.kind === 'session') await moveToUnlock()
  } finally {
    if (activeGeneration === generation) loading.value = false
  }
}

function editVersion(versionId: string) {
  void router.push({
    name: 'admin-story-edit',
    params: { slug: slug.value },
    query: { fromVersion: versionId },
  })
}

function editPreferredVersion() {
  const detail = story.value
  if (!detail) return
  const preferred =
    detail.versions.find(
      (version) => version.versionId === detail.draftVersion?.versionId && version.health === 'ready',
    ) ??
    detail.versions.find(
      (version) => version.versionId === detail.publishedVersion?.versionId && version.health === 'ready',
    ) ??
    detail.versions.find((version) => version.health === 'ready')
  if (preferred) editVersion(preferred.versionId)
}

async function publishSelected() {
  const detail = story.value
  const version = selectedVersion.value
  if (!detail || !version || !versionCanPublish(version) || publishing.value) return
  publishing.value = true
  error.value = null
  try {
    const result = await adminPublishStory(detail.slug, version.versionId)
    actionMessage.value = `Version ${result.publishedVersion?.version ?? version.version} published. Readers can now open it.`
    publishDialogOpen.value = false
    await loadStory(true)
  } catch (caught) {
    const projected = projectStoryStudioError(caught)
    error.value = projected
    publishDialogOpen.value = false
    if (projected.kind === 'session') await moveToUnlock()
  } finally {
    publishing.value = false
  }
}

async function unpublish() {
  const detail = story.value
  if (!detail || !storyCanUnpublish(detail) || unpublishing.value) return
  unpublishing.value = true
  error.value = null
  try {
    await adminUnpublishStory(detail.slug)
    actionMessage.value = 'Story unpublished. Drafts, versions and reading progress were retained.'
    unpublishDialogOpen.value = false
    await loadStory(true)
  } catch (caught) {
    const projected = projectStoryStudioError(caught)
    error.value = projected
    unpublishDialogOpen.value = false
    if (projected.kind === 'session') await moveToUnlock()
  } finally {
    unpublishing.value = false
  }
}

watch(
  () => route.fullPath,
  () => {
    actionMessage.value = ''
    const outcome = route.query.saved
    const version = Number(route.query.version)
    if (
      (outcome === 'created_story' ||
        outcome === 'created_version' ||
        outcome === 'reused') &&
      Number.isSafeInteger(version) &&
      version > 0
    ) {
      actionMessage.value = draftOutcomeMessage(outcome, version)
    }
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
    <StoryStudioState
      v-if="loading && !story"
      kind="loading"
      title="Opening story"
      message="Loading metadata and immutable version history."
    />
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
          <p class="studio-page-heading__eyebrow">Story details</p>
          <h1>{{ story.title }}</h1>
          <p class="studio-page-heading__summary">
            <span v-if="story.author">{{ story.author }} · </span>{{ story.slug }}
          </p>
        </div>
        <StoryStatusBadge :status="story.status" />
      </header>

      <p v-if="actionMessage" class="detail-message" role="status">{{ actionMessage }}</p>
      <div v-if="error" class="detail-error" role="alert">
        <div><strong>{{ error.title }}</strong><p>{{ error.message }}</p></div>
        <button v-if="error.retryable" type="button" class="studio-button studio-button--quiet" @click="loadStory(true)">Try again</button>
      </div>

      <section v-if="story.status === 'repair_required'" class="repair-banner" aria-labelledby="repair-title">
        <div aria-hidden="true">!</div>
        <span>
          <h2 id="repair-title">Needs attention</h2>
          <p>A stored version cannot be safely reused or published. Safe summaries remain available below; create a fresh version from a healthy source where possible.</p>
        </span>
      </section>

      <div class="detail-grid">
        <section class="studio-panel detail-overview" aria-labelledby="story-overview-title">
          <div class="detail-overview__heading">
            <h2 id="story-overview-title">Story overview</h2>
            <code>{{ story.slug }}</code>
          </div>
          <dl>
            <div><dt>Language</dt><dd>{{ story.language }}</dd></div>
            <div><dt>Rights</dt><dd>{{ storyRightsSummary(story.rights) }}</dd></div>
            <div><dt>Published</dt><dd>{{ story.publishedVersion ? `Version ${story.publishedVersion.version}` : 'Not published' }}</dd></div>
            <div><dt>Current draft</dt><dd>{{ story.draftVersion ? `Version ${story.draftVersion.version}` : 'No current draft' }}</dd></div>
            <div><dt>Total versions</dt><dd>{{ story.versionCount }}</dd></div>
            <div v-if="story.sourceUrl"><dt>Source</dt><dd><a :href="story.sourceUrl" rel="noreferrer" target="_blank">Open source reference</a></dd></div>
          </dl>
        </section>

        <aside class="studio-panel detail-actions" aria-labelledby="story-actions-title">
          <h2 id="story-actions-title">Actions</h2>
          <p>Saving and publishing are always separate decisions.</p>
          <button type="button" class="studio-button studio-button--primary" :disabled="!story.versions.some((version) => version.health === 'ready')" @click="editPreferredVersion">
            Edit as new draft
          </button>
          <button type="button" class="studio-button studio-button--quiet" :disabled="!selectedVersion" @click="publishDialogOpen = true">
            Publish selected version
          </button>
          <a v-if="story.publishedVersion" class="studio-button studio-button--quiet" :href="`/read/${encodeURIComponent(story.slug)}`">Open published story</a>
          <button v-if="story.publishedVersion" type="button" class="studio-button detail-actions__unpublish" @click="unpublishDialogOpen = true">Unpublish</button>
          <button type="button" class="detail-actions__return" @click="router.push('/admin/stories')">← Return to stories</button>
        </aside>
      </div>

      <div class="studio-panel detail-history">
        <StoryVersionHistory
          :versions="story.versions"
          :selected-version-id="selectedVersionId"
          @select="selectedVersionId = $event"
          @edit="editVersion"
        />
      </div>

      <StoryPublishDialog
        :open="publishDialogOpen"
        :title="story.title"
        :version="selectedVersion?.version ?? null"
        :current-published-version="story.publishedVersion?.version ?? null"
        :busy="publishing"
        @confirm="publishSelected"
        @cancel="publishDialogOpen = false"
      />
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
.detail-message,
.detail-error,
.repair-banner {
  margin-bottom: 1rem;
  border-radius: 0.9rem;
  padding: 0.9rem 1rem;
}

.detail-message { border: 1px solid var(--panda-success); background: var(--panda-success-surface); color: var(--panda-success); }
.detail-error { display: flex; align-items: center; justify-content: space-between; gap: 1rem; border: 1px solid var(--panda-danger); background: var(--panda-danger-surface); color: var(--panda-danger); }
.detail-error p { margin-top: 0.2rem; }

.repair-banner {
  display: flex;
  gap: 0.9rem;
  border: 1px solid var(--panda-warning);
  background: var(--panda-warning-surface);
  color: var(--panda-warning);
}

.repair-banner > div { display: grid; flex: 0 0 2.5rem; height: 2.5rem; place-items: center; border: 1px solid currentColor; border-radius: var(--panda-radius-compact); background: var(--panda-paper-raised); font-weight: 800; }
.repair-banner h2 { font-weight: 780; }
.repair-banner p { margin-top: 0.25rem; line-height: 1.5; }

.detail-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(16rem, 21rem); gap: 1rem; }
.detail-overview__heading { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.detail-overview h2,
.detail-actions h2 { font-family: var(--panda-serif); font-size: 1.2rem; font-weight: 650; }
.detail-overview code { overflow-wrap: anywhere; color: var(--studio-muted); font-size: 0.75rem; }
.detail-overview dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 1rem; gap: 1rem; }
.detail-overview dt { color: var(--studio-muted); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.06em; }
.detail-overview dd { overflow-wrap: anywhere; margin-top: 0.25rem; font-weight: 650; }
.detail-overview a { color: var(--panda-ink); font-weight: 700; text-decoration: underline; text-underline-offset: 0.2em; }

.detail-actions { display: flex; flex-direction: column; gap: 0.7rem; }
.detail-actions > p { margin-bottom: 0.25rem; color: var(--studio-muted); font-size: 0.85rem; line-height: 1.5; }
.detail-actions__unpublish { border-color: var(--panda-warning); color: var(--panda-warning); }
.detail-actions__return { min-height: 2.75rem; color: var(--studio-muted); text-align: left; }
.detail-history { margin-top: 1rem; }

@media (max-width: 760px) {
  .detail-grid { grid-template-columns: 1fr; }
  .detail-overview dl { grid-template-columns: 1fr; }
  .detail-error { align-items: stretch; flex-direction: column; }
}
</style>
