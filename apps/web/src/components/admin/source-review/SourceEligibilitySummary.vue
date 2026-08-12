<script setup lang="ts">
import { computed } from "vue";
import type {
  AdminCopyrightEvidenceReference,
  AdminCopyrightFactEvidence,
  AdminSourceEligibility,
} from "@/lib/api";

const props = defineProps<{
  eligibility: AdminSourceEligibility;
}>();

type EvidenceGroup = {
  label: string;
  references: AdminCopyrightEvidenceReference[];
};

const evidence = computed(() => props.eligibility.effectiveUkEvidence);

const evidenceGroups = computed<EvidenceGroup[]>(() => [
  { label: "Work type", references: evidence.value.workCategoryReferences },
  { label: "Authorship", references: evidence.value.authorshipReferences },
  { label: "Author", references: evidence.value.authorReferences },
  { label: "First publication", references: evidence.value.firstPublicationReferences },
  { label: "Translation screening", references: evidence.value.translation.references },
  {
    label: "Additional textual contribution screening",
    references: evidence.value.additionalTextualContribution.references,
  },
  {
    label: "Publication history",
    references: evidence.value.unpublishedAtEnd1988.references,
  },
]);

function titleCase(value: string) {
  return value
    .split("_")
    .filter(Boolean)
    .map((part) => part[0]?.toUpperCase() + part.slice(1))
    .join(" ");
}

function jurisdictionLabel(status: string) {
  return titleCase(status);
}

function workCategoryLabel(value: string) {
  if (value === "ordinary_literary") return "Ordinary literary work";
  return value === "unknown" ? "Not established" : titleCase(value);
}

function authorshipLabel(value: string) {
  switch (value) {
    case "single_known":
      return "Single known author";
    case "joint":
      return "Joint authorship";
    case "anonymous":
      return "Anonymous";
    case "pseudonymous":
      return "Pseudonymous";
    default:
      return "Not established";
  }
}

function factLabel(value: AdminCopyrightFactEvidence) {
  switch (value.state) {
    case "none_confirmed":
      return "None confirmed";
    case "present":
      return "Present";
    default:
      return "Not established";
  }
}

function yearLabel(value: number) {
  return value > 0 ? String(value) : "Not established";
}

function authorLabel(name: string) {
  return name.trim() || "Not established";
}

function contributorLabel(contributor: {
  name: string;
  role: string;
  birthYear?: number;
  deathYear?: number;
}) {
  const years =
    contributor.birthYear !== undefined || contributor.deathYear !== undefined
      ? ` (${contributor.birthYear ?? "?"}–${contributor.deathYear ?? "?"})`
      : "";
  return `${contributor.name} — ${titleCase(contributor.role)}${years}`;
}

function rightsLabel(value: string) {
  switch (value) {
    case "public_domain":
      return "Public domain";
    case "restricted":
      return "Restricted";
    case "no_classification":
      return "No classification";
    case "conflicting":
      return "Conflicting";
    default:
      return "Unknown";
  }
}

function referenceMeta(reference: AdminCopyrightEvidenceReference) {
  return [reference.identifier, reference.locator].filter(Boolean).join(" · ");
}
</script>

<template>
  <div class="source-eligibility-summary">
    <div
      class="source-eligibility-summary__decision"
      :data-status="eligibility.overall"
    >
      <strong>
        {{
          eligibility.overall === "eligible"
            ? "Eligible under Panda Pages V3"
            : "Blocked under Panda Pages V3"
        }}
      </strong>
      <span>
        Evaluated {{ eligibility.evaluationDate }} ·
        {{ eligibility.policyVersion }}
      </span>
    </div>

    <dl class="source-eligibility-summary__jurisdictions">
      <div>
        <dt>United States</dt>
        <dd>{{ jurisdictionLabel(eligibility.us.status) }}</dd>
      </div>
      <div>
        <dt>United Kingdom</dt>
        <dd>{{ jurisdictionLabel(eligibility.uk.status) }}</dd>
      </div>
    </dl>

    <dl class="source-eligibility-summary__facts">
      <div>
        <dt>Work</dt>
        <dd>{{ workCategoryLabel(evidence.workCategory) }}</dd>
      </div>
      <div>
        <dt>Authorship</dt>
        <dd>{{ authorshipLabel(evidence.authorship) }}</dd>
      </div>
      <div>
        <dt>Author</dt>
        <dd>{{ authorLabel(evidence.authorName) }}</dd>
      </div>
      <div>
        <dt>Author death</dt>
        <dd>{{ yearLabel(evidence.authorDeathYear) }}</dd>
      </div>
      <div>
        <dt>First publication</dt>
        <dd>{{ yearLabel(evidence.firstPublicationYear) }}</dd>
      </div>
      <div>
        <dt>Translation risk</dt>
        <dd>{{ factLabel(evidence.translation) }}</dd>
      </div>
      <div>
        <dt>Additional textual contribution</dt>
        <dd>{{ factLabel(evidence.additionalTextualContribution) }}</dd>
      </div>
      <div>
        <dt>Unpublished at end of 1988</dt>
        <dd>{{ factLabel(evidence.unpublishedAtEnd1988) }}</dd>
      </div>
    </dl>

    <details class="source-eligibility-summary__details">
      <summary>View evidence</summary>

      <div class="source-eligibility-summary__provider">
        <h4>Provider evidence</h4>
        <dl>
          <div>
            <dt>OPDS rights</dt>
            <dd>{{ rightsLabel(eligibility.opdsRights) }}</dd>
          </div>
          <div>
            <dt>RDF rights</dt>
            <dd>{{ rightsLabel(eligibility.rdfRights) }}</dd>
          </div>
          <div>
            <dt>Source header</dt>
            <dd>{{ rightsLabel(eligibility.headerRights) }}</dd>
          </div>
        </dl>
        <ul v-if="eligibility.contributors.length">
          <li
            v-for="contributor in eligibility.contributors"
            :key="`${contributor.name}-${contributor.role}`"
          >
            {{ contributorLabel(contributor) }}
          </li>
        </ul>
      </div>

      <div
        v-for="group in evidenceGroups"
        :key="group.label"
        class="source-eligibility-summary__evidence-group"
      >
        <h4>{{ group.label }}</h4>
        <ul v-if="group.references.length">
          <li
            v-for="(reference, index) in group.references"
            :key="`${group.label}-${index}-${reference.source}`"
          >
            <strong>{{ reference.source }}</strong>
            <span>{{ reference.fact }}</span>
            <small v-if="referenceMeta(reference)">
              {{ referenceMeta(reference) }}
            </small>
          </li>
        </ul>
        <p v-else>No supporting evidence was established.</p>
      </div>
    </details>
  </div>
</template>

<style scoped>
.source-eligibility-summary {
  display: grid;
  gap: 1rem;
}

.source-eligibility-summary__decision {
  display: grid;
  gap: 0.2rem;
  border: 1px solid var(--studio-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--studio-wash);
  padding: 0.85rem 1rem;
}

.source-eligibility-summary__decision strong {
  font-size: 1rem;
}

.source-eligibility-summary__decision span {
  color: var(--studio-muted);
  font-size: 0.82rem;
}

.source-eligibility-summary__jurisdictions,
.source-eligibility-summary__facts,
.source-eligibility-summary__provider dl {
  display: grid;
  margin: 0;
  gap: 0.55rem;
}

.source-eligibility-summary__jurisdictions,
.source-eligibility-summary__facts {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.source-eligibility-summary__jurisdictions > div,
.source-eligibility-summary__facts > div,
.source-eligibility-summary__provider dl > div {
  min-width: 0;
  border-bottom: 1px solid var(--studio-line);
  padding-bottom: 0.5rem;
}

.source-eligibility-summary dt {
  color: var(--studio-muted);
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.03em;
  text-transform: uppercase;
}

.source-eligibility-summary dd {
  margin: 0.15rem 0 0;
  font-weight: 650;
}

.source-eligibility-summary__details {
  border-top: 1px solid var(--studio-line);
  padding-top: 0.75rem;
}

.source-eligibility-summary__details summary {
  cursor: pointer;
  font-weight: 700;
}

.source-eligibility-summary__provider,
.source-eligibility-summary__evidence-group {
  margin-top: 1rem;
}

.source-eligibility-summary__provider h4,
.source-eligibility-summary__evidence-group h4 {
  margin: 0 0 0.5rem;
  font-size: 0.9rem;
}

.source-eligibility-summary__provider ul,
.source-eligibility-summary__evidence-group ul {
  display: grid;
  margin: 0.6rem 0 0;
  padding-left: 1.2rem;
  gap: 0.5rem;
}

.source-eligibility-summary__evidence-group li {
  display: grid;
  gap: 0.1rem;
}

.source-eligibility-summary__evidence-group li span,
.source-eligibility-summary__evidence-group li small {
  overflow-wrap: anywhere;
}

.source-eligibility-summary__evidence-group li small {
  color: var(--studio-muted);
}

.source-eligibility-summary__evidence-group p {
  margin: 0;
  color: var(--studio-muted);
}

@media (max-width: 700px) {
  .source-eligibility-summary__jurisdictions,
  .source-eligibility-summary__facts {
    grid-template-columns: 1fr;
  }
}
</style>
