<script setup lang="ts">
type LibraryStateKind =
  | 'empty'
  | 'search'
  | 'server-error'
  | 'malformed'
  | 'unavailable'
  | 'session-ended'

withDefaults(defineProps<{
  kind: LibraryStateKind
  query?: string
  retrying?: boolean
  unavailableCount?: number
}>(), {
  query: '',
  retrying: false,
  unavailableCount: 0,
})

const emit = defineEmits<{
  retry: []
  clear: []
  admin: []
}>()
</script>

<template>
  <section
    class="library-state"
    :class="`library-state--${kind}`"
    :role="kind === 'server-error' || kind === 'malformed' ? 'alert' : undefined"
    aria-live="polite"
  >
    <span class="library-state__mark" aria-hidden="true">
      {{ kind === 'search' ? '⌕' : kind.includes('error') || kind === 'malformed' || kind === 'unavailable' ? '!' : '✦' }}
    </span>

    <template v-if="kind === 'empty'">
      <p class="library-state__eyebrow">The shelf is ready</p>
      <h2>No published stories yet</h2>
      <p>When a parent publishes a story, it will appear here for reading.</p>
      <button type="button" @click="emit('admin')">Open Admin</button>
    </template>

    <template v-else-if="kind === 'search'">
      <p class="library-state__eyebrow">No matches</p>
      <h2>Nothing found for “{{ query }}”</h2>
      <p>Try a title or author, or clear the search to see the whole shelf.</p>
      <button type="button" @click="emit('clear')">Clear search</button>
    </template>

    <template v-else-if="kind === 'server-error'">
      <p class="library-state__eyebrow">A temporary wobble</p>
      <h2>The library could not be loaded</h2>
      <p>Your session is still active. The server may be briefly unavailable.</p>
      <button type="button" :disabled="retrying" @click="emit('retry')">
        {{ retrying ? 'Trying again…' : 'Try again' }}
      </button>
    </template>

    <template v-else-if="kind === 'malformed'">
      <p class="library-state__eyebrow">Incomplete story details</p>
      <h2>The library response could not be read safely</h2>
      <p>No uncertain progress or story information has been shown. Try again in a moment.</p>
      <button type="button" :disabled="retrying" @click="emit('retry')">
        {{ retrying ? 'Trying again…' : 'Try again' }}
      </button>
    </template>

    <template v-else-if="kind === 'unavailable'">
      <p class="library-state__eyebrow">Stories kept safe</p>
      <h2>Stories could not be shown safely</h2>
      <p>
        {{ unavailableCount === 1 ? 'One published story could' : `${unavailableCount} published stories could` }}
        not be shown safely. A parent needs to review the published stories before they can return to the bookshelf.
      </p>
    </template>

    <template v-else>
      <p class="library-state__eyebrow">Session ended</p>
      <h2>Returning to Unlock</h2>
      <p>Your library has been cleared from this screen.</p>
    </template>
  </section>
</template>

<style scoped>
.library-state {
  display: grid;
  width: min(42rem, 100%);
  min-height: 22rem;
  place-items: center;
  align-content: center;
  margin: 1rem auto;
  border: 1px solid var(--library-line-strong);
  border-radius: 2rem;
  padding: clamp(1.4rem, 5vw, 3rem);
  background: var(--library-white);
  text-align: center;
  box-shadow: 0.7rem 0.7rem 0 var(--library-mist);
}

.library-state__mark {
  display: grid;
  width: 4rem;
  height: 4rem;
  place-items: center;
  border: 2px solid var(--library-ink);
  border-radius: 50%;
  background: var(--library-accent);
  font-family: var(--library-serif);
  font-size: 1.5rem;
  font-weight: 800;
}

.library-state__eyebrow {
  margin: 1.2rem 0 0.4rem !important;
  color: var(--library-muted);
  font-size: 0.7rem !important;
  font-weight: 900;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.library-state h2 {
  max-width: 34rem;
  margin: 0;
  font-family: var(--library-serif);
  font-size: clamp(1.7rem, 6vw, 2.7rem);
  font-weight: 630;
  letter-spacing: -0.045em;
  line-height: 1.06;
  overflow-wrap: anywhere;
  text-wrap: balance;
}

.library-state p:not(.library-state__eyebrow) {
  max-width: 31rem;
  margin: 0.85rem 0 0;
  color: var(--library-muted);
  font-size: 0.92rem;
  line-height: 1.55;
}

.library-state button {
  min-height: 3rem;
  margin-top: 1.3rem;
  border: 1px solid var(--library-ink);
  border-radius: 999px;
  padding: 0.65rem 1.15rem;
  background: var(--library-ink);
  color: var(--library-white);
  font-weight: 850;
  cursor: pointer;
}

.library-state button:disabled {
  cursor: wait;
  opacity: 0.55;
}

.library-state--server-error .library-state__mark,
.library-state--malformed .library-state__mark,
.library-state--unavailable .library-state__mark {
  background: #ffd9ae;
}
</style>
