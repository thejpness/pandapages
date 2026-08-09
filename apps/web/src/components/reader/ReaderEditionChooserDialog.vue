<script setup lang="ts">
import ReaderDialogShell from './ReaderDialogShell.vue'
import {
  readerEditionDescription,
  readerEditionLabel,
} from '../../lib/reader-editions'
import type { ReaderEditionKey } from '../../lib/api'

defineProps<{
  open: boolean
  eligibleEditions: readonly ReaderEditionKey[]
  busy: boolean
}>()

const emit = defineEmits<{
  choose: [editionKey: ReaderEditionKey]
  library: []
}>()
</script>

<template>
  <ReaderDialogShell
    :open="open"
    content-id="reader-edition-chooser"
    title="Choose a story edition"
    description="Choose one of the editions available for this reading profile."
    :show-close="false"
  >
    <div class="reader-edition-chooser" aria-label="Available story editions">
      <button
        v-for="editionKey in eligibleEditions"
        :key="editionKey"
        class="reader-edition-chooser__option"
        type="button"
        :disabled="busy"
        @click="emit('choose', editionKey)"
      >
        <strong>{{ readerEditionLabel(editionKey) }}</strong>
        <span>{{ readerEditionDescription(editionKey) }}</span>
      </button>
    </div>

    <div class="reader-dialog-actions">
      <button
        class="reader-button reader-button--quiet"
        type="button"
        :disabled="busy"
        @click="emit('library')"
      >
        Return to Library
      </button>
    </div>
  </ReaderDialogShell>
</template>

<style scoped>
.reader-edition-chooser {
  display: grid;
  gap: 0.7rem;
}

.reader-edition-chooser__option {
  display: grid;
  gap: 0.3rem;
  width: 100%;
  border: 1px solid var(--reader-control-border);
  border-radius: 0.8rem;
  background: var(--reader-control-background);
  padding: 0.9rem 1rem;
  color: var(--reader-control-text);
  text-align: left;
}

.reader-edition-chooser__option:hover:not(:disabled) {
  background: var(--reader-control-hover);
}

.reader-edition-chooser__option:focus-visible {
  outline: 3px solid var(--reader-focus-ring);
  outline-offset: 2px;
}

.reader-edition-chooser__option strong {
  font-size: 1rem;
}

.reader-edition-chooser__option span {
  color: var(--reader-text-secondary);
  line-height: 1.45;
}
</style>
