<script setup lang="ts">
import type { AdminVersionSummary } from '@/lib/api'
import {
  versionCanPublish,
  versionCanSeedDraft,
  versionRoleLabels,
} from '@/lib/story-studio-navigation'
import StoryStatusBadge from './StoryStatusBadge.vue'

defineProps<{
  versions: readonly AdminVersionSummary[]
  selectedVersionId: string | null
}>()

const emit = defineEmits<{
  select: [versionId: string]
  edit: [versionId: string]
}>()

const dateFormatter = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})
</script>

<template>
  <section class="version-history" aria-labelledby="version-history-title">
    <div class="version-history__heading">
      <div>
        <h2 id="version-history-title">Version history</h2>
        <p>Each version is an immutable source snapshot.</p>
      </div>
    </div>

    <ol>
      <li v-for="version in versions" :key="version.versionId" class="version-row">
        <label class="version-row__choice">
          <input
            type="radio"
            name="publish-version"
            :value="version.versionId"
            :checked="selectedVersionId === version.versionId"
            :disabled="!versionCanPublish(version)"
            :aria-label="`Select version ${version.version} for publication`"
            @change="emit('select', version.versionId)"
          />
          <span class="version-row__number">v{{ version.version }}</span>
        </label>

        <div class="version-row__body">
          <div class="version-row__labels">
            <strong>{{ versionRoleLabels(version).join(' · ') }}</strong>
            <StoryStatusBadge :health="version.health" />
          </div>
          <time :datetime="version.createdAt">{{ dateFormatter.format(new Date(version.createdAt)) }}</time>
          <p>{{ version.wordCount }} words · {{ version.segmentCount }} segments · {{ version.chapterCount }} chapters</p>
          <p v-if="version.health === 'repair_required'" class="version-row__notice">
            This stored version cannot safely be reused or published.
          </p>
          <p v-else-if="version.health === 'unavailable'" class="version-row__notice">
            This version cannot currently be opened.
          </p>
        </div>

        <button
          v-if="versionCanSeedDraft(version)"
          type="button"
          class="studio-button studio-button--quiet"
          @click="emit('edit', version.versionId)"
        >
          Edit as new draft
        </button>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.version-history h2 {
  font-family: 'Literata Variable', Georgia, serif;
  font-size: 1.3rem;
  font-weight: 650;
}

.version-history__heading p {
  margin-top: 0.3rem;
  color: var(--studio-muted);
}

.version-history ol {
  display: grid;
  margin-top: 1rem;
  gap: 0.75rem;
}

.version-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 1rem;
  border: 1px solid var(--studio-line);
  border-radius: 1rem;
  background: var(--studio-card);
  padding: 1rem;
}

.version-row__choice {
  display: flex;
  align-items: center;
  gap: 0.55rem;
}

.version-row__choice input {
  width: 1.15rem;
  height: 1.15rem;
  accent-color: var(--studio-green);
}

.version-row__number {
  font-weight: 780;
}

.version-row__labels {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.55rem;
}

.version-row__body time,
.version-row__body > p {
  display: block;
  margin-top: 0.25rem;
  color: var(--studio-muted);
  font-size: 0.8rem;
}

.version-row__body .version-row__notice {
  color: #8a3f27;
  font-weight: 650;
}

@media (max-width: 680px) {
  .version-row { grid-template-columns: auto minmax(0, 1fr); }
  .version-row > .studio-button { grid-column: 1 / -1; width: 100%; }
}
</style>
