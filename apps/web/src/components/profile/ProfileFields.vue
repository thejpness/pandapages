<script setup lang="ts">
import type { ReaderEditionKey } from "../../lib/api";
import { readerEditionDescription, readerEditionLabel, readerEditionOrder } from "../../lib/reader-editions";
defineProps<{ nameId: string; levelId: string; disabled?: boolean }>();
const name = defineModel<string>("name", { required: true });
const readingLevel = defineModel<ReaderEditionKey>("readingLevel", { required: true });
</script>
<template>
  <label :for="nameId">Reader name</label>
  <input :id="nameId" v-model="name" :disabled="disabled" maxlength="80" required />
  <label :for="levelId">Reading level</label>
  <select :id="levelId" v-model="readingLevel" :disabled="disabled"><option v-for="key in readerEditionOrder" :key="key" :value="key">{{ readerEditionLabel(key) }}</option></select>
  <p class="hint">{{ readerEditionDescription(readingLevel) }}</p>
</template>
<style scoped>
label { font-weight: 700; } input,select { min-height:2.7rem; border:1px solid var(--panda-line-strong); border-radius:var(--panda-radius-compact); padding:.55rem .7rem; font:inherit; } .hint { margin:-.5rem 0 0; color:var(--panda-muted); font-size:.86rem; line-height:1.4; }
</style>
