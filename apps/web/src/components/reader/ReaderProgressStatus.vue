<script setup lang="ts">
defineProps<{ visible: boolean; busy: boolean }>()
const emit = defineEmits<{ retry: []; leave: [] }>()
</script>

<template>
  <aside v-if="visible" class="reader-save-gate" role="alert">
    <p>Progress could not be saved.</p>
    <div class="reader-save-gate-actions">
      <button
        class="reader-button reader-button--primary"
        type="button"
        :disabled="busy"
        @click="emit('retry')"
      >
        Retry
      </button>
      <button class="reader-button reader-button--quiet" type="button" @click="emit('leave')">
        Leave anyway
      </button>
    </div>
  </aside>
</template>
