<script setup lang="ts">
import { ref } from 'vue'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import { startSupabaseLogin } from '../lib/supabase-auth'

const busy = ref(false)
const errorMessage = ref('')

async function signIn() {
  if (busy.value) return
  busy.value = true
  errorMessage.value = ''
  try {
    await startSupabaseLogin()
  } catch {
    errorMessage.value = 'Secure sign-in could not start. Check the local Auth configuration and try again.'
    busy.value = false
  }
}
</script>

<template>
  <PandaAuthShell
    eyebrow="Identity foundation"
    title="Sign in to Panda Pages"
    description="This new adult account flow is being prepared for the application authentication cutover."
  >
    <div class="identity-actions">
      <button type="button" class="identity-primary" :disabled="busy" @click="signIn">
        {{ busy ? 'Opening secure sign-in…' : 'Continue with Google' }}
      </button>
      <p v-if="errorMessage" class="identity-error" role="alert">{{ errorMessage }}</p>
      <p class="identity-note">
        Reader, Library, Journey and Story Studio still use the temporary shared-passcode flow in this development build.
      </p>
      <RouterLink to="/unlock">Use temporary passcode</RouterLink>
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
