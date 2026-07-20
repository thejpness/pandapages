<script setup lang="ts">
import { ref } from 'vue'

const prompt = ref('')
const targetAge = ref(3)
const maxChars = ref(25000)

const generating = ref(false)
const result = ref<string>('')

async function generate() {
  generating.value = true
  try {
    // TODO: wire to your API
    // const res = await fetch('/api/v1/admin/ai/generate', { ... })
    await new Promise(r => setTimeout(r, 300))
    result.value =
      `# (stub) Generated story\n\nPrompt: ${prompt.value}\n\n(Connect this to your backend + OpenAI later.)`
  } finally {
    generating.value = false
  }
}
</script>

<template>
  <section class="admin-ai studio-panel">
    <div class="admin-ai__heading">
      <div>
        <p class="admin-ai__eyebrow">Story Studio</p>
        <h1>AI create</h1>
        <p>Generate markdown you can then save as a story.</p>
      </div>

      <button
        type="button"
        class="studio-button studio-button--primary"
        :disabled="generating || !prompt.trim()"
        @click="generate"
      >
        {{ generating ? 'Generating…' : 'Generate' }}
      </button>
    </div>

    <div class="admin-ai__options">
      <label>
        Target age
        <input
          v-model.number="targetAge"
          type="number"
          min="1"
          max="12"
        />
      </label>

      <label>
        Max chars
        <input
          v-model.number="maxChars"
          type="number"
          min="1000"
          max="5000000"
          step="500"
        />
      </label>

      <div class="admin-ai__later">
        <div>Later: style presets, safety filters, rhyme toggle, etc.</div>
      </div>
    </div>

    <label class="admin-ai__prompt">
      Prompt
      <textarea
        v-model="prompt"
        placeholder="Write a calming bedtime story about a panda learning to share…"
      />
    </label>

    <div v-if="result" class="admin-ai__result">
      <div>Result (markdown)</div>
      <pre>{{ result }}</pre>
    </div>
  </section>
</template>

<style scoped>
.admin-ai {
  color: var(--panda-ink);
}

.admin-ai__heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.admin-ai__eyebrow {
  color: var(--panda-muted);
  font-size: 0.75rem;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.admin-ai h1 {
  margin-top: 0.2rem;
  font-family: var(--panda-serif);
  font-size: clamp(1.7rem, 4vw, 2.4rem);
  font-weight: 650;
}

.admin-ai__heading p:last-child {
  margin-top: 0.35rem;
  color: var(--panda-muted);
}

.admin-ai__options {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: end;
  gap: 0.8rem;
  margin-top: 1.25rem;
}

.admin-ai label {
  color: var(--panda-soft-ink);
  font-size: 0.82rem;
  font-weight: 700;
}

.admin-ai input,
.admin-ai textarea {
  display: block;
  width: 100%;
  margin-top: 0.45rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-white);
  color: var(--panda-ink);
  padding: 0.7rem 0.8rem;
  font-size: max(1rem, 16px);
}

.admin-ai input {
  min-height: 2.75rem;
}

.admin-ai textarea {
  min-height: 10rem;
  resize: vertical;
}

.admin-ai__later {
  display: flex;
  align-items: flex-end;
  min-height: 2.75rem;
  color: var(--panda-muted);
  font-size: 0.78rem;
}

.admin-ai__prompt {
  display: block;
  margin-top: 1rem;
}

.admin-ai__result {
  margin-top: 1rem;
}

.admin-ai__result > div {
  margin-bottom: 0.45rem;
  color: var(--panda-muted);
  font-size: 0.72rem;
  font-weight: 750;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.admin-ai__result pre {
  overflow: auto;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-mist);
  padding: 1rem;
  color: var(--panda-ink);
  font-size: 0.9rem;
  white-space: pre-wrap;
}

@media (max-width: 44rem) {
  .admin-ai__heading {
    align-items: stretch;
    flex-direction: column;
  }

  .admin-ai__heading .studio-button {
    width: 100%;
  }

  .admin-ai__options {
    grid-template-columns: 1fr;
  }
}
</style>
