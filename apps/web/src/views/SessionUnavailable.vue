<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
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
  <main class="grid min-h-dvh place-items-center bg-[#0B1724] px-4 text-white">
    <section class="w-full max-w-md rounded-3xl border border-white/10 bg-white/5 p-6">
      <h1 class="text-xl font-semibold">Panda Pages could not verify the session</h1>
      <p class="mt-3 text-sm text-white/75">
        The server or database may be temporarily unavailable. Your session has not been treated as signed out.
      </p>
      <p v-if="retryError" class="mt-3 text-sm text-red-300" role="alert">
        {{ retryError }}
      </p>
      <button
        type="button"
        class="mt-5 w-full rounded-2xl bg-white py-3 font-semibold text-[#0B1724] disabled:opacity-60"
        :disabled="retrying"
        @click="retry"
      >
        {{ retrying ? 'Checking…' : 'Try again' }}
      </button>
    </section>
  </main>
</template>
