<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue"
import { useRouter } from "vue-router"
import StoryStudioState from "@/components/admin/story-studio/StoryStudioState.vue"
import {
  adminCheckSourceEligibility,
  adminGetSourceAcquisition,
  adminGetSourceProviderWork,
  adminListSourceAcquisitions,
  adminPersistSourceAcquisition,
  adminSearchSourceProvider,
  adminUpdateSourceAcquisitionSourceQualityReview,
  getAPIErrorStatus,
  parseAdminEligibility,
  type APIError,
  type AdminCopyrightFactState,
  type AdminSourceAcquisitionDetail,
  type AdminSourceAcquisitionSummary,
  type AdminSourceEligibility,
  type AdminSourceEligibilityHumanEvidence,
  type AdminSourceProviderWork,
  type AdminSourceQualityStatus,
} from "@/lib/api"

type WorkspacePanel = "find" | "saved"
const provider = "project-gutenberg" as const
const router = useRouter()
const panel = ref<WorkspacePanel>("find")
const query = ref("")
const searching = ref(false)
const searchStarted = ref(false)
const works = ref<AdminSourceProviderWork[]>([])
const selectedWork = ref<AdminSourceProviderWork | null>(null)
const selecting = ref(false)
const saving = ref(false)
const eligibilityLoading = ref(false)
const eligibility = ref<AdminSourceEligibility | null>(null)
const savedLoading = ref(false)
const saved = ref<AdminSourceAcquisitionSummary[]>([])
const selectedAcquisition = ref<AdminSourceAcquisitionDetail | null>(null)
const detailLoading = ref(false)
const qualityStatus = ref<AdminSourceQualityStatus>("pending")
const qualityNote = ref("")
const qualitySaving = ref(false)
const error = ref("")
const message = ref("")
const workCategory = ref<"ordinary_literary" | "unknown">("ordinary_literary")
const authorDeathYear = ref<number | undefined>()
const firstPublicationYear = ref<number | undefined>()
const evidenceSource = ref("")
const evidenceFact = ref("")
const evidenceLocator = ref("")
const translation = ref<AdminCopyrightFactState>("unknown")
const additionalTextual = ref<AdminCopyrightFactState>("unknown")
const specialCategory = ref<AdminCopyrightFactState>("unknown")
const unpublishedAtEnd1988 = ref<AdminCopyrightFactState>("unknown")
let searchController: AbortController | null = null
let savedController: AbortController | null = null
let detailController: AbortController | null = null
let eligibilityController: AbortController | null = null

const readyForPromotion = computed(() => selectedAcquisition.value?.eligibility?.overall === "eligible" && selectedAcquisition.value.sourceQuality.status === "approved")
const providerAuthorDeathKnown = computed(() => selectedWork.value !== null && (eligibility.value?.effectiveUkEvidence.authorDeathYear ?? 0) > 0)
const providerTranslationPresent = computed(() => eligibility.value?.effectiveUkEvidence.translation.state === "present")
const providerTextualContributionPresent = computed(() => eligibility.value?.effectiveUkEvidence.additionalTextualContribution.state === "present")

function contributorText(work: { contributors: readonly { name: string; role: string }[] }) { return work.contributors.map(({ name, role }) => role ? name + " (" + role + ")" : name).join(", ") || "No contributor metadata" }
function providerContributorText(contributor: { name: string; role: string; birthYear?: number; deathYear?: number }) { const years = contributor.birthYear || contributor.deathYear ? " (" + (contributor.birthYear ?? "?") + "–" + (contributor.deathYear ?? "?") + ")" : ""; return contributor.name + " — " + contributor.role + years }
function statusLabel(status: string) { return status[0]?.toUpperCase() + status.slice(1) }
function formatSavedAt(value: string) { return new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(new Date(value)) + " UTC" }
function clearFeedback() { error.value = ""; message.value = "" }
async function moveToSignIn() { await router.replace({ path: "/account/login", query: { next: "/admin/source-review" } }) }
async function showError(caught: unknown) {
  if (getAPIErrorStatus(caught) === 401) { await moveToSignIn(); return }
  const code = typeof caught === "object" && caught !== null && "code" in caught ? (caught as { code?: unknown }).code : undefined
  const messages: Record<string, string> = { source_eligibility_invalid: "Check the factual evidence fields and try again.", source_eligibility_blocked: "This source does not meet the current Panda Pages eligibility policy.", source_eligibility_evidence_failed: "Copyright evidence could not be verified safely.", source_eligibility_evidence_timeout: "Copyright evidence took too long to load.", source_provider_work_not_found: "That Project Gutenberg work is no longer available.", source_provider_unavailable: "Project Gutenberg is unavailable right now.", source_acquisition_not_found: "That saved source could not be found." }
  error.value = typeof code === "string" && messages[code] ? messages[code] : "Story Studio could not complete that source review action. Try again."
}
function evidence(fact: string) { return evidenceSource.value.trim() && evidenceFact.value.trim() ? [{ source: evidenceSource.value.trim(), fact, ...(evidenceLocator.value.trim() ? { locator: evidenceLocator.value.trim() } : {}) }] : [] }
function humanEvidence(): AdminSourceEligibilityHumanEvidence {
  return { workCategory: workCategory.value, workCategoryReferences: evidence("Work is an ordinary literary work."), ...(providerAuthorDeathKnown.value || authorDeathYear.value === undefined ? {} : { authorDeathYear: authorDeathYear.value, authorDeathReferences: evidence("Author death year." ) }), ...(firstPublicationYear.value === undefined ? {} : { firstPublicationYear: firstPublicationYear.value, firstPublicationReferences: evidence("First publication year." ) }), translation: { state: translation.value, references: evidence("Translation status for the acquired text." ) }, additionalTextualContribution: { state: additionalTextual.value, references: evidence("Additional textual-contribution status for the acquired text." ) }, specialCategory: { state: specialCategory.value, references: evidence("Special-category status." ) }, unpublishedAtEnd1988: { state: unpublishedAtEnd1988.value, references: evidence("Publication history at the end of 1988." ) } }
}
function invalidateEligibility() { eligibility.value = null }
async function search() {
  const term = query.value.trim(); searchStarted.value = true; clearFeedback(); selectedWork.value = null; invalidateEligibility()
  if (term.length < 2) { works.value = []; error.value = "Enter at least two characters to search Project Gutenberg."; return }
  const controller = new AbortController(); searchController?.abort(); searchController = controller; searching.value = true
  try { const response = await adminSearchSourceProvider(provider, term, controller.signal); if (!controller.signal.aborted && searchController === controller) works.value = response.results } catch (caught) { if (!controller.signal.aborted && searchController === controller) await showError(caught) } finally { if (searchController === controller) { searching.value = false; searchController = null } }
}
async function selectWork(work: AdminSourceProviderWork) {
  clearFeedback(); selecting.value = true; selectedWork.value = null; invalidateEligibility()
  try { selectedWork.value = await adminGetSourceProviderWork(provider, work.externalId); await loadEligibility() } catch (caught) { await showError(caught) } finally { selecting.value = false }
}
async function loadEligibility() {
  const work = selectedWork.value; if (!work) return
  const controller = new AbortController(); eligibilityController?.abort(); eligibilityController = controller; eligibilityLoading.value = true
  try { const result = await adminCheckSourceEligibility(provider, work.externalId, humanEvidence(), controller.signal); if (!controller.signal.aborted && eligibilityController === controller) eligibility.value = result } catch (caught) { if (!controller.signal.aborted && eligibilityController === controller) await showError(caught) } finally { if (eligibilityController === controller) { eligibilityLoading.value = false; eligibilityController = null } }
}
function applyQualityDraft(detail: AdminSourceAcquisitionDetail) { qualityStatus.value = detail.sourceQuality.status; qualityNote.value = detail.sourceQuality.note ?? "" }
async function loadSaved() {
  const controller = new AbortController(); savedController?.abort(); savedController = controller; savedLoading.value = true
  try { const response = await adminListSourceAcquisitions(controller.signal); if (!controller.signal.aborted && savedController === controller) saved.value = response.items } catch (caught) { if (!controller.signal.aborted && savedController === controller) await showError(caught) } finally { if (savedController === controller) { savedLoading.value = false; savedController = null } }
}
async function openSaved(id: string, preserveFeedback = false) {
  if (!preserveFeedback) clearFeedback(); const controller = new AbortController(); detailController?.abort(); detailController = controller; detailLoading.value = true
  try { const detail = await adminGetSourceAcquisition(id, controller.signal); if (controller.signal.aborted || detailController !== controller) return; selectedAcquisition.value = detail; applyQualityDraft(detail); panel.value = "saved" } catch (caught) { if (!controller.signal.aborted && detailController === controller) await showError(caught) } finally { if (detailController === controller) { detailLoading.value = false; detailController = null } }
}
async function saveForReview() {
  const work = selectedWork.value; if (!work || saving.value) return; clearFeedback(); saving.value = true
  try { const result = await adminPersistSourceAcquisition(provider, work.externalId, humanEvidence()); message.value = result.outcome === "created" ? "Saved for source review." : "This exact saved source already exists. Opening it for review."; await loadSaved(); await openSaved(result.acquisition.id, true) } catch (caught) { const body = (caught as APIError).body; try { const record = body as { eligibility?: unknown }; if (record && record.eligibility !== undefined) eligibility.value = parseAdminEligibility(record.eligibility) } catch { /* retain the safe API error if its optional assessment is malformed */ } await showError(caught) } finally { saving.value = false }
}
async function saveQuality() {
  const detail = selectedAcquisition.value; if (!detail || qualitySaving.value) return; const note = qualityNote.value.trim(); clearFeedback(); if (qualityStatus.value !== "pending" && !note) { error.value = "Add a rationale before approving or rejecting a source."; return }
  qualitySaving.value = true
  try { const summary = await adminUpdateSourceAcquisitionSourceQualityReview(detail.id, { status: qualityStatus.value, note }); selectedAcquisition.value = { ...summary, sourceText: detail.sourceText }; applyQualityDraft(selectedAcquisition.value); saved.value = saved.value.map((item) => item.id === summary.id ? summary : item); message.value = "Source quality review updated." } catch (caught) { await showError(caught) } finally { qualitySaving.value = false }
}
function changePanel(next: WorkspacePanel) { panel.value = next; clearFeedback(); if (next === "saved") void loadSaved() }
onMounted(loadSaved)
onBeforeUnmount(() => { searchController?.abort(); savedController?.abort(); detailController?.abort(); eligibilityController?.abort() })
</script>

<template>
  <div class="source-review">
    <header class="studio-page-heading"><p class="studio-page-heading__eyebrow">Story Studio</p><h1>Source review</h1><p class="studio-page-heading__summary">Find Project Gutenberg source material and review durable evidence before future canonical-source promotion.</p></header>
    <div class="source-review__tabs" role="tablist" aria-label="Source review workspace"><button type="button" role="tab" v-bind:aria-selected="panel === 'find'" v-on:click="changePanel('find')">Find a source</button><button type="button" role="tab" v-bind:aria-selected="panel === 'saved'" v-on:click="changePanel('saved')">Saved sources</button></div>
    <p v-if="message" class="source-review__message" role="status">{{ message }}</p><p v-if="error" class="source-review__error" role="alert">{{ error }}</p>
    <section v-if="panel === 'find'"><div class="studio-panel"><h2>Search Project Gutenberg</h2><form class="source-review__search" v-on:submit.prevent="search"><div class="studio-field"><label for="source-provider-search">Search Project Gutenberg</label><input id="source-provider-search" v-model="query" type="search" placeholder="Title or author" /></div><button type="submit" class="studio-button studio-button--primary" v-bind:disabled="searching">{{ searching ? 'Searching…' : 'Search' }}</button></form></div>
      <StoryStudioState v-if="searching" kind="loading" title="Searching Project Gutenberg" message="Finding provider works." /><StoryStudioState v-else-if="searchStarted && works.length === 0 && !error" kind="empty" title="No matching works" message="Try another title or author search." />
      <ol v-else-if="works.length" class="source-review__results"><li v-for="work in works" v-bind:key="work.externalId" class="studio-panel source-result"><div><h3>{{ work.title }}</h3><p>{{ contributorText(work) }}</p><p>Project Gutenberg #{{ work.externalId }}</p></div><button type="button" class="studio-button studio-button--quiet" v-on:click="selectWork(work)">Select work</button></li></ol>
      <section v-if="selectedWork" class="studio-panel source-review__work"><h2>{{ selectedWork.title }}</h2><p>{{ contributorText(selectedWork) }} · Project Gutenberg #{{ selectedWork.externalId }}</p><a v-bind:href="selectedWork.landingUrl" target="_blank" rel="noreferrer">Open provider page</a><h3>Copyright eligibility</h3><StoryStudioState v-if="eligibilityLoading" kind="loading" title="Checking provider evidence" message="Loading current Project Gutenberg evidence." /><div v-else-if="eligibility"><p>United States: <strong>{{ statusLabel(eligibility.us.status) }}</strong> · United Kingdom: <strong>{{ statusLabel(eligibility.uk.status) }}</strong><br>{{ eligibility.us.reason }} · {{ eligibility.uk.reason }}</p><p>Provider rights evidence: OPDS {{ eligibility.opdsRights }} · RDF {{ eligibility.rdfRights }}.</p><ul v-if="eligibility.contributors.length"><li v-for="contributor in eligibility.contributors" v-bind:key="contributor.name + contributor.role">{{ providerContributorText(contributor) }}</li></ul></div><p v-else>Changing factual evidence requires a fresh server evaluation.</p><h3>UK factual evidence</h3><p>Provide facts and their evidence source, not a legal conclusion. Citation locators are never fetched.</p><div class="source-review__review-fields"><div class="studio-field"><label for="work-category">Work type</label><select id="work-category" v-model="workCategory" v-on:change="invalidateEligibility"><option value="ordinary_literary">Ordinary literary work</option><option value="unknown">Unknown</option></select></div><div class="studio-field"><label for="first-publication">First publication year</label><input id="first-publication" v-model.number="firstPublicationYear" type="number" min="1" v-on:input="invalidateEligibility" /></div><div v-if="!providerAuthorDeathKnown" class="studio-field"><label for="author-death">Author death year</label><input id="author-death" v-model.number="authorDeathYear" type="number" min="1" v-on:input="invalidateEligibility" /></div><p v-else>Provider author death year: {{ eligibility?.effectiveUkEvidence.authorDeathYear }}</p><div class="studio-field"><label for="evidence-source">Evidence source</label><input id="evidence-source" v-model="evidenceSource" v-on:input="invalidateEligibility" /></div><div class="studio-field"><label for="evidence-fact">Observed fact</label><input id="evidence-fact" v-model="evidenceFact" v-on:input="invalidateEligibility" /></div><div class="studio-field"><label for="evidence-locator">Evidence locator (optional)</label><input id="evidence-locator" v-model="evidenceLocator" type="url" placeholder="https://…" v-on:input="invalidateEligibility" /></div><p v-if="providerTranslationPresent">Project Gutenberg reports a translator. This narrow policy cannot pass this source.</p><div v-else class="studio-field"><label for="translation">Translation</label><select id="translation" v-model="translation" v-on:change="invalidateEligibility"><option value="none_confirmed">None confirmed</option><option value="present">Present</option><option value="unknown">Unknown</option></select></div><p v-if="providerTextualContributionPresent">Project Gutenberg reports a possible additional textual contributor. This narrow policy cannot pass this source.</p><div v-else class="studio-field"><label for="textual">Additional textual contribution</label><select id="textual" v-model="additionalTextual" v-on:change="invalidateEligibility"><option value="none_confirmed">None confirmed</option><option value="present">Present</option><option value="unknown">Unknown</option></select></div><div class="studio-field"><label for="special">Special category</label><select id="special" v-model="specialCategory" v-on:change="invalidateEligibility"><option value="none_confirmed">None confirmed</option><option value="present">Present</option><option value="unknown">Unknown</option></select></div><div class="studio-field"><label for="unpublished">Unpublished at end of 1988</label><select id="unpublished" v-model="unpublishedAtEnd1988" v-on:change="invalidateEligibility"><option value="none_confirmed">None confirmed</option><option value="present">Present</option><option value="unknown">Unknown</option></select></div></div><button type="button" class="studio-button studio-button--primary" v-bind:disabled="saving || eligibilityLoading" v-on:click="saveForReview">{{ saving ? 'Validating and saving…' : 'Validate & save for source review' }}</button></section>
    </section>
    <section v-else><div class="source-review__saved-grid"><div><StoryStudioState v-if="savedLoading && saved.length === 0" kind="loading" title="Loading saved sources" message="Opening durable source acquisitions." /><StoryStudioState v-else-if="saved.length === 0 && !error" kind="empty" title="No sources saved for review yet" message="Search Project Gutenberg to save an exact source acquisition." /><ol v-else class="source-review__saved-list"><li v-for="item in saved" v-bind:key="item.id"><button type="button" v-on:click="openSaved(item.id)"><strong>{{ item.title }}</strong><span>Project Gutenberg #{{ item.externalId }} · {{ formatSavedAt(item.createdAt) }}</span><span>Eligibility: {{ item.eligibility?.overall === 'eligible' ? 'Eligible' : 'Recheck required' }} · Source quality: {{ statusLabel(item.sourceQuality.status) }}</span></button></li></ol></div><StoryStudioState v-if="detailLoading" kind="loading" title="Opening saved source" message="Loading durable source evidence." /><article v-else-if="selectedAcquisition" class="source-review__detail"><header class="studio-panel source-review__detail-heading"><h2>{{ selectedAcquisition.title }}</h2><a v-bind:href="selectedAcquisition.landingUrl" target="_blank" rel="noreferrer">Provider page</a></header><section v-if="readyForPromotion" class="source-review__ready"><h3>Ready for canonical-source promotion</h3><p>This source has completed review. Canonical-source promotion is not available yet.</p></section><section class="studio-panel source-review__rights"><h3>Copyright eligibility</h3><div v-if="selectedAcquisition.eligibility"><p>United States: {{ statusLabel(selectedAcquisition.eligibility.us.status) }} · United Kingdom: {{ statusLabel(selectedAcquisition.eligibility.uk.status) }}<br>Policy: {{ selectedAcquisition.eligibility.policyVersion }} · {{ formatSavedAt(selectedAcquisition.eligibility.evaluatedAt) }}</p><details><summary>Eligibility evidence</summary><p>OPDS: {{ selectedAcquisition.eligibility.opdsRights }} · RDF: {{ selectedAcquisition.eligibility.rdfRights }} · source header: {{ selectedAcquisition.eligibility.headerRights }}</p><ul><li v-for="contributor in selectedAcquisition.eligibility.contributors" v-bind:key="contributor.name + contributor.role">{{ providerContributorText(contributor) }}</li></ul></details></div><p v-else>Eligibility: Recheck required.</p></section><section class="studio-panel source-review__quality"><h3>Source quality review</h3><div class="studio-field"><label for="quality-status">Source quality status</label><select id="quality-status" v-model="qualityStatus"><option value="pending">Pending</option><option value="approved">Approved</option><option value="rejected">Rejected</option></select></div><div v-if="qualityStatus !== 'pending'" class="studio-field"><label for="quality-note">Rationale</label><textarea id="quality-note" v-model="qualityNote" rows="3" /></div><button type="button" class="studio-button studio-button--quiet" v-bind:disabled="qualitySaving" v-on:click="saveQuality">Save source quality review</button></section><details class="studio-panel source-review__provenance"><summary>Source provenance</summary><p v-if="selectedAcquisition.eligibility">RDF evidence digest: <code>{{ selectedAcquisition.eligibility.rdfDigest }}</code></p><p>Snapshot hash: <code>{{ selectedAcquisition.snapshotHash }}</code></p></details><section class="studio-panel source-review__text"><h3>Saved source text</h3><pre tabindex="0">{{ selectedAcquisition.sourceText }}</pre></section></article></div></section>
  </div>
</template>
<style scoped>
.source-review__tabs{display:flex;gap:.45rem;margin-bottom:1.25rem;border-bottom:1px solid var(--studio-line-strong)}.source-review__tabs button{min-height:2.75rem;border-bottom:3px solid transparent;padding:.65rem .9rem;color:var(--studio-muted);font-weight:720}.source-review__tabs button[aria-selected='true']{border-color:var(--panda-ink);color:var(--studio-ink)}.source-review__message,.source-review__error,.source-review__ready{margin-bottom:1rem;border-radius:var(--panda-radius-compact);padding:.85rem 1rem}.source-review__message,.source-review__ready{border:1px solid var(--panda-success);background:var(--panda-success-surface);color:var(--panda-success)}.source-review__error{border:1px solid var(--panda-danger);background:var(--panda-danger-surface);color:var(--panda-danger)}.source-review__intro,.source-review__save-note,.source-review__detail-heading p,.source-review__rights>p,.source-review__quality>p,.source-review__text>p{margin-top:.4rem;color:var(--studio-muted);line-height:1.55}.source-review__search{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:.8rem;margin-top:1rem}.source-review__results{margin-top:1.4rem}.source-review__results>h2{font-family:var(--panda-serif);font-size:1.3rem}.source-review__results ol,.source-review__saved-list{display:grid;gap:.75rem;margin-top:.75rem}.source-result{display:flex;align-items:center;justify-content:space-between;gap:1rem}.source-result h3,.source-review__work h2,.source-review__detail h2{font-family:var(--panda-serif);font-size:1.25rem;font-weight:650}.source-result p{margin-top:.25rem;color:var(--studio-muted)}.source-result__meta,.source-review__muted{font-size:.84rem}.source-review__work{margin-top:1.4rem}.source-review__work-heading,.source-review__detail-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.source-review__work-metadata{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.75rem;margin:1rem 0}.source-review__work-metadata dt,.source-review__provenance dt{color:var(--studio-muted);font-size:.75rem;font-weight:720;letter-spacing:.05em;text-transform:uppercase}.source-review__work-metadata dd,.source-review__provenance dd{overflow-wrap:anywhere;margin-top:.2rem}.source-review__saved-grid{display:grid;grid-template-columns:minmax(16rem,.42fr) minmax(0,1fr);align-items:start;gap:1rem}.source-review__saved-list button{display:grid;width:100%;gap:.25rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);background:var(--studio-card);padding:.85rem;text-align:left;box-shadow:var(--studio-shadow-soft)}.source-review__saved-list button:hover,.source-review__saved-list button[aria-current='true']{border-color:var(--studio-line-strong);background:var(--panda-mist)}.source-review__saved-list span{color:var(--studio-muted);font-size:.8rem;line-height:1.45}.source-review__statuses{font-weight:650}.source-review__detail{display:grid;gap:1rem;min-width:0}.source-review__rights h3,.source-review__quality h3,.source-review__text h3{font-family:var(--panda-serif);font-size:1.2rem}.source-review__rights blockquote{margin-top:.8rem;border-left:3px solid var(--studio-line-strong);padding-left:.8rem;color:var(--studio-muted);font-style:italic;line-height:1.5}.source-review__review-fields{display:grid;gap:.8rem;margin:1rem 0}.source-review__review-fields textarea{display:block;width:100%;resize:vertical;margin-top:.4rem;border:1px solid var(--studio-line-strong);border-radius:var(--panda-radius-compact);background:var(--panda-white);padding:.65rem .75rem;color:var(--studio-ink);font:inherit;line-height:1.45}.source-review__provenance summary{cursor:pointer;font-weight:720}.source-review__provenance dl{display:grid;gap:.8rem;margin-top:1rem}.source-review__text pre{max-height:32rem;overflow:auto;margin-top:1rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);background:var(--panda-mist);padding:1rem;color:var(--studio-ink);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.82rem;line-height:1.55;white-space:pre-wrap;overflow-wrap:anywhere}@media(max-width:760px){.source-review__search,.source-review__saved-grid{grid-template-columns:1fr}.source-review__search .studio-button{width:100%}.source-result,.source-review__work-heading,.source-review__detail-heading{align-items:stretch;flex-direction:column}.source-result .studio-button,.source-review__work-heading .studio-button,.source-review__detail-heading .studio-button{width:100%}.source-review__work-metadata{grid-template-columns:1fr}.source-review__text pre{max-height:22rem}}
</style>
