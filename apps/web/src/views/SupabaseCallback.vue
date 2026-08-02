<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import { completeSupabaseCallback, onboardIdentity } from '../lib/supabase-auth'

const router = useRouter()
const errorMessage = ref('')

onMounted(async () => {
  try {
    const session = await completeSupabaseCallback(new URL(window.location.href).search)
    await onboardIdentity(session.access_token)
    await router.replace('/account')
  } catch {
    errorMessage.value = 'Secure sign-in could not be completed. Start again from Panda Pages.'
  }
})
</script>

<template>
  <PandaAuthShell
    eyebrow="Secure callback"
    title="Completing sign-in"
    description="Panda Pages is verifying your external identity and preparing your account."
  >
    <p v-if="!errorMessage" class="callback-status" role="status">Please wait…</p>
    <div v-else class="callback-error" role="alert">
      <p>{{ errorMessage }}</p>
      <RouterLink :to="{ path: '/account/login' }">Return to sign-in</RouterLink>
    </div>
  </PandaAuthShell>
</template>

<style scoped>
.callback-status,
.callback-error p {
  margin: 0;
  line-height: 1.5;
}

.callback-status {
  color: var(--panda-muted);
}

.callback-error {
  display: grid;
  gap: 1rem;
  color: var(--panda-danger, #8b1e1e);
}

.callback-error a {
  width: fit-content;
  color: var(--panda-ink);
  font-weight: 750;
}
</style>
