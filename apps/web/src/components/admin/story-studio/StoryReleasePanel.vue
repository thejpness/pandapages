<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type {
  AdminReleaseEditionRequest,
  AdminStoryDetail,
  AdminStoryEditionKey,
} from '@/lib/api'
import {
  buildStoryReleaseCandidate,
  releaseCandidateMatchesCurrent,
  releaseCandidateRequest,
} from '@/lib/story-release'
import {
  storyEditionLabel,
  storyEditionOrder,
} from '@/lib/story-studio-navigation'
import StoryStudioDialog from './StoryStudioDialog.vue'

const props = defineProps<{
  story: AdminStoryDetail
  busy: boolean
}>()
const emit = defineEmits<{
  publish: [editions: AdminReleaseEditionRequest[]]
  unpublish: []
}>()

const included = reactive<Record<AdminStoryEditionKey, boolean>>({
  classic: false,
  'confident-readers': false,
  'growing-readers': false,
  'story-explorers': false,
  'little-listeners': false,
})
const selected = reactive<Record<AdminStoryEditionKey, string | null>>({
  classic: null,
  'confident-readers': null,
  'growing-readers': null,
  'story-explorers': null,
  'little-listeners': null,
})
const reviewOpen = ref(false)

const rows = computed(() =>
  storyEditionOrder.map((editionKey) => {
    const edition = props.story.editions.find(
      (item) => item.editionKey === editionKey,
    )
    return {
      editionKey,
      versions: (edition?.versions ?? []).filter(
        (version) => version.health === 'ready',
      ),
    }
  }),
)

function resetCandidate() {
  const candidate = buildStoryReleaseCandidate(props.story)
  for (const row of candidate) {
    included[row.editionKey] = row.included
    selected[row.editionKey] = row.selectedVersionId
  }
  reviewOpen.value = false
}

watch(() => props.story, resetCandidate, { immediate: true })

const request = computed(() =>
  releaseCandidateRequest(
    rows.value.map((row) => ({
      ...row,
      included: included[row.editionKey],
      selectedVersionId: selected[row.editionKey],
    })),
  ),
)
const includedCount = computed(() => request.value.length)
const candidateUnchanged = computed(() =>
  releaseCandidateMatchesCurrent(request.value, props.story.currentRelease),
)
const canReview = computed(
  () =>
    !props.busy &&
    includedCount.value >= 1 &&
    !candidateUnchanged.value,
)
const nextRelease = computed(() => props.story.releaseCount + 1)

function versionLabel(editionKey: AdminStoryEditionKey, versionId: string) {
  const version = rows.value
    .find((row) => row.editionKey === editionKey)
    ?.versions.find((item) => item.versionId === versionId)
  return version ? `v${version.version}` : 'Version'
}

function confirmRelease() {
  if (!canReview.value) return
  reviewOpen.value = false
  emit('publish', request.value)
}
</script>

<template>
  <section class="release-panel studio-panel" aria-labelledby="release-panel-title">
    <div class="release-panel__heading">
      <div>
        <p class="studio-page-heading__eyebrow">Publication</p>
        <h2 id="release-panel-title">Story release</h2>
        <p>
          A release is an immutable snapshot of one to five reading editions.
          Omitted editions are intentionally not live.
        </p>
      </div>
      <div class="release-panel__current">
        <strong v-if="story.currentRelease">
          Release {{ story.currentRelease.release }}
        </strong>
        <strong v-else>Not published</strong>
        <span>
          {{ story.currentRelease?.editions.length ?? 0 }}
          {{ (story.currentRelease?.editions.length ?? 0) === 1 ? 'edition' : 'editions' }} live
        </span>
      </div>
    </div>

    <div class="release-candidate" aria-labelledby="release-candidate-title">
      <div class="release-candidate__heading">
        <div>
          <h3 id="release-candidate-title">Release candidate</h3>
          <p>
            Choose only the editions that belong in the next live release.
          </p>
        </div>
        <span v-if="candidateUnchanged" class="release-candidate__unchanged">
          Matches current release
        </span>
      </div>

      <ol>
        <li v-for="row in rows" :key="row.editionKey" class="release-edition">
          <label class="release-edition__include">
            <input
              v-model="included[row.editionKey]"
              type="checkbox"
              :disabled="row.versions.length === 0 || busy"
              :aria-label="`Include ${storyEditionLabel(row.editionKey)} in release`"
            />
            <span>
              <strong>{{ storyEditionLabel(row.editionKey) }}</strong>
              <small v-if="row.versions.length === 0">No healthy edition version</small>
              <small v-else-if="included[row.editionKey]">Included in next release</small>
              <small v-else>Not live in next release</small>
            </span>
          </label>

          <label v-if="row.versions.length" class="release-edition__version">
            <span class="studio-visually-hidden">
              {{ storyEditionLabel(row.editionKey) }} release version
            </span>
            <select
              v-model="selected[row.editionKey]"
              :disabled="!included[row.editionKey] || busy"
              :aria-label="`${storyEditionLabel(row.editionKey)} release version`"
            >
              <option
                v-for="version in row.versions"
                :key="version.versionId"
                :value="version.versionId"
              >
                v{{ version.version }}{{ version.isDraft ? ' · current draft' : '' }}{{ version.isPublished ? ' · live now' : '' }}
              </option>
            </select>
          </label>
        </li>
      </ol>

      <div class="release-candidate__actions">
        <p v-if="includedCount === 0" role="status">
          Select at least one healthy edition to create a release.
        </p>
        <p v-else>
          {{ includedCount }} {{ includedCount === 1 ? 'edition' : 'editions' }} selected.
        </p>
        <button
          type="button"
          class="studio-button studio-button--primary"
          :disabled="!canReview"
          @click="reviewOpen = true"
        >
          Review release
        </button>
        <button
          v-if="story.currentRelease"
          type="button"
          class="studio-button release-panel__unpublish"
          :disabled="busy"
          @click="emit('unpublish')"
        >
          Unpublish story
        </button>
      </div>
    </div>

    <details v-if="story.releases.length" class="release-history">
      <summary>Release history · {{ story.releaseCount }}</summary>
      <ol>
        <li v-for="release in story.releases" :key="release.release">
          <strong>
            Release {{ release.release }}
            <span v-if="story.currentRelease?.release === release.release"> · current</span>
          </strong>
          <span>
            {{ release.editions.map((edition) => `${storyEditionLabel(edition.editionKey)} v${edition.version}`).join(' · ') }}
          </span>
          <time :datetime="release.createdAt">
            {{ new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(release.createdAt)) }}
          </time>
        </li>
      </ol>
    </details>

    <StoryStudioDialog
      :open="reviewOpen"
      title="Publish release?"
      :description="`Release ${nextRelease} will replace the story's current live edition set atomically.`"
      confirm-label="Publish release"
      :busy="busy"
      @confirm="confirmRelease"
      @cancel="reviewOpen = false"
    >
      <ul class="release-review">
        <li v-for="edition in request" :key="edition.editionKey">
          <strong>{{ storyEditionLabel(edition.editionKey) }}</strong>
          <span>{{ versionLabel(edition.editionKey, edition.versionId) }}</span>
        </li>
      </ul>
      <p>
        Editions not listed here will not be live in this release. Their drafts and immutable history remain in Story Studio.
      </p>
    </StoryStudioDialog>
  </section>
</template>

<style scoped>
.release-panel{margin-top:1rem}.release-panel__heading,.release-candidate__heading,.release-candidate__actions{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.release-panel h2{font-family:var(--panda-serif);font-size:1.45rem;font-weight:650}.release-panel__heading p:last-child,.release-candidate__heading p{margin-top:.3rem;color:var(--studio-muted);line-height:1.5}.release-panel__current{display:grid;min-width:9rem;text-align:right}.release-panel__current span{margin-top:.2rem;color:var(--studio-muted);font-size:.8rem}.release-candidate{margin-top:1rem;border-top:1px solid var(--studio-line);padding-top:1rem}.release-candidate h3{font-weight:780}.release-candidate__unchanged{border-radius:var(--panda-radius-pill);background:var(--panda-mist);padding:.35rem .6rem;color:var(--studio-muted);font-size:.75rem;font-weight:700}.release-candidate ol{display:grid;margin-top:.85rem;gap:.55rem}.release-edition{display:grid;grid-template-columns:minmax(0,1fr) minmax(10rem,14rem);align-items:center;gap:1rem;border:1px solid var(--studio-line-strong);border-radius:var(--panda-radius-compact);padding:.75rem}.release-edition__include{display:flex;align-items:center;gap:.7rem}.release-edition__include input{width:1.15rem;height:1.15rem;accent-color:var(--panda-ink)}.release-edition__include small{display:block;margin-top:.15rem;color:var(--studio-muted)}.release-edition__version select{width:100%;min-height:2.6rem}.release-candidate__actions{align-items:center;margin-top:1rem}.release-candidate__actions p{flex:1;color:var(--studio-muted);font-size:.85rem}.release-panel__unpublish{border:1px solid var(--panda-warning);border-radius:var(--panda-radius-compact);background:transparent;padding:.65rem 1rem;color:var(--panda-warning);font-weight:720}.release-history{margin-top:1rem;border-top:1px solid var(--studio-line);padding-top:.8rem}.release-history summary{cursor:pointer;font-weight:720}.release-history ol{display:grid;margin-top:.7rem;gap:.5rem}.release-history li{display:grid;grid-template-columns:auto 1fr auto;gap:.8rem;color:var(--studio-muted);font-size:.8rem}.release-history strong{color:var(--studio-ink)}.release-review{display:grid;gap:.4rem;margin-bottom:.8rem}.release-review li{display:flex;justify-content:space-between;gap:1rem}@media(max-width:720px){.release-panel__heading,.release-candidate__heading,.release-candidate__actions{align-items:stretch;flex-direction:column}.release-panel__current{text-align:left}.release-edition{grid-template-columns:1fr}.release-history li{grid-template-columns:1fr}.release-candidate__actions .studio-button,.release-panel__unpublish{width:100%}}
</style>
