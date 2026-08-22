<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import StoryOrchestrationEditorialReview from './StoryOrchestrationEditorialReview.vue'
import StoryStudioDialog from './StoryStudioDialog.vue'
import {
  adminGenerateSourceVersion,
  adminGetSource,
  adminGetStoryOrchestrationRun,
  adminListStoryOrchestrationRuns,
  type AdminGeneratedEditionArtifact,
  type AdminGeneratedEditionKey,
  type AdminSemanticAssessmentArtifact,
  type AdminStoryOrchestrationRun,
  type AdminStoryOrchestrationRunSummary,
  type AdminStorySourceSummary,
  type AdminSourceDetail,
  type AdminSourceVersionSummary,
} from '@/lib/api'
import { renderGeneratedMarkdown } from '@/lib/generated-markdown'
import {
  generatedEditionLabel,
  generatedEditionOrder,
  projectStoryGenerationError,
  type StoryGenerationError,
} from '@/lib/story-studio-navigation'

const props = defineProps<{
  slug: string
  source: AdminStorySourceSummary
}>()

const router = useRouter()
const sourceDetail = ref<AdminSourceDetail | null>(null)
const sourceLoading = ref(false)
const sourceError = ref('')
const selectedSourceVersionID = ref<string | null>(null)
const history = ref<AdminStoryOrchestrationRunSummary[]>([])
const historyLoading = ref(false)
const historyError = ref<StoryGenerationError | null>(null)
const selectedRunID = ref<string | null>(null)
const selectedRun = ref<AdminStoryOrchestrationRun | null>(null)
const runLoading = ref(false)
const runError = ref<StoryGenerationError | null>(null)
const activeEditionKey = ref<AdminGeneratedEditionKey>('confident-readers')
const generating = ref(false)
const generationError = ref<StoryGenerationError | null>(null)
const leaveDialogOpen = ref(false)

let sourceGeneration = 0
let historyGeneration = 0
let runGeneration = 0
let sourceController: AbortController | null = null
let historyController: AbortController | null = null
let runController: AbortController | null = null
let generationController: AbortController | null = null
let pendingRoute: string | null = null
let allowRouteLeave = false

const selectedSourceVersion = computed<AdminSourceVersionSummary | null>(() =>
  sourceDetail.value?.versions.find((version) => version.versionId === selectedSourceVersionID.value) ?? null,
)
const sourceReadyForSelection = computed(() =>
  props.source.status === 'ready' && props.source.currentVersion !== null,
)
const selectedEdition = computed<AdminGeneratedEditionArtifact | null>(() =>
  selectedRun.value?.editions.find((edition) => edition.EditionKey === activeEditionKey.value) ?? null,
)
const selectedAssessment = computed<AdminSemanticAssessmentArtifact | null>(() =>
  selectedRun.value?.editionAssessments.find((assessment) => assessment.EditionKey === activeEditionKey.value) ?? null,
)
const renderedEdition = computed(() => {
  if (!selectedEdition.value) return null
  try {
    return renderGeneratedMarkdown(selectedEdition.value.Markdown)
  } catch {
    return null
  }
})
const generationHint = computed(() => {
  if (!selectedSourceVersion.value) return 'Choose a canonical source revision to load its recent generations.'
  if (!selectedSourceVersion.value.provenance) {
    return 'This revision has no recorded provider-acquisition provenance. Eligibility is verified when generation starts.'
  }
  return 'Eligibility is verified when generation starts. The server remains authoritative.'
})

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('en-GB', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

function resultLabel(result: 'pass' | 'needs_review' | 'fail'): string {
  if (result === 'pass') return 'Pass — machine assessment'
  if (result === 'needs_review') return 'Needs review — machine assessment'
  return 'Fail — machine assessment'
}

function resultClass(result: 'pass' | 'needs_review' | 'fail'): string {
  return `generation-result generation-result--${result.replace('_', '-')}`
}

function abortSourceRequest() {
  sourceGeneration += 1
  sourceController?.abort()
  sourceController = null
}

function abortHistoryRequest() {
  historyGeneration += 1
  historyController?.abort()
  historyController = null
}

function abortRunRequest() {
  runGeneration += 1
  runController?.abort()
  runController = null
}

function clearSelectedRun() {
  abortRunRequest()
  selectedRunID.value = null
  selectedRun.value = null
  runLoading.value = false
  runError.value = null
  activeEditionKey.value = 'confident-readers'
}

async function moveToSignIn() {
  await router.replace({
    path: '/account/login',
    query: { next: `/admin/stories/${encodeURIComponent(props.slug)}` },
  })
}

async function loadSourceVersions() {
  abortSourceRequest()
  clearSelectedRun()
  abortHistoryRequest()
  history.value = []
  historyLoading.value = false
  historyError.value = null
  sourceDetail.value = null
  selectedSourceVersionID.value = null
  sourceError.value = ''

  if (!sourceReadyForSelection.value) return
  sourceLoading.value = true
  const controller = new AbortController()
  sourceController = controller
  const requestGeneration = ++sourceGeneration
  try {
    const detail = await adminGetSource(props.slug, controller.signal)
    if (requestGeneration !== sourceGeneration || controller.signal.aborted) return
    sourceDetail.value = detail
    selectedSourceVersionID.value = detail.currentVersion?.versionId
      ?? detail.versions.find((version) => version.isCurrent)?.versionId
      ?? null
  } catch {
    if (requestGeneration !== sourceGeneration || controller.signal.aborted) return
    sourceError.value = 'Canonical source revisions could not be loaded. Try reopening this story.'
  } finally {
    if (requestGeneration === sourceGeneration) sourceLoading.value = false
  }
}

async function loadHistory(sourceVersionID = selectedSourceVersionID.value) {
  abortHistoryRequest()
  clearSelectedRun()
  history.value = []
  historyError.value = null
  if (!sourceVersionID) return

  historyLoading.value = true
  const controller = new AbortController()
  historyController = controller
  const requestGeneration = ++historyGeneration
  try {
    const response = await adminListStoryOrchestrationRuns(sourceVersionID, undefined, controller.signal)
    if (
      requestGeneration !== historyGeneration ||
      controller.signal.aborted ||
      selectedSourceVersionID.value !== sourceVersionID
    ) return
    history.value = response.items
  } catch (caught) {
    if (requestGeneration !== historyGeneration || controller.signal.aborted) return
    const projected = projectStoryGenerationError(caught, 'history')
    historyError.value = projected
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    if (requestGeneration === historyGeneration) historyLoading.value = false
  }
}

async function selectRun(summary: AdminStoryOrchestrationRunSummary) {
  const expectedSourceVersionID = selectedSourceVersionID.value
  if (!expectedSourceVersionID || summary.sourceVersionId !== expectedSourceVersionID) return
  abortRunRequest()
  selectedRunID.value = summary.id
  selectedRun.value = null
  runError.value = null
  runLoading.value = true
  const controller = new AbortController()
  runController = controller
  const requestGeneration = ++runGeneration
  try {
    const run = await adminGetStoryOrchestrationRun(summary.id, controller.signal)
    if (
      requestGeneration !== runGeneration ||
      controller.signal.aborted ||
      selectedSourceVersionID.value !== expectedSourceVersionID ||
      selectedRunID.value !== summary.id
    ) return
    if (
      run.sourceVersionId !== expectedSourceVersionID ||
      (summary.sourceSha256 !== '' && run.sourceSha256 !== summary.sourceSha256)
    ) {
      throw new Error('Invalid orchestration run identity')
    }
    selectedRun.value = run
    activeEditionKey.value = generatedEditionOrder[0]
  } catch (caught) {
    if (requestGeneration !== runGeneration || controller.signal.aborted) return
    const projected = projectStoryGenerationError(caught, 'detail')
    runError.value = projected
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    if (requestGeneration === runGeneration) runLoading.value = false
  }
}

async function generate() {
  const sourceVersionID = selectedSourceVersionID.value
  if (!sourceVersionID || generating.value) return
  generationError.value = null
  generating.value = true
  const controller = new AbortController()
  generationController = controller
  try {
    const response = await adminGenerateSourceVersion(sourceVersionID, controller.signal)
    if (controller.signal.aborted || selectedSourceVersionID.value !== sourceVersionID) return
    await loadHistory(sourceVersionID)
    if (controller.signal.aborted || selectedSourceVersionID.value !== sourceVersionID) return
    const summary = history.value.find((candidate) => candidate.id === response.id)
    if (summary) await selectRun(summary)
    else await selectRun({
      id: response.id,
      sourceVersionId: response.sourceVersionId,
      sourceSha256: selectedRun.value?.sourceSha256 ?? '',
      semanticResult: response.semanticResult,
      createdAt: response.createdAt,
    })
  } catch (caught) {
    if (controller.signal.aborted) return
    const projected = projectStoryGenerationError(caught, 'generation')
    generationError.value = projected
    if (projected.kind === 'session') await moveToSignIn()
  } finally {
    if (generationController === controller) {
      generating.value = false
      generationController = null
    }
  }
}

function selectEdition(key: AdminGeneratedEditionKey) {
  activeEditionKey.value = key
}

function onEditionKeydown(event: KeyboardEvent, current: AdminGeneratedEditionKey) {
  const index = generatedEditionOrder.indexOf(current)
  if (index < 0) return
  let target: AdminGeneratedEditionKey | null = null
  if (event.key === 'ArrowRight') target = generatedEditionOrder[(index + 1) % generatedEditionOrder.length]
  if (event.key === 'ArrowLeft') target = generatedEditionOrder[(index - 1 + generatedEditionOrder.length) % generatedEditionOrder.length]
  if (event.key === 'Home') target = generatedEditionOrder[0]
  if (event.key === 'End') target = generatedEditionOrder.at(-1) ?? null
  if (!target) return
  event.preventDefault()
  activeEditionKey.value = target
  void nextTick(() => document.getElementById(`generation-edition-tab-${target}`)?.focus())
}

function retryHistory() {
  void loadHistory()
}

function retryRun() {
  const summary = history.value.find((item) => item.id === selectedRunID.value)
  if (summary) void selectRun(summary)
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (!generating.value) return
  event.preventDefault()
  event.returnValue = ''
}

function cancelLeave() {
  leaveDialogOpen.value = false
  pendingRoute = null
}

async function confirmLeave() {
  const destination = pendingRoute
  leaveDialogOpen.value = false
  pendingRoute = null
  generationController?.abort()
  if (!destination) return
  allowRouteLeave = true
  try {
    await router.push(destination)
  } finally {
    allowRouteLeave = false
  }
}

watch(
  () => [props.slug, props.source.status, props.source.currentVersion?.versionId] as const,
  () => { void loadSourceVersions() },
  { immediate: true },
)

watch(selectedSourceVersionID, (sourceVersionID, previous) => {
  if (sourceVersionID === previous) return
  void loadHistory(sourceVersionID)
})

onBeforeRouteLeave((to, from) => {
  if (!generating.value || allowRouteLeave || to.fullPath === from.fullPath) return true
  pendingRoute = to.fullPath
  leaveDialogOpen.value = true
  return false
})

onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  abortSourceRequest()
  abortHistoryRequest()
  abortRunRequest()
  generationController?.abort()
})
</script>

<template>
  <section class="studio-panel generation-review" aria-labelledby="generation-review-title" :aria-busy="generating">
    <div class="generation-review__heading">
      <div>
        <p class="studio-page-heading__eyebrow">Adaptation generation</p>
        <h2 id="generation-review-title">Generate and review adaptations</h2>
        <p>Generated editions remain immutable orchestration evidence. Machine assessment is not editorial approval.</p>
      </div>
      <button
        type="button"
        class="studio-button studio-button--primary"
        :disabled="!selectedSourceVersionID || sourceLoading || generating"
        @click="generate"
      >
        {{ generating ? 'Generating adaptations…' : history.length ? 'Generate again' : 'Generate adaptations' }}
      </button>
    </div>

    <div class="studio-field generation-review__source-field">
      <label for="generation-source-version">Canonical source revision <span>Required</span></label>
      <select
        id="generation-source-version"
        v-model="selectedSourceVersionID"
        :disabled="sourceLoading || generating || !sourceDetail"
      >
        <option v-if="!selectedSourceVersionID" :value="null" disabled>Select a source revision</option>
        <option v-for="version in sourceDetail?.versions ?? []" :key="version.versionId" :value="version.versionId">
          r{{ version.version }}{{ version.isCurrent ? ' · Current' : '' }} · {{ version.createdAt }}
        </option>
      </select>
      <p class="studio-field__hint">{{ generationHint }}</p>
    </div>

    <p v-if="sourceLoading" class="generation-review__status" role="status">Loading canonical source revisions…</p>
    <p v-else-if="sourceError" class="generation-review__error" role="alert">{{ sourceError }}</p>
    <p v-else-if="props.source.status === 'missing'" class="generation-review__hint">Add a canonical source before generating adaptations.</p>
    <p v-else-if="props.source.status === 'repair_required'" class="generation-review__hint">Canonical source provenance needs attention before generation can start.</p>

    <p v-if="generating" class="generation-review__progress" role="status" aria-live="polite">
      Generating four adaptations. This can take several minutes.
    </p>
    <div v-if="generationError" class="generation-review__error" role="alert">
      <div><strong>{{ generationError.title }}</strong><p>{{ generationError.message }}</p></div>
      <button v-if="generationError.retryable" type="button" class="studio-button studio-button--quiet" @click="generate">Try again</button>
    </div>

    <div class="generation-review__history" aria-labelledby="generation-history-title">
      <div class="generation-review__section-heading">
        <div><h3 id="generation-history-title">Recent generations</h3><p>Newest first for this source revision.</p></div>
        <button v-if="selectedSourceVersionID && !historyLoading" type="button" class="generation-review__refresh" @click="retryHistory">Refresh</button>
      </div>
      <p v-if="historyLoading" class="generation-review__status" role="status">Loading recent generations…</p>
      <div v-else-if="historyError" class="generation-review__error" role="alert">
        <div><strong>{{ historyError.title }}</strong><p>{{ historyError.message }}</p></div>
        <button v-if="historyError.retryable" type="button" class="studio-button studio-button--quiet" @click="retryHistory">Try again</button>
      </div>
      <p v-else-if="selectedSourceVersionID && history.length === 0" class="generation-review__hint">No generations for this source revision yet.</p>
      <ol v-else-if="history.length" class="generation-history-list">
        <li v-for="item in history" :key="item.id">
          <button
            type="button"
            class="generation-history-list__item"
            :class="{ 'generation-history-list__item--selected': selectedRunID === item.id }"
            :aria-current="selectedRunID === item.id ? 'true' : undefined"
            @click="selectRun(item)"
          >
            <time :datetime="item.createdAt">{{ formatDate(item.createdAt) }}</time>
            <span :class="resultClass(item.semanticResult)">{{ resultLabel(item.semanticResult) }}</span>
          </button>
        </li>
      </ol>
    </div>

    <section v-if="selectedRunID" class="generation-run" aria-labelledby="generation-run-title">
      <p v-if="runLoading" class="generation-review__status" role="status">Opening generation run…</p>
      <div v-else-if="runError" class="generation-review__error" role="alert">
        <div><strong>{{ runError.title }}</strong><p>{{ runError.message }}</p></div>
        <button v-if="runError.retryable" type="button" class="studio-button studio-button--quiet" @click="retryRun">Try again</button>
      </div>
      <template v-else-if="selectedRun">
        <header class="generation-run__heading">
          <div>
            <p class="studio-page-heading__eyebrow">Generation run</p>
            <h3 id="generation-run-title">Generated adaptations</h3>
            <p><time :datetime="selectedRun.createdAt">{{ formatDate(selectedRun.createdAt) }}</time> · Source revision <code>{{ selectedRun.sourceVersionId }}</code></p>
          </div>
          <span :class="resultClass(selectedRun.semanticResult)">{{ resultLabel(selectedRun.semanticResult) }}</span>
        </header>

        <StoryOrchestrationEditorialReview
          :key="selectedRun.id"
          :run-id="selectedRun.id"
          :story-slug="props.slug"
        />

        <div class="generation-tabs" role="tablist" aria-label="Generated editions">
          <button
            v-for="key in generatedEditionOrder"
            :id="`generation-edition-tab-${key}`"
            :key="key"
            type="button"
            role="tab"
            :aria-selected="activeEditionKey === key"
            :aria-controls="`generation-edition-panel-${key}`"
            :tabindex="activeEditionKey === key ? 0 : -1"
            @click="selectEdition(key)"
            @keydown="onEditionKeydown($event, key)"
          >{{ generatedEditionLabel(key) }}</button>
        </div>

        <article
          v-if="selectedEdition && selectedAssessment"
          :id="`generation-edition-panel-${activeEditionKey}`"
          class="generation-edition"
          role="tabpanel"
          :aria-labelledby="`generation-edition-tab-${activeEditionKey}`"
          tabindex="0"
        >
          <div class="generation-edition__heading">
            <div><p class="studio-page-heading__eyebrow">Generated story</p><h4>{{ generatedEditionLabel(activeEditionKey) }}</h4></div>
            <span :class="resultClass(selectedAssessment.Assessment.result)">{{ resultLabel(selectedAssessment.Assessment.result) }}</span>
          </div>
          <div v-if="renderedEdition" class="studio-rendered-story generation-edition__prose" v-html="renderedEdition" />
          <p v-else class="generation-review__error" role="alert">Generated Markdown could not be rendered safely.</p>

          <section class="generation-assessment" :aria-labelledby="`generation-assessment-${activeEditionKey}`">
            <h5 :id="`generation-assessment-${activeEditionKey}`">Edition assessment</h5>
            <p>Machine assessment for this generated edition only.</p>
            <p v-if="selectedAssessment.Assessment.findings.length === 0" class="generation-review__hint">No semantic findings were recorded.</p>
            <ol v-else class="generation-findings">
              <li v-for="(finding, findingIndex) in selectedAssessment.Assessment.findings" :key="`${finding.code}-${findingIndex}`">
                <strong>{{ finding.severity === 'blocking' ? 'Blocking' : 'Review' }} · {{ finding.code }}</strong>
                <p>{{ finding.message }}</p>
                <details>
                  <summary>Evidence ({{ finding.evidence.length }})</summary>
                  <ol class="generation-evidence">
                    <li v-for="(evidence, evidenceIndex) in finding.evidence" :key="`${evidence.location}-${evidenceIndex}`">
                      <p><strong>{{ evidence.location.replace('_', ' ') }}</strong><span v-if="evidence.editionKey"> · {{ generatedEditionLabel(evidence.editionKey) }}</span></p>
                      <blockquote>{{ evidence.excerpt }}</blockquote>
                      <p>{{ evidence.explanation }}</p>
                    </li>
                  </ol>
                </details>
              </li>
            </ol>
          </section>
        </article>

        <section class="generation-bundle" aria-labelledby="generation-bundle-title">
          <div class="generation-bundle__heading"><div><h4 id="generation-bundle-title">Bundle assessment</h4><p>Machine assessment of the generated set as a whole.</p></div><span :class="resultClass(selectedRun.bundleAssessment.Assessment.result)">{{ resultLabel(selectedRun.bundleAssessment.Assessment.result) }}</span></div>
          <p v-if="selectedRun.bundleAssessment.Assessment.findings.length === 0" class="generation-review__hint">No bundle findings were recorded.</p>
          <ol v-else class="generation-findings">
            <li v-for="(finding, findingIndex) in selectedRun.bundleAssessment.Assessment.findings" :key="`${finding.code}-${findingIndex}`">
              <strong>{{ finding.severity === 'blocking' ? 'Blocking' : 'Review' }} · {{ finding.code }}</strong>
              <p>{{ finding.message }}</p>
              <details><summary>Evidence ({{ finding.evidence.length }})</summary><ol class="generation-evidence"><li v-for="(evidence, evidenceIndex) in finding.evidence" :key="`${evidence.location}-${evidenceIndex}`"><p><strong>{{ evidence.location.replace('_', ' ') }}</strong><span v-if="evidence.editionKey"> · {{ generatedEditionLabel(evidence.editionKey) }}</span></p><blockquote>{{ evidence.excerpt }}</blockquote><p>{{ evidence.explanation }}</p></li></ol></details>
            </li>
          </ol>
        </section>

        <details class="generation-disclosure">
          <summary>Source analysis</summary>
          <div class="generation-analysis">
            <section><h4>Central plot</h4><p>{{ selectedRun.analysisArtifact.Analysis.centralPlot }}</p></section>
            <section><h4>Characters</h4><ul><li v-for="character in selectedRun.analysisArtifact.Analysis.characters" :key="character.name"><strong>{{ character.name }}</strong> — {{ character.role }}<span v-if="character.explicitMotivations.length">. Motivations: {{ character.explicitMotivations.join('; ') }}</span><span v-if="character.flawsOrAmbiguities.length">. Flaws or ambiguities: {{ character.flawsOrAmbiguities.join('; ') }}</span></li></ul></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.relationships.length"><h4>Relationships</h4><ul><li v-for="(relationship, index) in selectedRun.analysisArtifact.Analysis.relationships" :key="index"><strong>{{ relationship.parties.join(' and ') }}</strong> — {{ relationship.nature }}<span v-if="relationship.powerDynamics">. {{ relationship.powerDynamics }}</span></li></ul></section>
            <section><h4>Core story beats</h4><ol><li v-for="(beat, index) in selectedRun.analysisArtifact.Analysis.coreStoryBeats" :key="index">{{ beat.summary }}</li></ol></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.developmentBeats.length"><h4>Development beats</h4><ol><li v-for="(beat, index) in selectedRun.analysisArtifact.Analysis.developmentBeats" :key="index">{{ beat.summary }}</li></ol></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.enrichmentMaterial.length"><h4>Enrichment material</h4><ol><li v-for="(beat, index) in selectedRun.analysisArtifact.Analysis.enrichmentMaterial" :key="index">{{ beat.summary }}</li></ol></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.causalDependencies.length"><h4>Causal dependencies</h4><ol><li v-for="(dependency, index) in selectedRun.analysisArtifact.Analysis.causalDependencies" :key="index"><strong>{{ dependency.cause }}</strong> → {{ dependency.effect }}. {{ dependency.whyItMatters }}</li></ol></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.iconicMaterial.length"><h4>Iconic material</h4><ul><li v-for="(material, index) in selectedRun.analysisArtifact.Analysis.iconicMaterial" :key="index"><strong>{{ material.kind }}</strong> — {{ material.textOrDescription }}. {{ material.importance }}</li></ul></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.intenseMaterial.length"><h4>Intense material</h4><ul><li v-for="(material, index) in selectedRun.analysisArtifact.Analysis.intenseMaterial" :key="index"><strong>{{ material.kind }}</strong> — {{ material.description }}. {{ material.narrativeFunction }}</li></ul></section>
            <section v-if="selectedRun.analysisArtifact.Analysis.adaptationRisks.length"><h4>Adaptation risks</h4><ul><li v-for="(risk, index) in selectedRun.analysisArtifact.Analysis.adaptationRisks" :key="index"><strong>{{ risk.kind }}</strong> — {{ risk.description }}. Preserve: {{ risk.whatMustBePreserved }}</li></ul></section>
          </div>
        </details>

        <details class="generation-disclosure">
          <summary>Technical provenance</summary>
          <div class="generation-provenance">
            <section><h4>Source analysis</h4><dl><div><dt>Specification</dt><dd>{{ selectedRun.analysisArtifact.SpecificationVersion }}</dd></div><div><dt>Prompt</dt><dd>{{ selectedRun.analysisArtifact.PromptVersion }}</dd></div><div><dt>Requested / returned model</dt><dd>{{ selectedRun.analysisArtifact.RequestedModel }} / {{ selectedRun.analysisArtifact.ReturnedModel }}</dd></div><div><dt>Reasoning effort</dt><dd>{{ selectedRun.analysisArtifact.ReasoningEffort }}</dd></div><div><dt>Response ID</dt><dd><code>{{ selectedRun.analysisArtifact.ResponseID }}</code></dd></div><div><dt>Source / analysis SHA-256</dt><dd><code>{{ selectedRun.analysisArtifact.SourceSHA256 }}</code><br><code>{{ selectedRun.analysisArtifact.AnalysisSHA256 }}</code></dd></div><div><dt>Token usage</dt><dd>{{ selectedRun.analysisArtifact.Usage.TotalTokens }} total · {{ selectedRun.analysisArtifact.Usage.InputTokens }} input · {{ selectedRun.analysisArtifact.Usage.OutputTokens }} output · {{ selectedRun.analysisArtifact.Usage.ReasoningTokens }} reasoning</dd></div></dl></section>
            <section v-if="selectedEdition"><h4>{{ generatedEditionLabel(activeEditionKey) }} generation</h4><dl><div><dt>Specification / prompt</dt><dd>{{ selectedEdition.SpecificationVersion }} / {{ selectedEdition.PromptVersion }}</dd></div><div><dt>Requested / returned model</dt><dd>{{ selectedEdition.RequestedModel }} / {{ selectedEdition.ReturnedModel }}</dd></div><div><dt>Reasoning effort</dt><dd>{{ selectedEdition.ReasoningEffort }}</dd></div><div><dt>Response ID</dt><dd><code>{{ selectedEdition.ResponseID }}</code></dd></div><div><dt>Content SHA-256</dt><dd><code>{{ selectedEdition.ContentSHA256 }}</code></dd></div><div><dt>Structural validation</dt><dd>{{ selectedEdition.StructuralValidation.ContractVersion }} · {{ selectedEdition.StructuralValidation.Findings.length ? `${selectedEdition.StructuralValidation.Findings.length} finding(s)` : 'No findings' }}</dd></div><div><dt>Token usage</dt><dd>{{ selectedEdition.Usage.TotalTokens }} total · {{ selectedEdition.Usage.InputTokens }} input · {{ selectedEdition.Usage.OutputTokens }} output</dd></div></dl></section>
            <section v-if="selectedAssessment"><h4>{{ generatedEditionLabel(activeEditionKey) }} semantic assessment</h4><dl><div><dt>Validation / specification / prompt</dt><dd>{{ selectedAssessment.ValidationVersion }} / {{ selectedAssessment.SpecificationVersion }} / {{ selectedAssessment.PromptVersion }}</dd></div><div><dt>Requested / returned model</dt><dd>{{ selectedAssessment.RequestedModel }} / {{ selectedAssessment.ReturnedModel }}</dd></div><div><dt>Reasoning effort</dt><dd>{{ selectedAssessment.ReasoningEffort }}</dd></div><div><dt>Response ID</dt><dd><code>{{ selectedAssessment.ResponseID }}</code></dd></div><div><dt>Assessment SHA-256</dt><dd><code>{{ selectedAssessment.AssessmentSHA256 }}</code></dd></div><div><dt>Token usage</dt><dd>{{ selectedAssessment.Usage.TotalTokens }} total · {{ selectedAssessment.Usage.InputTokens }} input · {{ selectedAssessment.Usage.OutputTokens }} output</dd></div></dl></section>
          </div>
        </details>
      </template>
    </section>
  </section>

  <StoryStudioDialog
    :open="leaveDialogOpen"
    title="Leave while generation is running?"
    description="Leaving this Story Studio page stops the browser request and may cancel generation."
    confirm-label="Leave and stop generation"
    danger
    @confirm="confirmLeave"
    @cancel="cancelLeave"
  >
    <p>Choose Cancel to remain here while Panda Pages generates the four adaptations.</p>
  </StoryStudioDialog>
</template>

<style scoped>
.generation-review{margin-top:1rem}.generation-review__heading,.generation-run__heading,.generation-edition__heading,.generation-bundle__heading,.generation-review__section-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.generation-review h2,.generation-run h3,.generation-edition h4,.generation-bundle h4,.generation-analysis h4,.generation-provenance h4{font-family:var(--panda-serif);font-weight:650}.generation-review h2{font-size:1.35rem}.generation-review__heading>div>p:last-child,.generation-run__heading p:last-child,.generation-review__section-heading p,.generation-assessment>p,.generation-bundle p{margin-top:.3rem;color:var(--studio-muted);line-height:1.5}.generation-review__source-field{max-width:38rem;margin-top:1rem}.generation-review__status,.generation-review__progress,.generation-review__hint{margin-top:1rem;border-left:3px solid var(--panda-line-strong);padding-left:.8rem;color:var(--studio-muted);line-height:1.5}.generation-review__progress{border-color:var(--panda-warning);color:var(--panda-warning);font-weight:700}.generation-review__error{display:flex;align-items:center;justify-content:space-between;gap:1rem;margin-top:1rem;border:1px solid var(--panda-danger);border-radius:var(--panda-radius-compact);background:var(--panda-danger-surface);padding:.85rem 1rem;color:var(--panda-danger)}.generation-review__error p{margin-top:.2rem;line-height:1.45}.generation-review__history,.generation-run{margin-top:1.5rem;border-top:1px solid var(--studio-line);padding-top:1.25rem}.generation-review__section-heading h3,.generation-run h3{font-size:1.15rem}.generation-review__refresh{border:0;background:transparent;color:var(--panda-ink);font-weight:720;text-decoration:underline;text-underline-offset:.2em}.generation-history-list{display:grid;gap:.55rem;margin-top:1rem}.generation-history-list__item{display:flex;width:100%;align-items:center;justify-content:space-between;gap:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);background:var(--panda-paper-raised);padding:.75rem .85rem;text-align:left}.generation-history-list__item:hover,.generation-history-list__item--selected{border-color:var(--panda-ink);background:var(--panda-mist)}.generation-history-list time{color:var(--studio-muted);font-size:.88rem}.generation-result{display:inline-flex;align-items:center;width:max-content;border:1px solid currentColor;border-radius:999px;padding:.28rem .52rem;font-size:.76rem;font-weight:760;line-height:1.15}.generation-result--pass{color:var(--panda-success);background:var(--panda-success-surface)}.generation-result--needs-review{color:var(--panda-warning);background:var(--panda-warning-surface)}.generation-result--fail{color:var(--panda-danger);background:var(--panda-danger-surface)}.generation-run__heading code{overflow-wrap:anywhere;font-size:.72rem}.generation-tabs{display:flex;gap:.45rem;overflow-x:auto;margin-top:1.25rem;border-bottom:1px solid var(--studio-line);padding-bottom:.55rem}.generation-tabs button{flex:0 0 auto;border:1px solid var(--studio-line-strong);border-radius:var(--panda-radius-compact);background:var(--panda-paper-raised);padding:.6rem .75rem;color:var(--panda-ink);font-weight:720}.generation-tabs button[aria-selected='true']{border-color:var(--panda-ink);background:var(--panda-ink);color:var(--panda-white)}.generation-edition{margin-top:1rem;outline:none}.generation-edition__prose{max-width:44rem;margin-top:1.1rem}.generation-assessment,.generation-bundle{margin-top:1.4rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);padding:1rem}.generation-assessment h5{font-size:1rem;font-weight:780}.generation-findings{display:grid;gap:.8rem;margin-top:1rem}.generation-findings>li{border-left:3px solid var(--panda-warning);padding-left:.75rem}.generation-findings p{margin-top:.25rem;color:var(--studio-muted);line-height:1.5}.generation-findings details{margin-top:.55rem}.generation-findings summary,.generation-disclosure>summary{cursor:pointer;font-weight:720}.generation-evidence{display:grid;gap:.75rem;margin-top:.75rem}.generation-evidence li{border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);padding:.75rem}.generation-evidence blockquote{margin-top:.45rem;border-left:3px solid var(--studio-line-strong);padding-left:.75rem;color:var(--panda-soft-ink);font-family:var(--panda-serif);line-height:1.55;white-space:pre-wrap}.generation-disclosure{margin-top:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);padding:.9rem 1rem}.generation-analysis,.generation-provenance{display:grid;gap:1rem;margin-top:1rem}.generation-analysis section{line-height:1.55}.generation-analysis h4,.generation-provenance h4{font-size:1rem}.generation-analysis p,.generation-analysis ul,.generation-analysis ol{margin-top:.45rem}.generation-analysis li+li{margin-top:.35rem}.generation-provenance section+section{border-top:1px solid var(--studio-line);padding-top:1rem}.generation-provenance dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.8rem;margin-top:.65rem}.generation-provenance dt{color:var(--studio-muted);font-size:.72rem;font-weight:700;text-transform:uppercase}.generation-provenance dd{overflow-wrap:anywhere;margin-top:.2rem;line-height:1.45}.generation-provenance code{font-size:.72rem}@media(max-width:780px){.generation-review__heading,.generation-run__heading,.generation-edition__heading,.generation-bundle__heading,.generation-review__section-heading,.generation-review__error{align-items:stretch;flex-direction:column}.generation-review__heading>.studio-button{width:100%}.generation-history-list__item{align-items:flex-start;flex-direction:column}.generation-provenance dl{grid-template-columns:1fr}}
</style>
