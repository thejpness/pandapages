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
  border-radius: 999px;
  padding: 0.3rem 0.65rem;
  font-size: 0.75rem;
  font-weight: 750;
  line-height: 1.15;
}

.studio-status--good { background: #dcefe8; color: #15584f; }
.studio-status--draft { background: #e8e6f6; color: #4d4885; }
.studio-status--attention { background: #f9e2d7; color: #8a3f27; }
.studio-status--quiet { background: #e9ece8; color: #536467; }
</style>
