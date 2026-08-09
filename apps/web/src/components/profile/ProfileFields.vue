<script setup lang="ts">
import type { ReaderEditionKey } from "../../lib/api";
import { readerEditionDescription, readerEditionLabel, readerEditionOrder } from "../../lib/reader-editions";
defineProps<{ nameId: string; levelId: string; disabled?: boolean }>();
const name = defineModel<string>("name", { required: true });
const readingLevel = defineModel<ReaderEditionKey>("readingLevel", { required: true });
</script>
<template>
  <fieldset class="profile-fields" :disabled="disabled">
    <legend>Reader details</legend>
    <div class="profile-fields__field">
      <label :for="nameId">Reader name</label>
      <input :id="nameId" v-model="name" maxlength="80" required />
    </div>
    <div class="profile-fields__field">
      <label :for="levelId">Reading level</label>
      <select :id="levelId" v-model="readingLevel" :aria-describedby="`${levelId}-hint`">
        <option v-for="key in readerEditionOrder" :key="key" :value="key">
          {{ readerEditionLabel(key) }}
        </option>
      </select>
      <p :id="`${levelId}-hint`" class="profile-fields__hint">
        {{ readerEditionDescription(readingLevel) }}
      </p>
    </div>
  </fieldset>
</template>
<style scoped>
.profile-fields {
  display: grid;
  gap: 1rem;
  min-width: 0;
  margin: 0;
  border: 0;
  padding: 0;
}

.profile-fields legend {
  margin-bottom: 0.25rem;
  color: var(--panda-ink);
  font-family: var(--panda-serif);
  font-size: 1.2rem;
  font-weight: 680;
}

.profile-fields__field { display: grid; gap: 0.42rem; }
.profile-fields label { font-weight: 800; }

.profile-fields input,
.profile-fields select {
  width: 100%;
  min-height: 2.75rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  padding: 0.58rem 0.72rem;
  background: var(--panda-white);
  color: var(--panda-ink);
  font: inherit;
}

.profile-fields :is(input, select):focus-visible {
  outline: 3px solid var(--panda-focus);
  outline-offset: 3px;
}

.profile-fields__hint {
  margin: 0;
  color: var(--panda-muted);
  font-size: 0.88rem;
  line-height: 1.45;
}

@media (forced-colors: active) {
  .profile-fields :is(input, select) { border-color: CanvasText; background: Canvas; color: CanvasText; }
}
</style>
