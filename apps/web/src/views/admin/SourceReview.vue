<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import SourceEvidenceReferenceFields, {
  type SourceEvidenceReferenceInput,
} from "@/components/admin/source-review/SourceEvidenceReferenceFields.vue";
import StoryStudioState from "@/components/admin/story-studio/StoryStudioState.vue";
import {
  adminCheckSourceEligibility,
  adminListStories,
  adminPromoteSourceAcquisition,
  adminGetSourceAcquisition,
  adminGetSourceProviderWork,
  adminListSourceAcquisitions,
  adminPersistSourceAcquisition,
  adminSearchSourceProvider,
  adminUpdateSourceAcquisitionSourceQualityReview,
  getAPIErrorStatus,
  parseAdminEligibility,
  type APIError,
  type AdminCopyrightEvidenceReference,
  type AdminCopyrightFactState,
  type AdminSourceAcquisitionDetail,
  type AdminSourceAcquisitionSummary,
  type AdminSourceEligibility,
  type AdminSourceEligibilityHumanEvidence,
  type AdminSourceProviderWork,
  type AdminSourceQualityStatus,
  type AdminSourceAcquisitionPromotionTarget,
  type AdminStoryListItem,
} from "@/lib/api";

type WorkspacePanel = "find" | "saved";

const provider = "project-gutenberg" as const;
const router = useRouter();
const panel = ref<WorkspacePanel>("find");
const query = ref("");
const searching = ref(false);
const searchStarted = ref(false);
const works = ref<AdminSourceProviderWork[]>([]);
const selectedWork = ref<AdminSourceProviderWork | null>(null);
const selecting = ref(false);
const saving = ref(false);
const eligibilityLoading = ref(false);

// Provider evidence is tied to the selected exact work. It remains visible when
// the factual form changes; only the assessment becomes stale.
const providerEvidence = ref<AdminSourceEligibility | null>(null);
const assessment = ref<AdminSourceEligibility | null>(null);
const assessmentStale = ref(false);

const savedLoading = ref(false);
const saved = ref<AdminSourceAcquisitionSummary[]>([]);
const selectedAcquisition = ref<AdminSourceAcquisitionDetail | null>(null);
const detailLoading = ref(false);
const qualityStatus = ref<AdminSourceQualityStatus>("pending");
const qualityNote = ref("");
const qualitySaving = ref(false);
const promoting = ref(false);
const promotionMode = ref<"new_story" | "existing_story">("new_story");
const promotionTitle = ref("");
const promotionSlug = ref("");
const promotionStorySlug = ref("");
const publicStories = ref<AdminStoryListItem[]>([]);
const promotionFormOpen = ref(false);
const error = ref("");
const message = ref("");

const workCategory = ref<"ordinary_literary" | "unknown">("ordinary_literary");
const authorDeathYear = ref<number | undefined>();
const firstPublicationYear = ref<number | undefined>();
const translation = ref<AdminCopyrightFactState>("unknown");
const additionalTextual = ref<AdminCopyrightFactState>("unknown");
const specialCategory = ref<AdminCopyrightFactState>("unknown");
const unpublishedAtEnd1988 = ref<AdminCopyrightFactState>("unknown");
const workCategoryEvidence = ref(newEvidenceInput());
const authorDeathEvidence = ref(newEvidenceInput());
const firstPublicationEvidence = ref(newEvidenceInput());
const translationEvidence = ref(newEvidenceInput());
const additionalTextualEvidence = ref(newEvidenceInput());
const specialCategoryEvidence = ref(newEvidenceInput());
const unpublishedEvidence = ref(newEvidenceInput());

let searchController: AbortController | null = null;
let workController: AbortController | null = null;
let savedController: AbortController | null = null;
let detailController: AbortController | null = null;
let eligibilityController: AbortController | null = null;

const readyForPromotion = computed(
  () =>
    selectedAcquisition.value?.promotion === null &&
    selectedAcquisition.value?.eligibility?.overall === "eligible" &&
    selectedAcquisition.value.sourceQuality.status === "approved",
);
const sourceQualityLocked = computed(() => selectedAcquisition.value?.promotion !== null);
const providerAuthorDeathYear = computed(() => {
  const authors =
    providerEvidence.value?.contributors.filter(
      (contributor) =>
        contributor.role === "author" && contributor.deathYear !== undefined,
    ) ?? [];
  return authors.length === 1 ? authors[0].deathYear : undefined;
});
const providerAuthorDeathKnown = computed(
  () => providerAuthorDeathYear.value !== undefined,
);
const providerTranslationPresent = computed(
  () =>
    providerEvidence.value?.contributors.some(
      (contributor) => contributor.role === "translator",
    ) ?? false,
);
const providerTextualContributionPresent = computed(
  () =>
    providerEvidence.value?.contributors.some((contributor) =>
      [
        "adapter",
        "annotator",
        "compiler",
        "introduction_author",
        "editor",
        "contributor",
      ].includes(contributor.role),
    ) ?? false,
);

function newEvidenceInput(): SourceEvidenceReferenceInput {
  return { source: "", fact: "", locator: "" };
}

function evidenceReferences(
  input: SourceEvidenceReferenceInput,
): AdminCopyrightEvidenceReference[] {
  const source = input.source.trim();
  const fact = input.fact.trim();
  const locator = input.locator.trim();
  return source && fact
    ? [{ source, fact, ...(locator ? { locator } : {}) }]
    : [];
}

function humanEvidence(): AdminSourceEligibilityHumanEvidence {
  return {
    workCategory: workCategory.value,
    workCategoryReferences: evidenceReferences(workCategoryEvidence.value),
    ...(providerAuthorDeathKnown.value || authorDeathYear.value === undefined
      ? {}
      : {
          authorDeathYear: authorDeathYear.value,
          authorDeathReferences: evidenceReferences(authorDeathEvidence.value),
        }),
    ...(firstPublicationYear.value === undefined
      ? {}
      : {
          firstPublicationYear: firstPublicationYear.value,
          firstPublicationReferences: evidenceReferences(
            firstPublicationEvidence.value,
          ),
        }),
    translation: {
      state: translation.value,
      references: evidenceReferences(translationEvidence.value),
    },
    additionalTextualContribution: {
      state: additionalTextual.value,
      references: evidenceReferences(additionalTextualEvidence.value),
    },
    specialCategory: {
      state: specialCategory.value,
      references: evidenceReferences(specialCategoryEvidence.value),
    },
    unpublishedAtEnd1988: {
      state: unpublishedAtEnd1988.value,
      references: evidenceReferences(unpublishedEvidence.value),
    },
  };
}

function contributorText(work: {
  contributors: readonly { name: string; role: string }[];
}) {
  return (
    work.contributors
      .map(({ name, role }) => (role ? `${name} (${role})` : name))
      .join(", ") || "No contributor metadata"
  );
}

function providerContributorText(contributor: {
  name: string;
  role: string;
  birthYear?: number;
  deathYear?: number;
}) {
  const years =
    contributor.birthYear || contributor.deathYear
      ? ` (${contributor.birthYear ?? "?"}–${contributor.deathYear ?? "?"})`
      : "";
  return `${contributor.name} — ${contributor.role}${years}`;
}

function statusLabel(status: string) {
  return status[0]?.toUpperCase() + status.slice(1);
}

function formatSavedAt(value: string) {
  return `${new Intl.DateTimeFormat("en-GB", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(new Date(value))} UTC`;
}

function clearFeedback() {
  error.value = "";
  message.value = "";
}

function clearSelectedWork() {
  workController?.abort();
  eligibilityController?.abort();
  workController = null;
  eligibilityController = null;
  selecting.value = false;
  eligibilityLoading.value = false;
  selectedWork.value = null;
  providerEvidence.value = null;
  assessment.value = null;
  assessmentStale.value = false;
}

function invalidateAssessment() {
  eligibilityController?.abort();
  eligibilityController = null;
  eligibilityLoading.value = false;
  assessment.value = null;
  assessmentStale.value = providerEvidence.value !== null;
}

async function moveToSignIn() {
  await router.replace({
    path: "/account/login",
    query: { next: "/admin/source-review" },
  });
}

async function showError(caught: unknown) {
  if (getAPIErrorStatus(caught) === 401) {
    await moveToSignIn();
    return;
  }
  const code =
    typeof caught === "object" && caught !== null && "code" in caught
      ? (caught as { code?: unknown }).code
      : undefined;
  const messages: Record<string, string> = {
    source_eligibility_invalid:
      "Check the factual evidence fields and try again.",
    source_eligibility_blocked:
      "This source does not meet the current Panda Pages eligibility policy.",
    source_eligibility_evidence_failed:
      "Copyright evidence could not be verified safely.",
    source_eligibility_evidence_timeout:
      "Copyright evidence took too long to load.",
    source_provider_work_not_found:
      "That Project Gutenberg work is no longer available.",
    source_provider_unavailable: "Project Gutenberg is unavailable right now.",
    source_acquisition_not_found: "That saved source could not be found.",
    source_acquisition_not_ready: "This saved source is not ready for canonical-source promotion.",
    source_acquisition_already_promoted: "This source was already promoted to another story.",
    source_acquisition_promotion_target_not_found: "Choose an existing public Story Studio story.",
    source_acquisition_promotion_conflict: "That new story slug is already in use.",
  };
  error.value =
    typeof code === "string" && messages[code]
      ? messages[code]
      : "Story Studio could not complete that source review action. Try again.";
}

async function search() {
  const term = query.value.trim();
  searchStarted.value = true;
  clearFeedback();
  clearSelectedWork();
  if (term.length < 2) {
    works.value = [];
    error.value = "Enter at least two characters to search Project Gutenberg.";
    return;
  }

  const controller = new AbortController();
  searchController?.abort();
  searchController = controller;
  searching.value = true;
  try {
    const response = await adminSearchSourceProvider(
      provider,
      term,
      controller.signal,
    );
    if (!controller.signal.aborted && searchController === controller)
      works.value = response.results;
  } catch (caught) {
    if (!controller.signal.aborted && searchController === controller)
      await showError(caught);
  } finally {
    if (searchController === controller) {
      searching.value = false;
      searchController = null;
    }
  }
}

async function selectWork(work: AdminSourceProviderWork) {
  const controller = new AbortController();
  workController?.abort();
  eligibilityController?.abort();
  workController = controller;
  eligibilityController = null;
  clearFeedback();
  selecting.value = true;
  eligibilityLoading.value = false;
  selectedWork.value = null;
  providerEvidence.value = null;
  assessment.value = null;
  assessmentStale.value = false;

  try {
    const detail = await adminGetSourceProviderWork(
      provider,
      work.externalId,
      controller.signal,
    );
    if (controller.signal.aborted || workController !== controller) return;
    selectedWork.value = detail;
    await loadEligibility(detail);
  } catch (caught) {
    if (!controller.signal.aborted && workController === controller)
      await showError(caught);
  } finally {
    if (workController === controller) {
      selecting.value = false;
      workController = null;
    }
  }
}

async function loadEligibility(work: AdminSourceProviderWork) {
  const controller = new AbortController();
  eligibilityController?.abort();
  eligibilityController = controller;
  eligibilityLoading.value = true;
  const evidence = humanEvidence();
  try {
    const result = await adminCheckSourceEligibility(
      provider,
      work.externalId,
      evidence,
      controller.signal,
    );
    if (
      !controller.signal.aborted &&
      eligibilityController === controller &&
      selectedWork.value?.externalId === work.externalId
    ) {
      providerEvidence.value = result;
      assessment.value = result;
      assessmentStale.value = false;
    }
  } catch (caught) {
    if (!controller.signal.aborted && eligibilityController === controller)
      await showError(caught);
  } finally {
    if (eligibilityController === controller) {
      eligibilityLoading.value = false;
      eligibilityController = null;
    }
  }
}

function applyQualityDraft(detail: AdminSourceAcquisitionDetail) {
  qualityStatus.value = detail.sourceQuality.status;
  qualityNote.value = detail.sourceQuality.note ?? "";
}

async function loadSaved() {
  const controller = new AbortController();
  savedController?.abort();
  savedController = controller;
  savedLoading.value = true;
  try {
    const response = await adminListSourceAcquisitions(controller.signal);
    if (!controller.signal.aborted && savedController === controller)
      saved.value = response.items;
  } catch (caught) {
    if (!controller.signal.aborted && savedController === controller)
      await showError(caught);
  } finally {
    if (savedController === controller) {
      savedLoading.value = false;
      savedController = null;
    }
  }
}

async function openSaved(id: string, preserveFeedback = false) {
  if (!preserveFeedback) clearFeedback();
  const controller = new AbortController();
  detailController?.abort();
  detailController = controller;
  detailLoading.value = true;
  try {
    const detail = await adminGetSourceAcquisition(id, controller.signal);
    if (controller.signal.aborted || detailController !== controller) return;
    selectedAcquisition.value = detail;
    applyQualityDraft(detail);
    panel.value = "saved";
  } catch (caught) {
    if (!controller.signal.aborted && detailController === controller)
      await showError(caught);
  } finally {
    if (detailController === controller) {
      detailLoading.value = false;
      detailController = null;
    }
  }
}

async function saveForReview() {
  const work = selectedWork.value;
  if (!work || saving.value) return;
  clearFeedback();
  saving.value = true;
  try {
    const result = await adminPersistSourceAcquisition(
      provider,
      work.externalId,
      humanEvidence(),
    );
    message.value =
      result.outcome === "created"
        ? "Saved for source review."
        : "This exact saved source already exists. Opening it for review.";
    await loadSaved();
    await openSaved(result.acquisition.id, true);
  } catch (caught) {
    const body = (caught as APIError).body;
    try {
      const record = body as { eligibility?: unknown };
      if (record?.eligibility !== undefined) {
        const result = parseAdminEligibility(record.eligibility);
        providerEvidence.value = result;
        assessment.value = result;
        assessmentStale.value = false;
      }
    } catch {
      // Retain the safe API error if its optional assessment is malformed.
    }
    await showError(caught);
  } finally {
    saving.value = false;
  }
}

async function saveQuality() {
  const detail = selectedAcquisition.value;
  if (!detail || qualitySaving.value) return;
  const note = qualityNote.value.trim();
  clearFeedback();
  if (qualityStatus.value !== "pending" && !note) {
    error.value = "Add a rationale before approving or rejecting a source.";
    return;
  }
  qualitySaving.value = true;
  try {
    const summary = await adminUpdateSourceAcquisitionSourceQualityReview(
      detail.id,
      { status: qualityStatus.value, note },
    );
    selectedAcquisition.value = { ...summary, sourceText: detail.sourceText };
    applyQualityDraft(selectedAcquisition.value);
    saved.value = saved.value.map((item) =>
      item.id === summary.id ? summary : item,
    );
    message.value = "Source quality review updated.";
  } catch (caught) {
    await showError(caught);
  } finally {
    qualitySaving.value = false;
  }
}

function suggestedPromotionSlug(title: string) {
  return title
    .toLowerCase()
    .normalize("NFKD")
    .replaceAll(/[^a-z0-9]+/g, "-")
    .replaceAll(/^-+|-+$/g, "")
}

async function openPromotionForm() {
  const detail = selectedAcquisition.value;
  if (!detail) return;
  promotionTitle.value = detail.title;
  promotionSlug.value = suggestedPromotionSlug(detail.title);
  promotionMode.value = "new_story";
  promotionStorySlug.value = "";
  promotionFormOpen.value = true;
  try {
    publicStories.value = (await adminListStories()).items;
  } catch (caught) {
    await showError(caught);
  }
}

async function promoteSource() {
  const detail = selectedAcquisition.value;
  if (!detail || promoting.value) return;
  const target: AdminSourceAcquisitionPromotionTarget =
    promotionMode.value === "new_story"
      ? { mode: "new_story", title: promotionTitle.value.trim(), slug: promotionSlug.value.trim() }
      : { mode: "existing_story", storySlug: promotionStorySlug.value };
  clearFeedback();
  promoting.value = true;
  try {
    const result = await adminPromoteSourceAcquisition(detail.id, target);
    message.value = result.outcome === "created"
      ? "Source promoted to canonical source."
      : "This source was already promoted. Opening its canonical destination.";
    promotionFormOpen.value = false;
    await loadSaved();
    await openSaved(detail.id, true);
  } catch (caught) {
    await showError(caught);
  } finally {
    promoting.value = false;
  }
}

function tabID(value: WorkspacePanel) {
  return `source-review-tab-${value}`;
}

function panelID(value: WorkspacePanel) {
  return `source-review-panel-${value}`;
}

async function changePanel(next: WorkspacePanel, focusTab = false) {
  panel.value = next;
  clearFeedback();
  if (next === "saved") void loadSaved();
  if (focusTab) {
    await nextTick();
    document.getElementById(tabID(next))?.focus();
  }
}

function onTabKeydown(event: KeyboardEvent, current: WorkspacePanel) {
  const order: WorkspacePanel[] = ["find", "saved"];
  const index = order.indexOf(current);
  const next =
    event.key === "ArrowRight"
      ? order[(index + 1) % order.length]
      : event.key === "ArrowLeft"
        ? order[(index - 1 + order.length) % order.length]
        : event.key === "Home"
          ? order[0]
          : event.key === "End"
            ? order[order.length - 1]
            : null;
  if (!next) return;
  event.preventDefault();
  void changePanel(next, true);
}

onMounted(loadSaved);
onBeforeUnmount(() => {
  searchController?.abort();
  workController?.abort();
  savedController?.abort();
  detailController?.abort();
  eligibilityController?.abort();
});
</script>

<template>
  <div class="source-review">
    <header class="studio-page-heading">
      <p class="studio-page-heading__eyebrow">Story Studio</p>
      <h1>Source review</h1>
      <p class="studio-page-heading__summary">
        Find Project Gutenberg source material and review durable evidence
        before future canonical-source promotion.
      </p>
    </header>

    <div
      class="source-review__tabs"
      role="tablist"
      aria-label="Source review workspace"
    >
      <button
        :id="tabID('find')"
        type="button"
        role="tab"
        :aria-controls="panelID('find')"
        :aria-selected="panel === 'find'"
        :tabindex="panel === 'find' ? 0 : -1"
        @click="changePanel('find')"
        @keydown="onTabKeydown($event, 'find')"
      >
        Find a source
      </button>
      <button
        :id="tabID('saved')"
        type="button"
        role="tab"
        :aria-controls="panelID('saved')"
        :aria-selected="panel === 'saved'"
        :tabindex="panel === 'saved' ? 0 : -1"
        @click="changePanel('saved')"
        @keydown="onTabKeydown($event, 'saved')"
      >
        Saved sources
      </button>
    </div>

    <p v-if="message" class="source-review__message" role="status">
      {{ message }}
    </p>
    <p v-if="error" class="source-review__error" role="alert">{{ error }}</p>

    <section
      v-if="panel === 'find'"
      :id="panelID('find')"
      role="tabpanel"
      :aria-labelledby="tabID('find')"
    >
      <div class="studio-panel">
        <h2>Search Project Gutenberg</h2>
        <form class="source-review__search" @submit.prevent="search">
          <div class="studio-field">
            <label for="source-provider-search">Search Project Gutenberg</label>
            <input
              id="source-provider-search"
              v-model="query"
              type="search"
              placeholder="Title or author"
            />
          </div>
          <button
            type="submit"
            class="studio-button studio-button--primary"
            :disabled="searching"
          >
            {{ searching ? "Searching…" : "Search" }}
          </button>
        </form>
      </div>

      <StoryStudioState
        v-if="searching"
        kind="loading"
        title="Searching Project Gutenberg"
        message="Finding provider works."
      />
      <StoryStudioState
        v-else-if="searchStarted && works.length === 0 && !error"
        kind="empty"
        title="No matching works"
        message="Try another title or author search."
      />

      <ol v-else-if="works.length" class="source-review__results">
        <li
          v-for="work in works"
          :key="work.externalId"
          class="studio-panel source-result"
        >
          <div>
            <h3>{{ work.title }}</h3>
            <p>{{ contributorText(work) }}</p>
            <p>Project Gutenberg #{{ work.externalId }}</p>
          </div>
          <button
            type="button"
            class="studio-button studio-button--quiet"
            @click="selectWork(work)"
          >
            Select work
          </button>
        </li>
      </ol>

      <StoryStudioState
        v-if="selecting"
        kind="loading"
        title="Loading selected work"
        message="Checking the exact provider work."
      />

      <section v-if="selectedWork" class="studio-panel source-review__work">
        <header class="source-review__work-heading">
          <div>
            <h2>{{ selectedWork.title }}</h2>
            <p>
              {{ contributorText(selectedWork) }} · Project Gutenberg #{{
                selectedWork.externalId
              }}
            </p>
          </div>
          <a :href="selectedWork.landingUrl" target="_blank" rel="noreferrer"
            >Open provider page</a
          >
        </header>

        <section class="source-review__eligibility">
          <h3>Copyright eligibility</h3>
          <StoryStudioState
            v-if="eligibilityLoading"
            kind="loading"
            title="Checking provider evidence"
            message="Loading current Project Gutenberg evidence."
          />
          <template v-else>
            <div
              v-if="providerEvidence"
              class="source-review__provider-evidence"
            >
              <p>
                Provider rights evidence: OPDS
                {{ providerEvidence.opdsRights }} · RDF
                {{ providerEvidence.rdfRights }}.
              </p>
              <ul v-if="providerEvidence.contributors.length">
                <li
                  v-for="contributor in providerEvidence.contributors"
                  :key="`${contributor.name}-${contributor.role}`"
                >
                  {{ providerContributorText(contributor) }}
                </li>
              </ul>
            </div>
            <p v-else>
              Provider evidence could not be loaded. Saving remains blocked
              until it can be verified.
            </p>
            <div
              v-if="assessment && !assessmentStale"
              class="source-review__assessment"
            >
              <p>
                United States:
                <strong>{{ statusLabel(assessment.us.status) }}</strong> ·
                United Kingdom:
                <strong>{{ statusLabel(assessment.uk.status) }}</strong
                ><br />
                {{ assessment.us.reason }} · {{ assessment.uk.reason }}
              </p>
            </div>
            <p
              v-else-if="providerEvidence"
              class="source-review__stale"
              role="status"
            >
              Eligibility conclusion needs revalidation after factual evidence
              changed.
            </p>
          </template>
        </section>

        <section class="source-review__uk-evidence">
          <h3>UK factual evidence</h3>
          <p>
            Provide facts and their evidence source, not a legal conclusion.
            Citation locators are evidence metadata only and are never fetched.
          </p>
          <div class="source-review__review-fields">
            <div class="studio-field">
              <label for="work-category">Work type</label>
              <select
                id="work-category"
                v-model="workCategory"
                @change="invalidateAssessment"
              >
                <option value="ordinary_literary">
                  Ordinary literary work
                </option>
                <option value="unknown">Unknown</option>
              </select>
            </div>
            <SourceEvidenceReferenceFields
              v-model="workCategoryEvidence"
              label="Work type"
              id-prefix="work-category"
              @update:model-value="invalidateAssessment"
            />

            <div class="studio-field">
              <label for="first-publication">First publication year</label>
              <input
                id="first-publication"
                v-model.number="firstPublicationYear"
                type="number"
                min="1"
                @input="invalidateAssessment"
              />
            </div>
            <SourceEvidenceReferenceFields
              v-model="firstPublicationEvidence"
              label="First publication"
              id-prefix="first-publication"
              @update:model-value="invalidateAssessment"
            />

            <template v-if="!providerAuthorDeathKnown">
              <div class="studio-field">
                <label for="author-death">Author death year</label>
                <input
                  id="author-death"
                  v-model.number="authorDeathYear"
                  type="number"
                  min="1"
                  @input="invalidateAssessment"
                />
              </div>
              <SourceEvidenceReferenceFields
                v-model="authorDeathEvidence"
                label="Author death"
                id-prefix="author-death"
                @update:model-value="invalidateAssessment"
              />
            </template>
            <p v-else>
              Provider author death year: {{ providerAuthorDeathYear }}
            </p>

            <template v-if="providerTranslationPresent">
              <p>
                Project Gutenberg reports a translator. This narrow policy
                cannot pass this source.
              </p>
            </template>
            <template v-else>
              <div class="studio-field">
                <label for="translation">Translation</label>
                <select
                  id="translation"
                  v-model="translation"
                  @change="invalidateAssessment"
                >
                  <option value="none_confirmed">None confirmed</option>
                  <option value="present">Present</option>
                  <option value="unknown">Unknown</option>
                </select>
              </div>
              <SourceEvidenceReferenceFields
                v-model="translationEvidence"
                label="Translation"
                id-prefix="translation"
                @update:model-value="invalidateAssessment"
              />
            </template>

            <template v-if="providerTextualContributionPresent">
              <p>
                Project Gutenberg reports a possible additional textual
                contributor. This narrow policy cannot pass this source.
              </p>
            </template>
            <template v-else>
              <div class="studio-field">
                <label for="textual">Additional textual contribution</label>
                <select
                  id="textual"
                  v-model="additionalTextual"
                  @change="invalidateAssessment"
                >
                  <option value="none_confirmed">None confirmed</option>
                  <option value="present">Present</option>
                  <option value="unknown">Unknown</option>
                </select>
              </div>
              <SourceEvidenceReferenceFields
                v-model="additionalTextualEvidence"
                label="Additional textual contribution"
                id-prefix="textual"
                @update:model-value="invalidateAssessment"
              />
            </template>

            <div class="studio-field">
              <label for="special">Special category</label>
              <select
                id="special"
                v-model="specialCategory"
                @change="invalidateAssessment"
              >
                <option value="none_confirmed">None confirmed</option>
                <option value="present">Present</option>
                <option value="unknown">Unknown</option>
              </select>
            </div>
            <SourceEvidenceReferenceFields
              v-model="specialCategoryEvidence"
              label="Special category"
              id-prefix="special"
              @update:model-value="invalidateAssessment"
            />

            <div class="studio-field">
              <label for="unpublished">Unpublished at end of 1988</label>
              <select
                id="unpublished"
                v-model="unpublishedAtEnd1988"
                @change="invalidateAssessment"
              >
                <option value="none_confirmed">None confirmed</option>
                <option value="present">Present</option>
                <option value="unknown">Unknown</option>
              </select>
            </div>
            <SourceEvidenceReferenceFields
              v-model="unpublishedEvidence"
              label="Unpublished-at-end-of-1988"
              id-prefix="unpublished"
              @update:model-value="invalidateAssessment"
            />
          </div>
          <button
            type="button"
            class="studio-button studio-button--primary"
            :disabled="saving || eligibilityLoading"
            @click="saveForReview"
          >
            {{
              saving
                ? "Validating and saving…"
                : "Validate & save for source review"
            }}
          </button>
        </section>
      </section>
    </section>

    <section
      v-if="panel === 'saved'"
      :id="panelID('saved')"
      role="tabpanel"
      :aria-labelledby="tabID('saved')"
    >
      <div class="source-review__saved-grid">
        <div>
          <StoryStudioState
            v-if="savedLoading && saved.length === 0"
            kind="loading"
            title="Loading saved sources"
            message="Opening durable source acquisitions."
          />
          <StoryStudioState
            v-else-if="saved.length === 0 && !error"
            kind="empty"
            title="No sources saved for review yet"
            message="Search Project Gutenberg to save an exact source acquisition."
          />
          <ol v-else class="source-review__saved-list">
            <li v-for="item in saved" :key="item.id">
              <button type="button" @click="openSaved(item.id)">
                <strong>{{ item.title }}</strong>
                <span
                  >Project Gutenberg #{{ item.externalId }} ·
                  {{ formatSavedAt(item.createdAt) }}</span
                >
                <span
                  >Eligibility:
                  {{
                    item.eligibility?.overall === "eligible"
                      ? "Eligible"
                      : "Recheck required"
                  }}
                  · Source quality:
                  {{ statusLabel(item.sourceQuality.status) }}</span
                >
              </button>
            </li>
          </ol>
        </div>

        <StoryStudioState
          v-if="detailLoading"
          kind="loading"
          title="Opening saved source"
          message="Loading durable source evidence."
        />
        <article v-else-if="selectedAcquisition" class="source-review__detail">
          <header class="studio-panel source-review__detail-heading">
            <h2>{{ selectedAcquisition.title }}</h2>
            <a
              :href="selectedAcquisition.landingUrl"
              target="_blank"
              rel="noreferrer"
              >Provider page</a
            >
          </header>
          <section v-if="selectedAcquisition.promotion" class="source-review__ready">
            <h3>Promoted to canonical source</h3>
            <p>
              {{ selectedAcquisition.promotion.storyTitle }} is now the canonical-source destination.
              Source quality was locked when this acquisition was promoted.
            </p>
          </section>
          <section v-else-if="readyForPromotion" class="source-review__ready">
            <h3>Ready for canonical-source promotion</h3>
            <p>Promotion creates a canonical source revision. It does not create editions or publish the story.</p>
            <button type="button" class="studio-button studio-button--primary" @click="openPromotionForm">
              Promote to canonical source
            </button>
            <form v-if="promotionFormOpen" class="source-review__promotion" @submit.prevent="promoteSource">
              <fieldset>
                <legend>Promotion target</legend>
                <label>
                  <input v-model="promotionMode" type="radio" value="new_story" />
                  Create new story
                </label>
                <label>
                  <input v-model="promotionMode" type="radio" value="existing_story" />
                  Existing public story
                </label>
              </fieldset>
              <template v-if="promotionMode === 'new_story'">
                <div class="studio-field">
                  <label for="promotion-title">Story title</label>
                  <input id="promotion-title" v-model="promotionTitle" />
                </div>
                <div class="studio-field">
                  <label for="promotion-slug">Story slug</label>
                  <input id="promotion-slug" v-model="promotionSlug" />
                </div>
              </template>
              <div v-else class="studio-field">
                <label for="promotion-story">Existing public story</label>
                <select id="promotion-story" v-model="promotionStorySlug">
                  <option value="">Select a story</option>
                  <option v-for="story in publicStories" :key="story.slug" :value="story.slug">
                    {{ story.title }} ({{ story.slug }})
                  </option>
                </select>
              </div>
              <button type="submit" class="studio-button studio-button--primary" :disabled="promoting">
                {{ promoting ? "Promoting…" : promotionMode === "new_story" ? "Create story & promote source" : "Promote source to existing story" }}
              </button>
            </form>
          </section>
          <section class="studio-panel source-review__rights">
            <h3>Copyright eligibility</h3>
            <div v-if="selectedAcquisition.eligibility">
              <p>
                United States:
                {{ statusLabel(selectedAcquisition.eligibility.us.status) }} ·
                United Kingdom:
                {{ statusLabel(selectedAcquisition.eligibility.uk.status)
                }}<br />
                Policy: {{ selectedAcquisition.eligibility.policyVersion }} ·
                {{ formatSavedAt(selectedAcquisition.eligibility.evaluatedAt) }}
              </p>
              <details>
                <summary>Eligibility evidence</summary>
                <p>
                  OPDS: {{ selectedAcquisition.eligibility.opdsRights }} · RDF:
                  {{ selectedAcquisition.eligibility.rdfRights }} · source
                  header: {{ selectedAcquisition.eligibility.headerRights }}
                </p>
                <ul>
                  <li
                    v-for="contributor in selectedAcquisition.eligibility
                      .contributors"
                    :key="`${contributor.name}-${contributor.role}`"
                  >
                    {{ providerContributorText(contributor) }}
                  </li>
                </ul>
              </details>
            </div>
            <p v-else>Eligibility: Recheck required.</p>
          </section>
          <section class="studio-panel source-review__quality">
            <h3>Source quality review</h3>
            <template v-if="sourceQualityLocked">
              <p>Source quality was locked when this acquisition was promoted.</p>
              <dl class="source-review__quality-history">
                <div>
                  <dt>Status</dt>
                  <dd>{{ selectedAcquisition.sourceQuality.status }}</dd>
                </div>
                <div v-if="selectedAcquisition.sourceQuality.note">
                  <dt>Rationale</dt>
                  <dd>{{ selectedAcquisition.sourceQuality.note }}</dd>
                </div>
                <div v-if="selectedAcquisition.sourceQuality.reviewedAt">
                  <dt>Reviewed</dt>
                  <dd>{{ formatSavedAt(selectedAcquisition.sourceQuality.reviewedAt) }}</dd>
                </div>
              </dl>
            </template>
            <template v-else>
              <div class="studio-field">
                <label for="quality-status">Source quality status</label>
                <select id="quality-status" v-model="qualityStatus">
                  <option value="pending">Pending</option>
                  <option value="approved">Approved</option>
                  <option value="rejected">Rejected</option>
                </select>
              </div>
              <div v-if="qualityStatus !== 'pending'" class="studio-field">
                <label for="quality-note">Rationale</label>
                <textarea id="quality-note" v-model="qualityNote" rows="3" />
              </div>
              <button
                type="button"
                class="studio-button studio-button--quiet"
                :disabled="qualitySaving"
                @click="saveQuality"
              >
                Save source quality review
              </button>
            </template>
          </section>
          <details class="studio-panel source-review__provenance">
            <summary>Source provenance</summary>
            <p v-if="selectedAcquisition.eligibility">
              RDF evidence digest:
              <code>{{ selectedAcquisition.eligibility.rdfDigest }}</code>
            </p>
            <p>
              Snapshot hash: <code>{{ selectedAcquisition.snapshotHash }}</code>
            </p>
          </details>
          <section class="studio-panel source-review__text">
            <h3>Saved source text</h3>
            <pre tabindex="0">{{ selectedAcquisition.sourceText }}</pre>
          </section>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
.source-review__tabs {
  display: flex;
  gap: 0.45rem;
  margin-bottom: 1.25rem;
  border-bottom: 1px solid var(--studio-line-strong);
}
.source-review__tabs button {
  min-height: 2.75rem;
  border-bottom: 3px solid transparent;
  padding: 0.65rem 0.9rem;
  color: var(--studio-muted);
  font-weight: 720;
}
.source-review__tabs button[aria-selected="true"] {
  border-color: var(--panda-ink);
  color: var(--studio-ink);
}
.source-review__message,
.source-review__error,
.source-review__ready,
.source-review__stale {
  margin-bottom: 1rem;
  border-radius: var(--panda-radius-compact);
  padding: 0.85rem 1rem;
}
.source-review__message,
.source-review__ready {
  border: 1px solid var(--panda-success);
  background: var(--panda-success-surface);
  color: var(--panda-success);
}
.source-review__stale {
  border: 1px solid var(--studio-line-strong);
  background: var(--panda-mist);
  color: var(--studio-ink);
}
.source-review__error {
  border: 1px solid var(--panda-danger);
  background: var(--panda-danger-surface);
  color: var(--panda-danger);
}
.source-review__work,
.source-review__detail {
  margin-top: 1.4rem;
}
.source-review__work > section + section,
.source-review__detail > section + section,
.source-review__detail > details + section {
  margin-top: 1rem;
}
.source-review__work-heading,
.source-review__detail-heading,
.source-result {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.source-review__work h2,
.source-review__detail h2,
.source-result h3,
.source-review__eligibility h3,
.source-review__uk-evidence h3,
.source-review__rights h3,
.source-review__quality h3,
.source-review__text h3 {
  font-family: var(--panda-serif);
  font-size: 1.2rem;
  font-weight: 650;
}
.source-review__work p,
.source-result p,
.source-review__eligibility > p,
.source-review__uk-evidence > p,
.source-review__provider-evidence,
.source-review__assessment,
.source-review__rights > p {
  margin-top: 0.4rem;
  color: var(--studio-muted);
  line-height: 1.55;
}
.source-review__search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 0.8rem;
  margin-top: 1rem;
}
.source-review__results,
.source-review__saved-list {
  display: grid;
  gap: 0.75rem;
  margin-top: 1.4rem;
}
.source-review__provider-evidence ul,
.source-review__rights ul {
  display: grid;
  gap: 0.25rem;
  margin-top: 0.65rem;
}
.source-review__review-fields {
  display: grid;
  gap: 0.9rem;
  margin: 1rem 0;
}
.source-review__promotion {
  display: grid;
  gap: 0.75rem;
  margin-top: 1rem;
}
.source-review__promotion fieldset {
  display: grid;
  gap: 0.5rem;
}
.source-review__saved-grid {
  display: grid;
  grid-template-columns: minmax(16rem, 0.42fr) minmax(0, 1fr);
  align-items: start;
  gap: 1rem;
}
.source-review__saved-list button {
  display: grid;
  width: 100%;
  gap: 0.25rem;
  border: 1px solid var(--studio-line);
  border-radius: var(--panda-radius-compact);
  background: var(--studio-card);
  padding: 0.85rem;
  text-align: left;
  box-shadow: var(--studio-shadow-soft);
}
.source-review__saved-list button:hover,
.source-review__saved-list button[aria-current="true"] {
  border-color: var(--studio-line-strong);
  background: var(--panda-mist);
}
.source-review__saved-list span {
  color: var(--studio-muted);
  font-size: 0.8rem;
  line-height: 1.45;
}
.source-review__detail {
  display: grid;
  min-width: 0;
  gap: 1rem;
}
.source-review__provenance summary {
  cursor: pointer;
  font-weight: 720;
}
.source-review__text pre {
  max-height: 32rem;
  overflow: auto;
  margin-top: 1rem;
  border: 1px solid var(--studio-line);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-mist);
  padding: 1rem;
  color: var(--studio-ink);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.82rem;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
@media (max-width: 760px) {
  .source-review__search,
  .source-review__saved-grid {
    grid-template-columns: 1fr;
  }
  .source-review__search .studio-button,
  .source-result .studio-button {
    width: 100%;
  }
  .source-result,
  .source-review__work-heading,
  .source-review__detail-heading {
    align-items: stretch;
    flex-direction: column;
  }
  .source-review__text pre {
    max-height: 22rem;
  }
}
</style>
