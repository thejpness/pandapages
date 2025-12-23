<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'

const route = useRoute()
const router = useRouter()

const tabs = [
  { label: 'Upload', to: '/admin/upload' },
  { label: 'AI Create', to: '/admin/ai' },
]

function isActive(to: string) {
  return route.path === to || route.path.startsWith(to + '/')
}
</script>

<template>
  <div class="min-h-dvh bg-[#0B1724] text-white">
    <header class="sticky top-0 z-20 border-b border-white/10 bg-[#0B1724]/70 backdrop-blur">
      <div class="max-w-6xl mx-auto px-4 md:px-8 py-4 pt-[calc(1rem+env(safe-area-inset-top))]">
        <div class="flex items-end justify-between gap-4">
          <div>
            <h1 class="text-2xl font-semibold tracking-tight">Admin</h1>
            <p class="text-sm opacity-80">Upload stories or generate via AI</p>
          </div>

          <button
            type="button"
            class="rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm hover:bg-white/10 active:scale-[0.99] transition"
            @click="router.push('/library')"
            aria-label="Back to library"
            title="Back to library"
          >
            ← Library
          </button>
        </div>

        <nav class="mt-4 flex gap-2">
          <router-link
            v-for="t in tabs"
            :key="t.to"
            :to="t.to"
            class="rounded-xl border border-white/10 px-3 py-2 text-sm transition"
            :class="isActive(t.to) ? 'bg-white text-black' : 'bg-white/5 hover:bg-white/10'"
          >
            {{ t.label }}
          </router-link>
        </nav>
      </div>
    </header>

    <main class="max-w-6xl mx-auto px-4 md:px-8 py-6 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
      <router-view />
    </main>
  </div>
</template>
