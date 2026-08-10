<script setup lang="ts">
import { ref } from 'vue'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import { startSupabaseLogin, type SupabaseOAuthProvider } from '../lib/supabase-auth'

const activeProvider = ref<SupabaseOAuthProvider | null>(null)
const errorMessage = ref('')

async function signIn(provider: SupabaseOAuthProvider) {
  if (activeProvider.value) return
  activeProvider.value = provider
  errorMessage.value = ''
  try {
    await startSupabaseLogin(provider)
  } catch {
    errorMessage.value = 'Secure sign-in could not start. Check the local Auth configuration and try again.'
    activeProvider.value = null
  }
}
</script>

<template>
  <PandaAuthShell
    eyebrow="Panda Pages"
    title="Sign in to Panda Pages"
    description="Use your adult account to continue to Panda Pages."
  >
    <div class="identity-actions" :aria-busy="activeProvider ? 'true' : undefined">
      <button type="button" class="identity-primary" :disabled="Boolean(activeProvider)" @click="signIn('google')">
        {{ activeProvider === 'google' ? 'Opening secure sign-in…' : 'Continue with Google' }}
      </button>
      <button type="button" class="identity-primary" :disabled="Boolean(activeProvider)" @click="signIn('facebook')">
        {{ activeProvider === 'facebook' ? 'Opening secure sign-in…' : 'Continue with Facebook' }}
      </button>
      <p v-if="errorMessage" class="identity-error" role="alert">{{ errorMessage }}</p>
    </div>
  </PandaAuthShell>
</template>

<style scoped>
.identity-actions {
  display: grid;
  gap: 1rem;
}

.identity-primary {
  min-height: 3rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.75rem 1rem;
  background: var(--panda-ink);
  color: var(--panda-paper);
  font: inherit;
  font-weight: 800;
  cursor: pointer;
}

.identity-primary:disabled {
  cursor: wait;
  opacity: 0.65;
}

.identity-note,
.identity-error {
  margin: 0;
  line-height: 1.5;
}

.identity-note {
  color: var(--panda-muted);
  font-size: 0.9rem;
}

.identity-error {
  color: var(--panda-danger, #8b1e1e);
  font-weight: 700;
}

a {
  width: fit-content;
  color: var(--panda-ink);
  font-weight: 750;
}
</style>
