<script setup lang="ts">
export type SourceEvidenceReferenceInput = {
  source: string;
  fact: string;
  locator: string;
};

const props = defineProps<{
  label: string;
  idPrefix: string;
  modelValue: SourceEvidenceReferenceInput;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: SourceEvidenceReferenceInput];
}>();

function update(field: keyof SourceEvidenceReferenceInput, value: string) {
  emit("update:modelValue", { ...props.modelValue, [field]: value });
}
</script>

<template>
  <fieldset class="source-evidence-reference">
    <legend>{{ label }} evidence</legend>
    <div class="source-evidence-reference__fields">
      <div class="studio-field">
        <label :for="`${idPrefix}-evidence-source`"
          >{{ label }} evidence source</label
        >
        <input
          :id="`${idPrefix}-evidence-source`"
          :value="modelValue.source"
          @input="update('source', ($event.target as HTMLInputElement).value)"
        />
      </div>
      <div class="studio-field">
        <label :for="`${idPrefix}-observed-fact`"
          >{{ label }} observed fact</label
        >
        <input
          :id="`${idPrefix}-observed-fact`"
          :value="modelValue.fact"
          @input="update('fact', ($event.target as HTMLInputElement).value)"
        />
      </div>
      <div class="studio-field">
        <label :for="`${idPrefix}-evidence-locator`"
          >{{ label }} evidence locator / reference (optional)</label
        >
        <input
          :id="`${idPrefix}-evidence-locator`"
          :value="modelValue.locator"
          type="text"
          placeholder="URL, catalogue identifier, title page, or page reference"
          @input="update('locator', ($event.target as HTMLInputElement).value)"
        />
      </div>
    </div>
  </fieldset>
</template>

<style scoped>
.source-evidence-reference {
  min-width: 0;
  border: 1px solid var(--studio-line);
  border-radius: var(--panda-radius-compact);
  padding: 0.8rem;
}

.source-evidence-reference legend {
  padding: 0 0.25rem;
  color: var(--studio-muted);
  font-size: 0.9rem;
  font-weight: 720;
}

.source-evidence-reference__fields {
  display: grid;
  gap: 0.7rem;
}
</style>
