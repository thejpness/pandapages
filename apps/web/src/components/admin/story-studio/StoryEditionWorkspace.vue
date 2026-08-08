<script setup lang="ts">
import type { AdminEditionDetail, AdminStoryEditionKey } from '@/lib/api'
import { storyEditionDescription, storyEditionLabel } from '@/lib/story-studio-navigation'
import StoryStatusBadge from './StoryStatusBadge.vue'
defineProps<{ editions: readonly AdminEditionDetail[]; activeKey: AdminStoryEditionKey }>()
const emit = defineEmits<{ select: [key: AdminStoryEditionKey]; edit: [key: AdminStoryEditionKey] }>()
</script>
<template>
  <section class="edition-workspace" aria-labelledby="edition-workspace-title">
    <div class="edition-workspace__heading"><div><p class="studio-page-heading__eyebrow">Reading editions</p><h2 id="edition-workspace-title">Five-edition workspace</h2><p>Each slot is independently versioned. Editing one edition never rewrites another.</p></div></div>
    <div class="edition-workspace__grid">
      <article v-for="edition in editions" :key="edition.editionKey" class="edition-card" :class="{ 'edition-card--active': edition.editionKey === activeKey }">
        <button type="button" class="edition-card__select" :aria-pressed="edition.editionKey === activeKey" @click="emit('select', edition.editionKey)">
          <span><strong>{{ storyEditionLabel(edition.editionKey) }}</strong><small>{{ storyEditionDescription(edition.editionKey) }}</small></span>
          <StoryStatusBadge :edition-status="edition.status" />
        </button>
        <dl>
          <div><dt>Draft</dt><dd>{{ edition.draftVersion ? `v${edition.draftVersion.version}` : '—' }}</dd></div>
          <div><dt>Published</dt><dd>{{ edition.publishedVersion ? `v${edition.publishedVersion.version}` : '—' }}</dd></div>
          <div><dt>History</dt><dd>{{ edition.versionCount }}</dd></div>
        </dl>
        <button type="button" class="studio-button studio-button--quiet edition-card__edit" @click="emit('edit', edition.editionKey)">{{ edition.status === 'empty' ? 'Create edition' : 'Edit edition' }}</button>
      </article>
    </div>
  </section>
</template>
<style scoped>
.edition-workspace__heading h2{font-family:var(--panda-serif);font-size:1.45rem;font-weight:650}.edition-workspace__heading>div>p:last-child{margin-top:.35rem;color:var(--studio-muted)}.edition-workspace__grid{display:grid;grid-template-columns:repeat(5,minmax(11rem,1fr));gap:.8rem;margin-top:1rem;overflow-x:auto;padding:.15rem}.edition-card{display:flex;min-width:0;flex-direction:column;border:1px solid var(--studio-line-strong);border-radius:var(--panda-radius-card);background:var(--panda-paper-raised);padding:.85rem}.edition-card--active{border-color:var(--panda-ink);box-shadow:0 0 0 2px var(--panda-ink)}.edition-card__select{display:flex;width:100%;align-items:flex-start;justify-content:space-between;gap:.6rem;text-align:left}.edition-card__select>span{min-width:0}.edition-card__select strong{display:block;font-family:var(--panda-serif);font-size:1rem}.edition-card__select small{display:block;margin-top:.3rem;color:var(--studio-muted);line-height:1.4}.edition-card dl{display:grid;grid-template-columns:repeat(3,1fr);gap:.35rem;margin-top:.8rem;border-top:1px solid var(--studio-line);padding-top:.7rem}.edition-card dt{color:var(--studio-muted);font-size:.65rem;text-transform:uppercase}.edition-card dd{margin-top:.15rem;font-weight:720}.edition-card__edit{width:100%;margin-top:auto}@media(max-width:900px){.edition-workspace__grid{grid-template-columns:repeat(2,minmax(14rem,1fr))}}@media(max-width:560px){.edition-workspace__grid{grid-template-columns:1fr;overflow:visible}}
</style>
