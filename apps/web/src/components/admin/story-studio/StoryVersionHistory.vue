<script setup lang="ts">
import type { AdminVersionSummary } from '@/lib/api'
import { versionCanSeedDraft, versionRoleLabels } from '@/lib/story-studio-navigation'
import StoryStatusBadge from './StoryStatusBadge.vue'

defineProps<{
  versions: AdminVersionSummary[]
  editionLabel: string
}>()

const emit = defineEmits<{ edit: [versionId: string] }>()
const formatDate = (value: string) =>
  new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(value))
</script>

<template>
  <section aria-labelledby="version-history-title">
    <div class="history-heading">
      <div>
        <h2 id="version-history-title">{{ editionLabel }} history</h2>
        <p>Every saved version is immutable. Publication is managed by story releases.</p>
      </div>
    </div>
    <ol class="version-list">
      <li v-for="version in versions" :key="version.versionId" class="version-row">
        <span class="version-row__number">v{{ version.version }}</span>
        <div class="version-row__body">
          <div class="version-row__title">
            <strong>{{ versionRoleLabels(version).join(' · ') }}</strong>
            <StoryStatusBadge :version-health="version.health" />
          </div>
          <p>{{ version.wordCount.toLocaleString('en-GB') }} words · {{ version.segmentCount }} segments · {{ version.chapterCount }} chapters</p>
          <p>Saved {{ formatDate(version.createdAt) }}</p>
          <p v-if="version.health !== 'ready'" class="version-row__warning">This stored version cannot safely be reused or included in a release.</p>
        </div>
        <button
          type="button"
          class="studio-button studio-button--quiet"
          :disabled="!versionCanSeedDraft(version)"
          @click="emit('edit', version.versionId)"
        >
          Edit as new draft
        </button>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.history-heading{display:flex;align-items:flex-end;justify-content:space-between;gap:1rem;margin-bottom:.8rem}.history-heading h2{font-family:var(--panda-serif);font-size:1.15rem;font-weight:650}.history-heading p{margin-top:.25rem;color:var(--studio-muted);font-size:.85rem}.version-list{display:grid;gap:.65rem}.version-row{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.85rem;border:1px solid var(--studio-line);border-radius:var(--panda-radius-compact);background:var(--panda-paper);padding:.75rem}.version-row__number{display:grid;width:2.5rem;height:2.5rem;place-items:center;border-radius:50%;background:var(--panda-mist);font-weight:800}.version-row__title{display:flex;align-items:center;flex-wrap:wrap;gap:.45rem}.version-row__body p{margin-top:.18rem;color:var(--studio-muted);font-size:.78rem}.version-row__warning{color:var(--panda-warning)!important}@media(max-width:650px){.version-row{grid-template-columns:auto 1fr}.version-row>.studio-button{grid-column:1/-1;width:100%}}
</style>
