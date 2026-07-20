<script setup lang="ts">
import type { AdminPreviewResponse } from '@/lib/api'
import StoryValidationSummary from './StoryValidationSummary.vue'

defineProps<{
  preview: AdminPreviewResponse | null
  loading: boolean
  outdated: boolean
}>()

const emit = defineEmits<{ focus: [field: string] }>()
</script>

<template>
  <section class="preview-pane" aria-labelledby="story-preview-title" :aria-busy="loading">
    <div class="preview-pane__heading">
      <div>
        <p class="preview-pane__eyebrow">Canonical preview</p>
        <h2 id="story-preview-title">Reader result</h2>
      </div>
      <span v-if="outdated" class="preview-pane__outdated">Preview out of date</span>
    </div>

    <div v-if="loading" class="preview-pane__placeholder" role="status">Preparing preview…</div>
    <div v-else-if="!preview" class="preview-pane__placeholder">
      <p>Choose Preview to validate and render the canonical story.</p>
      <small>Previewing does not save or publish anything.</small>
    </div>
    <template v-else>
      <dl class="preview-pane__counts" aria-label="Preview counts">
        <div><dt>Words</dt><dd>{{ preview.wordCount }}</dd></div>
        <div><dt>Segments</dt><dd>{{ preview.segmentCount }}</dd></div>
        <div><dt>Chapters</dt><dd>{{ preview.chapterCount }}</dd></div>
      </dl>

      <div class="preview-pane__metadata">
        <strong>{{ preview.title }}</strong>
        <span v-if="preview.author">{{ preview.author }}</span>
        <span>{{ preview.language }}</span>
      </div>

      <StoryValidationSummary
        :issues="preview.warnings"
        title="Preview notes"
        @focus="emit('focus', $event)"
      />

      <article class="preview-pane__story studio-rendered-story" v-html="preview.renderedHtml" />
    </template>
  </section>
</template>

<style scoped>
.preview-pane {
  min-width: 0;
}

.preview-pane__heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.preview-pane__eyebrow {
  color: var(--panda-soft-ink);
  font-size: 0.72rem;
  font-weight: 760;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.preview-pane h2 {
  margin-top: 0.15rem;
  font-family: var(--panda-serif);
  font-size: 1.15rem;
  font-weight: 650;
}

.preview-pane__outdated {
  border: 1px solid var(--panda-warning);
  border-radius: var(--panda-radius-pill);
  background: var(--panda-warning-surface);
  color: var(--panda-warning);
  padding: 0.35rem 0.65rem;
  font-size: 0.72rem;
  font-weight: 720;
}

.preview-pane__placeholder {
  display: grid;
  min-height: 18rem;
  place-content: center;
  margin-top: 1.1rem;
  border: 1px dashed var(--studio-line-strong);
  border-radius: var(--panda-radius-card);
  background: var(--panda-paper-raised);
  padding: 2rem;
  color: var(--studio-muted);
  text-align: center;
}

.preview-pane__placeholder small { margin-top: 0.3rem; }

.preview-pane__counts {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  margin-top: 1rem;
  border-block: 1px solid var(--studio-line);
  padding-block: 0.8rem;
}

.preview-pane__counts div { text-align: center; }
.preview-pane__counts div + div { border-left: 1px solid var(--studio-line); }
.preview-pane__counts dt { color: var(--studio-muted); font-size: 0.72rem; }
.preview-pane__counts dd { margin-top: 0.15rem; font-size: 1.05rem; font-weight: 750; }

.preview-pane__metadata {
  display: flex;
  flex-wrap: wrap;
  gap: 0.35rem 0.8rem;
  margin-block: 1rem;
  color: var(--studio-muted);
  font-size: 0.85rem;
}

.preview-pane__metadata strong { width: 100%; color: var(--studio-ink); }

.preview-pane__story {
  max-height: 42rem;
  overflow: auto;
  margin-top: 1rem;
  border: 1px solid var(--studio-line);
  border-radius: var(--panda-radius-card);
  background: var(--panda-white);
  padding: clamp(1rem, 4vw, 2.2rem);
}
</style>
