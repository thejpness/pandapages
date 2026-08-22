<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import StoryOrchestrationDraftIngest from './StoryOrchestrationDraftIngest.vue'
import StoryStudioDialog from './StoryStudioDialog.vue'
import {
  adminCreateStoryOrchestrationEditorialReview,
  adminListStoryOrchestrationEditorialReviews,
  getAPIErrorStatus,
  type AdminStoryOrchestrationEditorialDecision,
  type AdminStoryOrchestrationEditorialReview,
} from '@/lib/api'

const props = defineProps<{
  runId: string
  storySlug: string
}>()

type EditorialReviewError = {
  kind: 'session' | 'forbidden' | 'not-found' | 'repair' | 'validation' | 'unavailable' | 'retry'
  title: string
  message: string
  retryable: boolean
}

const router = useRouter()
const history = ref<AdminStoryOrchestrationEditorialReview[]>([])
const historyFresh = ref(false)
const historyLoading = ref(false)
const historyError = ref<EditorialReviewError | null>(null)
const pendingDecision = ref<AdminStoryOrchestrationEditorialDecision | null>(null)
const submitting = ref(false)
const submissionError = ref<EditorialReviewError | null>(null)
const actionMessage = ref('')

let active = true
let historyGeneration = 0
let historyController: AbortController | null = null
let submissionController: AbortController | null = null

const currentDecision = computed<AdminStoryOrchestrationEditorialDecision | 'not_reviewed' | null>(() => {
  if (!historyFresh.value) return null
  return history.value[0]?.decision ?? 'not_reviewed'
})
const currentApprovedReview = computed<AdminStoryOrchestrationEditorialReview | null>(() =>
  historyFresh.value && history.value[0]?.decision === 'approved'
    ? history.value[0]
    : null,
)
const currentDecisionLabel = computed(() => {
  if (currentDecision.value === 'approved') return 'Approved'
  if (currentDecision.value === 'rejected') return 'Rejected'
  if (currentDecision.value === 'not_reviewed') return 'Not reviewed'
  return 'Current decision unavailable'
})
const currentDecisionClass = computed(() => {
  if (currentDecision.value === 'approved') return 'editorial-review__decision--approved'
  if (currentDecision.value === 'rejected') return 'editorial-review__decision--rejected'
  if (currentDecision.value === 'not_reviewed') return 'editorial-review__decision--not-reviewed'
  return 'editorial-review__decision--unavailable'
})
const confirmationTitle = computed(() => pendingDecision.value === 'approved' ? 'Approve this generated run?' : 'Reject this generated run?')
const confirmationLabel = computed(() => pendingDecision.value === 'approved' ? 'Record approval' : 'Record rejection')

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('en-GB', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function decisionLabel(decision: AdminStoryOrchestrationEditorialDecision): string {
  return decision === 'approved' ? 'Approved' : 'Rejected'
}

function abortHistoryRequest() {
  historyGeneration += 1
  historyController?.abort()
  historyController = null
}

function ownsRun(runId: string, controller?: AbortController): boolean {
  return active && props.runId === runId && !controller?.signal.aborted
}

function projectEditorialReviewError(error: unknown, surface: 'history' | 'submission'): EditorialReviewError {
  const status = getAPIErrorStatus(error)
  if (status === 401) return { kind: 'session', title: 'Session ended', message: 'Sign in to Panda Pages to continue in Story Studio.', retryable: false }
  if (status === 403) return { kind: 'forbidden', title: 'Editorial review is unavailable', message: 'Administrator access is not available for this request.', retryable: false }
  if (status === 400) return { kind: 'validation', title: 'Editorial review could not be opened safely', message: 'This generated run could not be used for editorial review.', retryable: false }
  if (status === 404) return { kind: 'not-found', title: 'Generation unavailable', message: 'This generation run is no longer available. Choose another recent generation.', retryable: false }
  if (status === 409) return { kind: 'repair', title: 'Generation run needs repair', message: 'The retained orchestration evidence cannot be reviewed until it is repaired.', retryable: false }
  if (status === 503) return { kind: 'unavailable', title: 'Editorial review is unavailable', message: 'The service is temporarily unavailable. Try again.', retryable: true }
  return {
    kind: 'retry',
    title: surface === 'history' ? 'Editorial history could not be loaded' : 'Editorial decision could not be recorded',
    message: 'The connection or server may be temporarily unavailable. Try again.',
    retryable: true,
  }
}

async function moveToSignIn() {
  await router.replace({
    path: '/account/login',
    query: { next: `/admin/stories/${encodeURIComponent(props.storySlug)}` },
  })
}

async function loadHistory(): Promise<boolean> {
  abortHistoryRequest()
  const runId = props.runId
  const controller = new AbortController()
  historyController = controller
  const requestGeneration = ++historyGeneration
  historyLoading.value = true
  historyFresh.value = false
  historyError.value = null
  try {
    const response = await adminListStoryOrchestrationEditorialReviews(runId, undefined, controller.signal)
    if (!ownsRun(runId, controller) || requestGeneration !== historyGeneration) return false
    history.value = response.items
    historyFresh.value = true
    return true
  } catch (caught) {
    if (!ownsRun(runId, controller) || requestGeneration !== historyGeneration) return false
    const projected = projectEditorialReviewError(caught, 'history')
    historyError.value = projected
    if (projected.kind === 'session') await moveToSignIn()
    return false
  } finally {
    if (requestGeneration === historyGeneration) {
      historyLoading.value = false
      historyController = null
    }
  }
}

function openConfirmation(decision: AdminStoryOrchestrationEditorialDecision) {
  if (submitting.value || currentDecision.value === decision) return
  submissionError.value = null
  actionMessage.value = ''
  pendingDecision.value = decision
}

function cancelConfirmation() {
  if (submitting.value) return
  pendingDecision.value = null
}

async function confirmDecision() {
  const decision = pendingDecision.value
  const runId = props.runId
  if (!decision || submitting.value) return
  submitting.value = true
  submissionError.value = null
  actionMessage.value = ''
  const controller = new AbortController()
  submissionController = controller
  try {
    await adminCreateStoryOrchestrationEditorialReview(runId, decision, controller.signal)
    if (!ownsRun(runId, controller) || submissionController !== controller) return
    pendingDecision.value = null
    const refreshed = await loadHistory()
    if (!ownsRun(runId, controller) || submissionController !== controller) return
    actionMessage.value = refreshed
      ? 'Editorial decision recorded. Editorial history is up to date.'
      : 'Decision recorded, but editorial history could not be refreshed.'
  } catch (caught) {
    if (!ownsRun(runId, controller) || submissionController !== controller) return
    const projected = projectEditorialReviewError(caught, 'submission')
    submissionError.value = projected
    pendingDecision.value = null
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    if (submissionController === controller) {
      submitting.value = false
      submissionController = null
    }
  }
}

function retryHistory() {
  if (!historyLoading.value && !submitting.value) void loadHistory()
}

function refreshHistoryAfterDraftIngestConflict() {
  if (!historyLoading.value && !submitting.value) void loadHistory()
}

watch(
  () => props.runId,
  () => {
    abortHistoryRequest()
    submissionController?.abort()
    submissionController = null
    history.value = []
    historyFresh.value = false
    historyLoading.value = false
    historyError.value = null
    pendingDecision.value = null
    submitting.value = false
    submissionError.value = null
    actionMessage.value = ''
    void loadHistory()
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  active = false
  abortHistoryRequest()
  submissionController?.abort()
  submissionController = null
})
</script>

<template>
  <section class="editorial-review" aria-labelledby="editorial-review-title" :aria-busy="historyLoading || submitting">
    <div class="editorial-review__heading">
      <div>
        <p class="studio-page-heading__eyebrow">Editorial review</p>
        <h4 id="editorial-review-title">Human editorial decision</h4>
        <p>This decision applies to this exact generated run. It does not publish or ingest the story.</p>
      </div>
      <span class="editorial-review__decision" :class="currentDecisionClass">{{ currentDecisionLabel }}</span>
    </div>

    <p v-if="historyLoading" class="editorial-review__status" role="status">Loading editorial history…</p>
    <div v-if="historyError" class="editorial-review__error" role="alert">
      <div><strong>{{ historyError.title }}</strong><p>{{ historyError.message }}</p></div>
      <button v-if="historyError.retryable" type="button" class="studio-button studio-button--quiet" :disabled="historyLoading || submitting" @click="retryHistory">Try again</button>
    </div>
    <p v-if="actionMessage" class="editorial-review__message" role="status">{{ actionMessage }}</p>
    <div v-if="submissionError" class="editorial-review__error" role="alert">
      <div><strong>{{ submissionError.title }}</strong><p>{{ submissionError.message }}</p></div>
    </div>

    <p v-if="currentDecision === 'approved'" class="editorial-review__hint">Approved is the current human decision. Reject this run to record a new decision.</p>
    <p v-else-if="currentDecision === 'rejected'" class="editorial-review__hint">Rejected is the current human decision. Approve this run to record a new decision.</p>
    <p v-else-if="currentDecision === null && !historyLoading" class="editorial-review__hint">Editorial history is unavailable, so the current human decision cannot be shown.</p>

    <StoryOrchestrationDraftIngest
      :run-id="props.runId"
      :story-slug="props.storySlug"
      :current-approved-review="currentApprovedReview"
      :review-mutation-pending="submitting"
      @refresh-editorial-history="refreshHistoryAfterDraftIngestConflict"
    />

    <div class="editorial-review__actions">
      <button type="button" class="studio-button studio-button--primary" :disabled="submitting || currentDecision === 'approved'" @click="openConfirmation('approved')">Approve this run</button>
      <button type="button" class="studio-button studio-button--danger" :disabled="submitting || currentDecision === 'rejected'" @click="openConfirmation('rejected')">Reject this run</button>
    </div>

    <details v-if="history.length || historyFresh" class="editorial-review__history">
      <summary>Editorial history · {{ history.length }} recorded {{ history.length === 1 ? 'decision' : 'decisions' }}</summary>
      <p v-if="!historyFresh" class="editorial-review__hint">Previously loaded history is shown below. Refresh it before relying on the current decision.</p>
      <p v-else-if="history.length === 0" class="editorial-review__hint">No editorial decisions have been recorded for this run.</p>
      <ol v-else class="editorial-review__history-list">
        <li v-for="review in history" :key="review.id">
          <strong>{{ decisionLabel(review.decision) }}</strong>
          <time :datetime="review.createdAt">{{ formatDate(review.createdAt) }}</time>
        </li>
      </ol>
    </details>
  </section>

  <StoryStudioDialog
    :open="pendingDecision !== null"
    :title="confirmationTitle"
    :description="`This will record a new immutable ${pendingDecision === 'approved' ? 'approval' : 'rejection'} for this generated run.`"
    :confirm-label="confirmationLabel"
    :busy="submitting"
    :danger="pendingDecision === 'rejected'"
    @confirm="confirmDecision"
    @cancel="cancelConfirmation"
  >
    <p>This becomes part of editorial history. It does not publish, ingest, or alter the generated story.</p>
  </StoryStudioDialog>
</template>

<style scoped>
.editorial-review{margin-top:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);padding:1rem}.editorial-review__heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.editorial-review h4{font-family:var(--panda-serif);font-size:1.05rem;font-weight:650}.editorial-review__heading>div>p:last-child,.editorial-review__hint{margin-top:.3rem;color:var(--studio-muted);line-height:1.5}.editorial-review__decision{display:inline-flex;flex:0 0 auto;align-items:center;border:1px solid currentColor;border-radius:999px;padding:.28rem .52rem;font-size:.76rem;font-weight:760;line-height:1.15}.editorial-review__decision--approved{color:var(--panda-success);background:var(--panda-success-surface)}.editorial-review__decision--rejected{color:var(--panda-danger);background:var(--panda-danger-surface)}.editorial-review__decision--not-reviewed,.editorial-review__decision--unavailable{color:var(--studio-muted);background:var(--panda-mist)}.editorial-review__status{margin-top:1rem;border-left:3px solid var(--panda-line-strong);padding-left:.8rem;color:var(--studio-muted);line-height:1.5}.editorial-review__error{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-top:1rem;border:1px solid var(--panda-danger);border-radius:var(--panda-radius-compact);background:var(--panda-danger-surface);padding:.75rem .85rem;color:var(--panda-danger)}.editorial-review__error p{margin-top:.2rem;line-height:1.45}.editorial-review__message{margin-top:1rem;border-left:3px solid var(--panda-success);padding-left:.8rem;color:var(--panda-success);font-weight:700;line-height:1.5}.editorial-review__actions{display:flex;flex-wrap:wrap;gap:.65rem;margin-top:1rem}.editorial-review__history{margin-top:1rem;border-top:1px solid var(--studio-line);padding-top:.85rem}.editorial-review__history summary{cursor:pointer;font-weight:720}.editorial-review__history-list{display:grid;gap:.55rem;margin-top:.8rem}.editorial-review__history-list li{display:flex;align-items:center;justify-content:space-between;gap:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);padding:.65rem .75rem}.editorial-review__history-list time{color:var(--studio-muted);font-size:.88rem}@media(max-width:780px){.editorial-review__heading,.editorial-review__error{align-items:stretch;flex-direction:column}.editorial-review__actions .studio-button{width:100%}.editorial-review__history-list li{align-items:flex-start;flex-direction:column}}
</style>
