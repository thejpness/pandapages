<script setup lang="ts">
import { ref } from 'vue'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import ProductAttribution from '../components/app/ProductAttribution.vue'
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
      <button
        type="button"
        class="identity-provider identity-provider--google"
        :disabled="Boolean(activeProvider)"
        @click="signIn('google')"
      >
        <img class="identity-provider__mark" src="/brand/google-sign-in.svg" alt="" aria-hidden="true" />
        <span>{{ activeProvider === 'google' ? 'Opening secure sign-in…' : 'Continue with Google' }}</span>
        <span aria-hidden="true"></span>
      </button>
      <button
        type="button"
        class="identity-provider identity-provider--facebook"
        :disabled="Boolean(activeProvider)"
        @click="signIn('facebook')"
      >
        <img class="identity-provider__mark" src="/brand/facebook-logo.svg" alt="" aria-hidden="true" />
        <span>{{ activeProvider === 'facebook' ? 'Opening secure sign-in…' : 'Continue with Facebook' }}</span>
        <span aria-hidden="true"></span>
      </button>
      <p v-if="errorMessage" class="identity-error" role="alert">{{ errorMessage }}</p>
    </div>
    <template #footer>
      <ProductAttribution />
    </template>
  </PandaAuthShell>
</template>

<style scoped>
.identity-actions {
  display: grid;
  gap: 0.75rem;
}

.identity-provider {
  display: grid;
  grid-template-columns: 2rem minmax(0, 1fr) 2rem;
  align-items: center;
  gap: 0.75rem;
  width: 100%;
  min-height: 3.25rem;
  border: 1px solid #747775;
  border-radius: var(--panda-radius-compact);
  padding: 0.5rem 0.75rem;
  background: #fff;
  color: #1f1f1f;
  font: inherit;
  font-weight: 800;
  line-height: 1.25;
  text-align: center;
  cursor: pointer;
}

.identity-provider:hover:not(:disabled) {
  background: #f8f9fa;
}

.identity-provider:active:not(:disabled) {
  background: #f1f3f4;
}

.identity-provider:focus-visible {
  outline: 3px solid var(--panda-focus);
  outline-offset: 3px;
}

.identity-provider:disabled {
  cursor: wait;
  opacity: 0.65;
}

.identity-provider__mark {
  display: block;
  width: 2rem;
  height: 2rem;
  object-fit: contain;
}

@media (forced-colors: active) {
  .identity-provider {
    border-color: ButtonText;
    background: ButtonFace;
    color: ButtonText;
  }

  .identity-provider:disabled {
    color: GrayText;
  }
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
