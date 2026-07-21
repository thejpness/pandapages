<script setup lang="ts">
import { useRegisterSW } from 'virtual:pwa-register/vue'

const { offlineReady, needRefresh, updateServiceWorker } = useRegisterSW()

function dismissUpdate() {
  offlineReady.value = false
  needRefresh.value = false
}

function applyUpdate() {
  void updateServiceWorker(true)
}
</script>

<template>
  <div class="min-h-dvh bg-[#0B1724] text-white">
    <router-view />

    <aside
      v-if="offlineReady || needRefresh"
      aria-live="polite"
      class="fixed inset-x-4 bottom-4 z-50 mx-auto max-w-md rounded-2xl border border-white/15 bg-[#13283b] p-4 shadow-2xl"
    >
      <p class="text-sm text-white/90">
        {{ needRefresh ? 'A new version of Panda Pages is ready.' : 'Installed and ready to open.' }}
      </p>
      <p v-if="offlineReady && !needRefresh" class="mt-1 text-xs text-white/70">
        Your library, stories and reading progress need an internet connection.
      </p>
      <div class="mt-3 flex justify-end gap-2">
        <button type="button" class="rounded-lg px-3 py-2 text-sm text-white/70" @click="dismissUpdate">
          Dismiss
        </button>
        <button
          v-if="needRefresh"
          type="button"
          class="rounded-lg bg-cyan-400 px-3 py-2 text-sm font-semibold text-slate-950"
          @click="applyUpdate"
        >
          Update
        </button>
      </div>
    </aside>
  </div>
</template>
