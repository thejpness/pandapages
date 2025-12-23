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
  <section class="rounded-2xl border border-white/10 bg-white/5 p-5">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h2 class="text-lg font-semibold">AI create</h2>
        <p class="mt-1 text-sm opacity-75">Generate markdown you can then save as a story.</p>
      </div>

      <button
        type="button"
        class="rounded-xl bg-white text-black px-4 py-2 text-sm font-medium disabled:opacity-60"
        :disabled="generating || !prompt.trim()"
        @click="generate"
      >
        {{ generating ? 'Generating…' : 'Generate' }}
      </button>
    </div>

    <div class="mt-5 grid grid-cols-1 md:grid-cols-3 gap-3">
      <label class="text-xs opacity-80">
        Target age
        <input
          v-model.number="targetAge"
          type="number"
          min="1"
          max="12"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-2 text-sm outline-none focus:border-white/25"
        />
      </label>

      <label class="text-xs opacity-80">
        Max chars
        <input
          v-model.number="maxChars"
          type="number"
          min="1000"
          max="5000000"
          step="500"
          class="mt-2 w-full rounded-xl bg-black/20 border border-white/10 px-3 py-2 text-sm outline-none focus:border-white/25"
        />
      </label>

      <div class="text-xs opacity-80 flex items-end">
        <div class="opacity-70">Later: style presets, safety filters, rhyme toggle, etc.</div>
      </div>
    </div>

    <label class="block mt-4 text-xs opacity-80">
      Prompt
      <textarea
        v-model="prompt"
        class="mt-2 w-full min-h-40 rounded-2xl bg-black/20 border border-white/10 px-4 py-3 text-sm outline-none focus:border-white/25"
        placeholder="Write a calming bedtime story about a panda learning to share…"
      />
    </label>

    <div v-if="result" class="mt-4">
      <div class="text-xs uppercase tracking-wide opacity-60 mb-2">Result (markdown)</div>
      <pre class="whitespace-pre-wrap rounded-2xl border border-white/10 bg-black/30 p-4 text-sm overflow-auto">{{ result }}</pre>
    </div>
  </section>
</template>
