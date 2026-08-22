<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useRouter } from 'vue-router'
import StoryStudioDialog from './StoryStudioDialog.vue'
import {
  adminCreateStoryOrchestrationDraftIngest,
  getAPIErrorStatus,
  type AdminStoryOrchestrationDraftIngest,
  type AdminStoryOrchestrationEditorialReview,
} from '@/lib/api'

const props = defineProps<{
  runId: string
  storySlug: string
  currentApprovedReview: AdminStoryOrchestrationEditorialReview | null
  reviewMutationPending: boolean
}>()

const emit = defineEmits<{
  refreshEditorialHistory: []
}>()

type DraftIngestError = {
  title: string
  message: string
  retryable: boolean
}

type PendingAuthorization = {
  runId: string
  editorialReviewId: string
}

const router = useRouter()
const pendingAuthorization = ref<PendingAuthorization | null>(null)
const submitting = ref(false)
const result = ref<AdminStoryOrchestrationDraftIngest | null>(null)
const actionError = ref<DraftIngestError | null>(null)

let active = true
let requestGeneration = 0
let requestController: AbortController | null = null

const actionAvailable = computed(() =>
  props.currentApprovedReview !== null &&
  !props.reviewMutationPending &&
  !submitting.value,
)
const unavailableHint = computed(() => {
  if (props.currentApprovedReview) return ''
  return 'A fresh current approval is required before editable drafts can be created.'
})
const confirmationTitle = 'Create editable drafts?'

function ownsRequest(authorization: PendingAuthorization, controller: AbortController, generation: number): boolean {
  return active &&
    !controller.signal.aborted &&
    requestController === controller &&
    requestGeneration === generation &&
    props.runId === authorization.runId
}

function projectDraftIngestError(error: unknown): DraftIngestError {
  const status = getAPIErrorStatus(error)
  if (status === 400) return {
    title: 'Draft request could not be started safely',
    message: 'Refresh the editorial decision before trying again.',
    retryable: false,
  }
  if (status === 401) return {
    title: 'Session ended',
    message: 'Sign in to Panda Pages to continue in Story Studio.',
    retryable: false,
  }
  if (status === 403) return {
    title: 'Admin access is unavailable',
    message: 'Administrator access is not available for this request.',
    retryable: false,
  }
  if (status === 404) return {
    title: 'Generation or approval unavailable',
    message: 'This generation run or editorial approval is no longer available.',
    retryable: false,
  }
  if (status === 409) return {
    title: 'Editable drafts could not be created',
    message: 'The editorial decision is being refreshed. Existing working content was not replaced.',
    retryable: false,
  }
  if (status === 500) return {
    title: 'Editable drafts could not be created',
    message: 'The service encountered an internal error. Try again after refreshing the editorial decision.',
    retryable: true,
  }
  if (status === 503) return {
    title: 'Editable draft creation is unavailable',
    message: 'The service is temporarily unavailable. Try again later.',
    retryable: true,
  }
  return {
    title: 'Draft result could not be confirmed',
    message: 'Refresh the editorial decision before retrying. If this approval remains current, retrying is safe.',
    retryable: true,
  }
}

async function moveToSignIn() {
  await router.replace({
    path: '/account/login',
    query: { next: `/admin/stories/${encodeURIComponent(props.storySlug)}` },
  })
}

function openConfirmation() {
  const approval = props.currentApprovedReview
  if (!approval || !actionAvailable.value) return
  actionError.value = null
  result.value = null
  pendingAuthorization.value = {
    runId: props.runId,
    editorialReviewId: approval.id,
  }
}

function cancelConfirmation() {
  if (submitting.value) return
  pendingAuthorization.value = null
}

async function confirmDraftIngest() {
  const authorization = pendingAuthorization.value
  if (!authorization || submitting.value) return
  if (
    props.runId !== authorization.runId ||
    props.currentApprovedReview?.id !== authorization.editorialReviewId ||
    props.reviewMutationPending
  ) {
    pendingAuthorization.value = null
    actionError.value = {
      title: 'Editorial decision changed',
      message: 'Refresh the current decision and confirm draft creation again.',
      retryable: false,
    }
    return
  }

  submitting.value = true
  actionError.value = null
  const controller = new AbortController()
  requestController = controller
  const generation = ++requestGeneration
  try {
    const created = await adminCreateStoryOrchestrationDraftIngest(
      authorization.runId,
      authorization.editorialReviewId,
      props.storySlug,
      controller.signal,
    )
    if (!ownsRequest(authorization, controller, generation)) return
    pendingAuthorization.value = null
    result.value = created
  } catch (caught) {
    if (!ownsRequest(authorization, controller, generation)) return
    pendingAuthorization.value = null
    const projected = projectDraftIngestError(caught)
    actionError.value = projected
    if (getAPIErrorStatus(caught) === 409) emit('refreshEditorialHistory')
    if (getAPIErrorStatus(caught) === 401) await moveToSignIn()
  } finally {
    if (requestController === controller && requestGeneration === generation) {
      submitting.value = false
      requestController = null
    }
  }
}

function retry() {
  if (actionAvailable.value) openConfirmation()
}

async function editDrafts() {
  const completed = result.value
  if (!completed || completed.storySlug !== props.storySlug) return
  await router.push({
    name: 'admin-story-edit',
    params: { slug: completed.storySlug },
    query: { edition: 'confident-readers' },
  })
}

onBeforeUnmount(() => {
  active = false
  requestGeneration += 1
  requestController?.abort()
  requestController = null
})
</script>

<template>
  <section class="draft-ingest" aria-labelledby="draft-ingest-title" :aria-busy="submitting">
    <div class="draft-ingest__heading">
      <div>
        <h5 id="draft-ingest-title">Editable drafts</h5>
        <p>Copy this approved generated run into four editable Story Studio drafts. Nothing is published.</p>
      </div>
      <button type="button" class="studio-button studio-button--primary" :disabled="!actionAvailable" @click="openConfirmation">
        Create editable drafts
      </button>
    </div>

    <p v-if="unavailableHint" class="draft-ingest__hint">{{ unavailableHint }}</p>
    <p v-if="result?.outcome === 'created'" class="draft-ingest__success" role="status">
      Editable drafts created. Nothing was published.
    </p>
    <p v-else-if="result?.outcome === 'reused'" class="draft-ingest__success" role="status">
      This exact ingest already exists. Open the current editable drafts.
    </p>
    <button v-if="result" type="button" class="studio-button studio-button--quiet draft-ingest__edit" @click="editDrafts">Edit drafts</button>

    <div v-if="actionError" class="draft-ingest__error" role="alert">
      <div><strong>{{ actionError.title }}</strong><p>{{ actionError.message }}</p></div>
      <button v-if="actionError.retryable" type="button" class="studio-button studio-button--quiet" :disabled="!actionAvailable" @click="retry">Try again</button>
    </div>
  </section>

  <StoryStudioDialog
    :open="pendingAuthorization !== null"
    :title="confirmationTitle"
    description="This uses this exact approved generation run."
    confirm-label="Create editable drafts"
    :busy="submitting"
    @confirm="confirmDraftIngest"
    @cancel="cancelConfirmation"
  >
    <p>The four generated adaptations will be copied into editable Story Studio drafts. Classic remains unchanged. The generated run and evidence remain immutable. Nothing will be published or released.</p>
  </StoryStudioDialog>
</template>

<style scoped>
.draft-ingest{margin-top:1rem;border-top:1px solid var(--studio-line);padding-top:1rem}.draft-ingest__heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.draft-ingest h5{font-family:var(--panda-serif);font-size:1rem;font-weight:650}.draft-ingest__heading p,.draft-ingest__hint{margin-top:.3rem;color:var(--studio-muted);line-height:1.5}.draft-ingest__hint{border-left:3px solid var(--panda-line-strong);padding-left:.8rem}.draft-ingest__success{margin-top:1rem;border-left:3px solid var(--panda-success);padding-left:.8rem;color:var(--panda-success);font-weight:700;line-height:1.5}.draft-ingest__edit{margin-top:.8rem}.draft-ingest__error{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-top:1rem;border:1px solid var(--panda-danger);border-radius:var(--panda-radius-compact);background:var(--panda-danger-surface);padding:.75rem .85rem;color:var(--panda-danger)}.draft-ingest__error p{margin-top:.2rem;line-height:1.45}@media(max-width:780px){.draft-ingest__heading,.draft-ingest__error{align-items:stretch;flex-direction:column}.draft-ingest__heading .studio-button,.draft-ingest__error .studio-button,.draft-ingest__edit{width:100%}}
</style>
