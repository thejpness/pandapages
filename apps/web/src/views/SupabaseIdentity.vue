<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import {
  reconcileAccountMembership,
  selectAccount,
} from '../lib/account-context'
import { logout as logoutSession } from '../lib/api'
import {
  loadIdentity,
  restoreSupabaseSession,
  type AuthenticatedIdentity,
} from '../lib/supabase-auth'

const router = useRouter()
const identity = ref<AuthenticatedIdentity | null>(null)
const errorMessage = ref('')
const busy = ref(true)

function chooseAccount(accountID: string) {
  selectAccount(accountID)
  void router.replace('/profiles')
}

async function signOut() {
  if (busy.value) return
  busy.value = true
  errorMessage.value = ''
  try {
    await logoutSession()
    await router.replace('/account/login')
  } catch {
    errorMessage.value = 'Sign-out could not be completed. Try again.'
    busy.value = false
  }
}

onMounted(async () => {
  try {
    const session = await restoreSupabaseSession()
    if (!session) {
      await router.replace('/account/login')
      return
    }
    const loadedIdentity = await loadIdentity(session.access_token)
    const membership = reconcileAccountMembership(loadedIdentity.memberships)
    if (membership !== null) {
      selectAccount(membership.accountId)
      await router.replace('/profiles')
      return
    }
    identity.value = loadedIdentity
  } catch {
    errorMessage.value = 'Your Panda Pages identity could not be loaded.'
  } finally {
    busy.value = false
  }
})

</script>

<template>
  <PandaAuthShell
    eyebrow="Panda Pages"
    title="Choose an account"
    description="Choose the Panda Pages account you want to use."
  >
    <p v-if="busy" class="identity-status" role="status">Loading identity…</p>
    <p v-else-if="errorMessage" class="identity-error" role="alert">{{ errorMessage }}</p>
    <div v-else-if="identity" class="identity-card">
      <p class="identity-name">{{ identity.principal.displayName }}</p>
      <ul aria-label="Account memberships">
        <li v-for="membership in identity.memberships" :key="membership.accountId">
          <span>{{ membership.accountName }}</span>
          <small>{{ membership.role }}</small>
          <button type="button" @click="chooseAccount(membership.accountId)">Choose</button>
        </li>
      </ul>
      <p class="identity-note">Your account choice is checked against current memberships on every request.</p>
      <button type="button" :disabled="busy" @click="signOut">Sign out</button>
    </div>
  </PandaAuthShell>
</template>

<style scoped>
.identity-status,
.identity-error,
.identity-name,
.identity-note {
  margin: 0;
}

.identity-error {
  color: var(--panda-danger, #8b1e1e);
  font-weight: 700;
}

.identity-card {
  display: grid;
  gap: 1rem;
}

.identity-name {
  font-family: var(--panda-serif);
  font-size: 1.35rem;
  font-weight: 700;
}

ul {
  display: grid;
  gap: 0.6rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

li {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  padding: 0.75rem;
}

li span {
  overflow-wrap: anywhere;
  font-weight: 750;
}

li small {
  color: var(--panda-muted);
  text-transform: capitalize;
}

.identity-note {
  color: var(--panda-muted);
  font-size: 0.9rem;
  line-height: 1.5;
}

button {
  width: fit-content;
  min-height: 2.75rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.65rem 1rem;
  background: var(--panda-paper-raised);
  color: var(--panda-ink);
  font: inherit;
  font-weight: 750;
  cursor: pointer;
}
</style>
