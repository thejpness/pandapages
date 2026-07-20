<script setup lang="ts">
import { computed } from 'vue'
import type { AdminStoryStatus, AdminVersionHealth } from '@/lib/api'
import {
  storyStatusLabel,
  versionHealthLabel,
} from '@/lib/story-studio-navigation'

const props = defineProps<{
  status?: AdminStoryStatus
  health?: AdminVersionHealth
}>()

const label = computed(() =>
  props.status
    ? storyStatusLabel(props.status)
    : props.health
      ? versionHealthLabel(props.health)
      : '',
)

const tone = computed(() => {
  const state = props.status ?? props.health
  if (state === 'published' || state === 'ready') return 'good'
  if (state === 'published_with_draft' || state === 'draft_only') return 'draft'
  if (state === 'repair_required' || state === 'unavailable') return 'attention'
  return 'quiet'
})
</script>

<template>
  <span class="studio-status" :class="`studio-status--${tone}`">{{ label }}</span>
</template>

<style scoped>
.studio-status {
  display: inline-flex;
  align-items: center;
  min-height: 1.8rem;
  max-width: 100%;
  border: 1px solid currentColor;
  border-radius: var(--panda-radius-pill);
  padding: 0.3rem 0.65rem;
  font-size: 0.75rem;
  font-weight: 750;
  line-height: 1.15;
}

.studio-status--good { background: var(--panda-success-surface); color: var(--panda-success); }
.studio-status--draft { background: var(--panda-mist); color: var(--panda-soft-ink); }
.studio-status--attention { background: var(--panda-warning-surface); color: var(--panda-warning); }
.studio-status--quiet { background: var(--panda-paper); color: var(--panda-muted); }
</style>
