<script setup lang="ts">
import { ref } from 'vue'

defineProps<{
  modelValue: string
  sourceFilename: string | null
  error: string | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  import: [file: File]
}>()

const fileInput = ref<HTMLInputElement | null>(null)

function chooseFile() {
  fileInput.value?.click()
}

function fileChosen(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) emit('import', file)
}
</script>

<template>
  <section class="markdown-editor" aria-labelledby="markdown-editor-title">
    <div class="markdown-editor__heading">
      <div>
        <h2 id="markdown-editor-title">Story source</h2>
        <p>Write Markdown or import a local text, Markdown or HTML file.</p>
      </div>
      <input
        ref="fileInput"
        class="studio-visually-hidden"
        type="file"
        aria-label="Import story file"
        accept=".txt,.md,.markdown,.html,.htm,text/plain,text/markdown,text/html"
        @change="fileChosen"
      />
      <button type="button" class="studio-button studio-button--quiet" @click="chooseFile">
        Import file
      </button>
    </div>

    <p v-if="sourceFilename" class="markdown-editor__source">
      Imported from <strong>{{ sourceFilename }}</strong>. The original file stays on this device.
    </p>
    <p v-if="error" class="studio-field__error" role="alert">{{ error }}</p>

    <label class="studio-visually-hidden" for="story-markdown">Markdown</label>
    <textarea
      id="story-markdown"
      :value="modelValue"
      spellcheck="true"
      placeholder="# Story title\n\nOnce upon a time…"
      @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
    />
  </section>
</template>

<style scoped>
.markdown-editor__heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.markdown-editor h2 {
  font-family: var(--panda-serif);
  font-size: 1.15rem;
  font-weight: 650;
}

.markdown-editor__heading p,
.markdown-editor__source {
  margin-top: 0.25rem;
  color: var(--studio-muted);
  font-size: 0.86rem;
  line-height: 1.5;
}

.markdown-editor__source {
  border-radius: var(--panda-radius-compact);
  background: var(--panda-mist);
  padding: 0.7rem 0.8rem;
}

.markdown-editor textarea {
  display: block;
  width: 100%;
  min-height: 30rem;
  resize: vertical;
  margin-top: 1rem;
  border: 1px solid var(--studio-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-white);
  color: var(--studio-ink);
  padding: 1rem;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: max(1rem, 16px);
  line-height: 1.65;
  tab-size: 2;
}

.markdown-editor textarea:focus {
  border-color: var(--panda-focus);
  outline: 3px solid color-mix(in srgb, var(--panda-focus) 24%, transparent);
  outline-offset: 2px;
}

@media (max-width: 500px) {
  .markdown-editor__heading { align-items: stretch; flex-direction: column; }
  .markdown-editor textarea { min-height: 22rem; }
}

@media (max-height: 520px) {
  .markdown-editor textarea { min-height: 16rem; }
}
</style>
