<script setup lang="ts">
import type { AdminValidationIssue } from '@/lib/api'

defineProps<{
  issues: readonly AdminValidationIssue[]
  title?: string
}>()

const emit = defineEmits<{ focus: [field: string] }>()
</script>

<template>
  <section v-if="issues.length" class="validation-summary" aria-labelledby="validation-summary-title" tabindex="-1">
    <h2 id="validation-summary-title">{{ title ?? 'Check these fields' }}</h2>
    <ul>
      <li v-for="(issue, index) in issues" :key="`${issue.field}-${issue.code}-${index}`">
        <button type="button" @click="emit('focus', issue.field)">{{ issue.message }}</button>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.validation-summary {
  border: 1px solid #e1a68e;
  border-left-width: 4px;
  border-radius: 0.9rem;
  background: #fff4ee;
  padding: 1rem 1.1rem;
  color: #753a27;
}

.validation-summary h2 {
  font-size: 0.95rem;
  font-weight: 780;
}

.validation-summary ul {
  margin-top: 0.45rem;
  padding-left: 1.2rem;
  list-style: disc;
}

.validation-summary button {
  min-height: 2rem;
  text-decoration: underline;
  text-underline-offset: 0.18em;
}
</style>
