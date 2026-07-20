<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import { authState } from '../lib/session'
import { safeNextPath } from '../lib/session-navigation'
import { navigationDidFail } from '../lib/session-transitions'

const route = useRoute()
const router = useRouter()

const retrying = ref(false)
const retryError = ref('')

async function retry() {
  if (retrying.value) return

  retrying.value = true
  retryError.value = ''
  const next = safeNextPath(route.query.next)
  const state = await authState.retry()

  try {
    let result
    if (state === 'unlocked') {
      result = await router.replace(next)
    } else if (state === 'locked') {
      result = await router.replace({ path: '/unlock', query: { next } })
    } else {
      retryError.value = 'Panda Pages still cannot verify the session. The server or database may be temporarily unavailable.'
      return
    }
    if (navigationDidFail(result)) {
      retryError.value = 'The session was verified, but Panda Pages could not open the next page. Try again.'
    }
  } catch {
    retryError.value = 'The session was verified, but Panda Pages could not open the next page. Try again.'
  } finally {
    retrying.value = false
  }
}
</script>

<template>
  <PandaAuthShell
    eyebrow="Session check"
    title="Panda Pages could not verify the session"
    description="The server or database may be temporarily unavailable. Your session has not been treated as signed out."
  >
    <p v-if="retryError" class="session-unavailable__error" role="alert">
      {{ retryError }}
    </p>
    <button
      type="button"
      class="session-unavailable__retry"
      :disabled="retrying"
      @click="retry"
    >
      {{ retrying ? 'Checking…' : 'Try again' }}
    </button>
  </PandaAuthShell>
</template>

<style scoped>
.session-unavailable__error {
  margin: 0 0 0.9rem;
  border: 1px solid color-mix(in srgb, var(--panda-danger) 45%, transparent);
  border-radius: var(--panda-radius-compact);
  padding: 0.75rem 0.85rem;
  background: var(--panda-danger-surface);
  color: var(--panda-danger);
  font-size: 0.86rem;
  font-weight: 650;
  line-height: 1.5;
}

.session-unavailable__retry {
  width: 100%;
  min-height: 2.75rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.7rem 1rem;
  background: var(--panda-ink);
  color: var(--panda-paper-raised);
  font: inherit;
  font-weight: 800;
  cursor: pointer;
}

.session-unavailable__retry:hover:not(:disabled) {
  box-shadow: inset 0 0 0 2px var(--panda-paper-raised);
}

.session-unavailable__retry:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

@media (forced-colors: active) {
  .session-unavailable__error,
  .session-unavailable__retry {
    border-color: CanvasText;
    background: Canvas;
    color: CanvasText;
  }
}
</style>
