<script setup lang="ts">
import { computed } from 'vue'
import { haptic } from '../lib/haptics'

const props = defineProps<{
  title: string
  author?: string | null
  meta?: string | null
  // 0..1 (optional) for continue/recent
  progress?: number | null
  // optional: override cover letters (e.g. "PP")
  badge?: string | null

  // Optional “secondary action” (e.g. show details / preview modal)
  showInfo?: boolean
  onInfo?: (() => void) | null
}>()

function clamp01(n: number) {
  if (!Number.isFinite(n)) return 0
  return Math.max(0, Math.min(1, n))
}

const progress01 = computed(() => clamp01(Number(props.progress ?? 0)))
const progressPct = computed(() => Math.round(progress01.value * 100))

const coverLetters = computed(() => {
  const b = (props.badge || '').trim()
  if (b) return b.slice(0, 2).toUpperCase()

  const t = (props.title || '').trim()
  if (!t) return 'PP'
  const words = t.split(/\s+/).filter(Boolean)
  const letters = (words[0]?.[0] || '') + (words[1]?.[0] || words[0]?.[1] || '')
  return (letters || 'PP').toUpperCase()
})

const showProgress = computed(() => props.progress != null)

function infoClick(e: MouseEvent) {
  e.preventDefault()
  e.stopPropagation()
  haptic('select')
  props.onInfo?.()
}
</script>

<template>
  <div class="flex items-start gap-4">
    <!-- Cover tile -->
    <div class="relative h-14 w-11 shrink-0 rounded-xl border border-white/10 overflow-hidden bg-white/5">
      <!-- subtle “paper” gradient -->
      <div class="absolute inset-0 bg-linear-to-b from-white/15 via-white/8 to-white/0"></div>

      <!-- shine -->
      <div class="absolute -left-6 top-0 h-full w-10 rotate-12 bg-white/10 blur-[1px] opacity-40"></div>

      <!-- badge -->
      <div class="relative h-full w-full grid place-items-center">
        <div class="text-[10px] font-semibold tracking-wide opacity-90">
          {{ coverLetters }}
        </div>
      </div>

      <!-- progress bar (bottom) -->
      <div v-if="showProgress" class="absolute left-0 right-0 bottom-0 h-0.75 bg-black/25">
        <div class="h-0.75 bg-white/60" :style="{ width: `${progressPct}%` }"></div>
      </div>
    </div>

    <!-- Text -->
    <div class="min-w-0 flex-1">
      <div class="flex items-start justify-between gap-2">
        <!-- Title: allow 2 lines -->
        <div class="text-[17px] font-semibold leading-snug line-clamp-2">
          {{ title }}
        </div>

        <!-- Optional info icon (secondary action) -->
        <button
          v-if="showInfo && onInfo"
          type="button"
          class="shrink-0 mt-0.5 rounded-xl border border-white/10 bg-white/5 px-2 py-1 text-xs
                 opacity-80 hover:opacity-100 hover:bg-white/10 active:scale-[0.99] transition"
          @click="infoClick"
          aria-label="Story info"
          title="Story info"
        >
          ⓘ
        </button>
      </div>

      <div v-if="author" class="mt-1 text-sm opacity-75 truncate">
        {{ author }}
      </div>

      <div class="mt-3 flex items-center gap-2">
        <span
          v-if="meta"
          class="max-w-full truncate rounded-full border border-white/10 bg-white/5 px-2.5 py-1 text-[11px] opacity-70"
        >
          {{ meta }}
        </span>

        <span
          v-if="showProgress"
          class="shrink-0 rounded-full border border-white/10 bg-white/5 px-2.5 py-1 text-[11px] opacity-70"
        >
          {{ progressPct }}%
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.line-clamp-2 {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
</style>
