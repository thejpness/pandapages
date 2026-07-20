<script setup lang="ts">
defineProps<{
  kind: 'loading' | 'empty' | 'error' | 'session' | 'forbidden' | 'repair'
  title: string
  message: string
  actionLabel?: string
}>()

const emit = defineEmits<{ action: [] }>()
</script>

<template>
  <section class="studio-state" :data-kind="kind" :aria-busy="kind === 'loading'">
    <div class="studio-state__mark" aria-hidden="true">
      {{ kind === 'loading' ? '…' : kind === 'repair' ? '!' : '◇' }}
    </div>
    <h2>{{ title }}</h2>
    <p>{{ message }}</p>
    <button v-if="actionLabel" type="button" class="studio-button studio-button--primary" @click="emit('action')">
      {{ actionLabel }}
    </button>
  </section>
</template>

<style scoped>
.studio-state {
  display: grid;
  justify-items: center;
  max-width: 42rem;
  margin: 3rem auto;
  border: 1px solid var(--studio-line);
  border-radius: 1.25rem;
  background: var(--studio-card);
  padding: clamp(1.5rem, 6vw, 3.5rem);
  text-align: center;
  box-shadow: var(--studio-shadow-soft);
}

.studio-state__mark {
  display: grid;
  place-items: center;
  width: 3rem;
  height: 3rem;
  border-radius: 1rem;
  background: var(--studio-wash);
  color: var(--studio-green);
  font-size: 1.35rem;
  font-weight: 800;
}

.studio-state h2 {
  margin-top: 1rem;
  font-family: 'Literata Variable', Georgia, serif;
  font-size: 1.3rem;
}

.studio-state p {
  max-width: 34rem;
  margin-top: 0.45rem;
  color: var(--studio-muted);
  line-height: 1.6;
}

.studio-state .studio-button { margin-top: 1.2rem; }
</style>
