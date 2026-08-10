<script setup lang="ts">
import { onUnmounted, watchEffect } from 'vue'
import { RouterLink } from 'vue-router'
import ProductAttribution from './ProductAttribution.vue'
const props = defineProps<{ title: string; summary?: string }>()
watchEffect(() => { document.title = `${props.title} | Panda Pages` })
onUnmounted(() => { document.title = 'Panda Pages' })
</script>

<template>
  <div class="public-info-layout panda-print-surface">
    <a class="public-info-layout__skip" href="#public-info-main">Skip to main content</a>
    <header class="public-info-layout__header">
      <RouterLink class="public-info-layout__brand" to="/" aria-label="Panda Pages home"><img src="/logo.png" alt="" width="48" height="48" decoding="async" /><span><strong>Panda Pages</strong><small>Stories for curious readers</small></span></RouterLink>
      <nav class="public-info-layout__nav" aria-label="Public information"><RouterLink to="/privacy">Privacy</RouterLink></nav>
    </header>
    <main id="public-info-main" class="public-info-layout__main" tabindex="-1"><article><p class="public-info-layout__eyebrow">Panda Pages information</p><h1>{{ title }}</h1><p v-if="summary" class="public-info-layout__summary">{{ summary }}</p><div class="public-info-layout__content"><slot /></div></article></main>
    <footer class="public-info-layout__footer"><ProductAttribution /><nav aria-label="Legal navigation"><RouterLink to="/privacy">Privacy</RouterLink></nav></footer>
  </div>
</template>

<style scoped>
.public-info-layout,.public-info-layout *,.public-info-layout *::before,.public-info-layout *::after{box-sizing:border-box}.public-info-layout{min-height:100dvh;overflow-x:clip;background:var(--panda-paper);color:var(--panda-ink);font-family:var(--panda-sans);-webkit-font-smoothing:antialiased}.public-info-layout :where(a):focus-visible{outline:3px solid var(--panda-focus);outline-offset:3px}.public-info-layout__skip{position:fixed;z-index:100;top:max(.65rem,env(safe-area-inset-top));left:max(.65rem,env(safe-area-inset-left));transform:translateY(-180%);border:2px solid var(--panda-ink);border-radius:var(--panda-radius-compact);padding:.65rem .9rem;background:var(--panda-white);color:var(--panda-ink);font-weight:800;text-decoration:none}.public-info-layout__skip:focus{transform:none}.public-info-layout__header,.public-info-layout__main,.public-info-layout__footer{width:min(100% - 2rem,52rem);margin-inline:auto}.public-info-layout__header{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding-block:max(1rem,env(safe-area-inset-top)) 1.25rem;border-bottom:1px solid var(--panda-line-strong)}.public-info-layout__brand{display:flex;min-width:0;align-items:center;gap:.7rem;color:inherit;text-decoration:none}.public-info-layout__brand img{flex:0 0 auto;border:1px solid var(--panda-line-strong);border-radius:var(--panda-radius-compact);background:var(--panda-white);object-fit:contain}.public-info-layout__brand span{display:grid;min-width:0}.public-info-layout__brand strong{font-size:1rem;font-weight:850;letter-spacing:-.025em}.public-info-layout__brand small{color:var(--panda-muted);font-size:.76rem;font-weight:650}.public-info-layout__nav,.public-info-layout__footer nav{display:flex;flex-wrap:wrap;gap:.8rem}.public-info-layout__nav a,.public-info-layout__footer nav a{display:inline-flex;min-height:2.75rem;align-items:center;color:var(--panda-ink);font-size:.88rem;font-weight:800;text-decoration:underline;text-decoration-thickness:.08em;text-underline-offset:.18em}.public-info-layout__main{padding-block:clamp(2rem,7vw,4.75rem)}.public-info-layout__eyebrow{margin:0 0 .5rem;color:var(--panda-soft-ink);font-size:.75rem;font-weight:850;letter-spacing:.11em;text-transform:uppercase}.public-info-layout h1{margin:0;overflow-wrap:anywhere;font-family:var(--panda-serif);font-size:clamp(2.25rem,8vw,3.7rem);font-weight:680;letter-spacing:-.05em;line-height:1.06;text-wrap:balance}.public-info-layout__summary{max-width:42rem;margin:1rem 0 0;color:var(--panda-muted);font-size:clamp(1rem,2vw,1.15rem);line-height:1.6}.public-info-layout__content{margin-top:clamp(2rem,6vw,3.5rem)}.public-info-layout__footer{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding-block:1.25rem max(1.25rem,env(safe-area-inset-bottom));border-top:1px solid var(--panda-line-strong)}@media (max-width:32rem){.public-info-layout__header,.public-info-layout__footer{align-items:flex-start;flex-direction:column}}@media (forced-colors:active){.public-info-layout{background:Canvas;color:CanvasText}.public-info-layout__header,.public-info-layout__footer,.public-info-layout__brand img{border-color:CanvasText}.public-info-layout__brand img{background:Canvas}.public-info-layout__nav a,.public-info-layout__footer nav a{color:LinkText}}
</style>
