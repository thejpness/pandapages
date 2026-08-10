<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<template>
  <span class="public-trust-links">
    <RouterLink class="public-trust-links__link" to="/privacy">Privacy</RouterLink>
    <span class="public-trust-links__separator" aria-hidden="true">·</span>
    <RouterLink class="public-trust-links__link" to="/content-and-copyright">Content &amp; Copyright</RouterLink>
  </span>
</template>

<style scoped>
.public-trust-links {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
}

.public-trust-links__link {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  color: inherit;
  font: inherit;
  font-weight: 750;
  text-decoration: underline;
  text-decoration-thickness: 0.08em;
  text-underline-offset: 0.16em;
}

.public-trust-links__link:focus-visible {
  outline: 3px solid var(--panda-focus, var(--ink));
  outline-offset: 3px;
}

.public-trust-links__separator {
  color: var(--panda-muted, var(--soft-ink));
  user-select: none;
}

@media (forced-colors: active) {
  .public-trust-links__link {
    color: LinkText;
  }
}
</style>
