<script setup lang="ts">
import type { ReaderContentState } from '../../lib/reader-content-state'

defineProps<{ state: ReaderContentState }>()
const emit = defineEmits<{ retry: []; library: [] }>()
</script>

<template>
  <main class="reader-state-shell">
    <section v-if="state.status === 'loading'" class="reader-state-card" aria-live="polite">
      <div class="reader-skeleton reader-skeleton--title" />
      <div class="reader-skeleton" />
      <div class="reader-skeleton reader-skeleton--short" />
      <span class="reader-sr-only">Loading story</span>
    </section>

    <section
      v-else-if="state.status === 'not-found'"
      class="reader-state-card"
      tabindex="-1"
      role="alert"
    >
      <h1>Story not found</h1>
      <p>This story is not available in the Library.</p>
      <button class="reader-button reader-button--primary" type="button" @click="emit('library')">
        Return to Library
      </button>
    </section>

    <section
      v-else-if="state.status === 'unavailable'"
      class="reader-state-card"
      tabindex="-1"
      role="alert"
    >
      <h1>Story unavailable</h1>
      <p>Try again, or return to the Library.</p>
      <div class="reader-state-actions">
        <button class="reader-button reader-button--primary" type="button" @click="emit('retry')">
          Retry
        </button>
        <button class="reader-button reader-button--quiet" type="button" @click="emit('library')">
          Return to Library
        </button>
      </div>
    </section>
  </main>
</template>
