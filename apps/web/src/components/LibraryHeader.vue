<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { haptic } from '../lib/haptics'

type Resume = { slug: string; title: string; author?: string; percent: number } | null
type Recent = { slug: string; title: string; author?: string; percent: number }

const props = defineProps<{
  loading: boolean
  q: string
  resultsLabel: string
  resume: Resume
  recent: Recent[]
  personalisationLabel: string
  hasPersonalisation: boolean
}>()

const emit = defineEmits<{
  (e: 'update:q', v: string): void
  (e: 'admin'): void
  (e: 'journey'): void
  (e: 'random'): void
  (e: 'clear'): void
  (e: 'top'): void
  (e: 'go', slug: string): void
}>()

const searchRef = ref<HTMLInputElement | null>(null)

const qModel = computed({
  get: () => props.q,
  set: (v: string) => emit('update:q', v),
})

function focusSearch() {
  searchRef.value?.focus()
}

defineExpose({ focusSearch })

async function clear() {
  emit('clear')
  await nextTick()
  focusSearch()
}
</script>

<template>
  <header class="sticky top-0 z-20 border-b border-white/10 bg-[#0B1724]/70 backdrop-blur">
    <div class="max-w-6xl mx-auto px-4 md:px-8 py-4 pt-[calc(1rem+env(safe-area-inset-top))]">
      <div class="flex items-end justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight">Library</h1>
          <p class="text-sm opacity-80">Tap a story to read</p>
          <p class="mt-1 text-xs opacity-70">{{ personalisationLabel }}</p>
        </div>

        <div class="hidden sm:flex items-center gap-2">
          <button
            type="button"
            class="rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="emit('journey')"
          >
            ✨ {{ hasPersonalisation ? 'Edit personalisation' : 'Personalise' }}
          </button>

          <button
            type="button"
            class="rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm hover:bg-white/10 active:scale-[0.99] transition"
            @pointerdown="haptic('select')"
            @click="emit('admin')"
          >
            🛠 Admin
          </button>

          <button
            type="button"
            class="rounded-xl border border-white/10 bg-white/5 px-3 py-2 text-sm hover:bg-white/10 active:scale-[0.99] transition"
            @click="emit('random')"
            :disabled="props.loading"
            :class="props.loading ? 'opacity-50 cursor-not-allowed' : ''"
          >
            🎲 Random
          </button>
        </div>
      </div>

      <div class="mt-4 flex items-center gap-3">
        <div class="relative flex-1">
          <input
            ref="searchRef"
            v-model="qModel"
            placeholder="Search stories…"
            class="w-full rounded-2xl bg-black/20 border border-white/10 px-4 py-3 pr-10 text-sm outline-none focus:border-white/25"
            inputmode="search"
            autocomplete="off"
            autocapitalize="none"
            enterkeyhint="go"
            @keydown.enter.prevent="emit('top')"
          />
          <button
            v-if="qModel"
            type="button"
            class="absolute right-2 top-1/2 -translate-y-1/2 rounded-xl px-2 py-1 text-xs opacity-80 hover:opacity-100 hover:bg-white/10 transition"
            @pointerdown="haptic('select')"
            @click="clear"
          >
            ✕
          </button>
        </div>

        <div class="text-xs opacity-70 whitespace-nowrap">
          {{ resultsLabel }}
        </div>
      </div>

      <div v-if="!props.loading && props.resume" class="mt-4">
        <button
          type="button"
          class="w-full text-left rounded-2xl border border-white/10 bg-white/10 p-5 hover:bg-white/15 active:scale-[0.995] transition"
          @pointerdown="haptic('select')"
          @click="emit('go', props.resume.slug)"
        >
          <div class="text-xs opacity-80">Resume</div>
          <div class="mt-1 flex items-baseline justify-between gap-3">
            <div class="text-lg font-semibold truncate">{{ props.resume.title }}</div>
            <div class="text-xs opacity-70 shrink-0">
              {{ props.resume.percent }}%
            </div>
          </div>
          <div v-if="props.resume.author" class="mt-1 text-sm opacity-75 truncate">
            {{ props.resume.author }}
          </div>
        </button>
      </div>

      <div v-if="!props.loading && props.recent.length" class="mt-3">
        <div class="text-xs uppercase tracking-wide opacity-60 mb-2">Recent</div>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <button
            v-for="r in props.recent"
            :key="r.slug"
            type="button"
            class="text-left rounded-2xl border border-white/10 bg-white/5 p-4 hover:bg-white/10 active:scale-[0.995] transition"
            @pointerdown="haptic('select')"
            @click="emit('go', r.slug)"
          >
            <div class="text-xs opacity-70 mb-1">{{ r.percent }}%</div>
            <div class="font-semibold truncate">{{ r.title }}</div>
            <div v-if="r.author" class="text-sm opacity-75 truncate">{{ r.author }}</div>
          </button>
        </div>
      </div>
    </div>
  </header>
</template>
