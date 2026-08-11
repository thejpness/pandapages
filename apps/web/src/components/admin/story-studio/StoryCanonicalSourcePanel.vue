<script setup lang="ts">
import type { AdminStorySourceSummary } from '@/lib/api'
import StoryStatusBadge from './StoryStatusBadge.vue'
defineProps<{ source: AdminStorySourceSummary }>()
const emit = defineEmits<{ edit: [] }>()
</script>
<template>
  <section class="source-panel studio-panel" aria-labelledby="canonical-source-title">
    <div class="source-panel__heading"><div><p class="studio-page-heading__eyebrow">Canonical original</p><h2 id="canonical-source-title">Source material</h2></div><StoryStatusBadge :source-status="source.status" /></div>
    <p>The canonical source is separate from every Panda Pages reading edition. It is never treated as Classic and never appears directly in Reader.</p>
    <dl><div><dt>Current revision</dt><dd>{{ source.currentVersion ? `r${source.currentVersion.version}` : '-' }}</dd></div><div><dt>Source history</dt><dd>{{ source.versionCount }}</dd></div></dl>
    <button type="button" class="studio-button studio-button--quiet" :disabled="source.status === 'repair_required'" @click="emit('edit')">{{ source.status === 'missing' ? 'Add canonical source' : 'Edit canonical source' }}</button>
    <p v-if="source.status === 'repair_required'" class="source-panel__warning">Stored source provenance needs repair before it can be edited safely.</p>
  </section>
</template>
<style scoped>
.source-panel{display:flex;flex-direction:column;gap:.8rem}.source-panel__heading{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem}.source-panel h2{font-family:var(--panda-serif);font-size:1.2rem;font-weight:650}.source-panel>p{color:var(--studio-muted);line-height:1.5}.source-panel dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.8rem}.source-panel dt{color:var(--studio-muted);font-size:.7rem;text-transform:uppercase}.source-panel dd{margin-top:.2rem;font-weight:720}.source-panel__warning{color:var(--panda-warning)!important;font-weight:650}
</style>
