<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getSettings, saveSettings, type SettingsPayload } from '../lib/api'

const router = useRouter()

const saving = ref(false)
const savedMsg = ref<string | null>(null)
const errMsg = ref<string | null>(null)

const step = ref<1 | 2 | 3>(1)

// Keep IDs so we UPDATE rather than always INSERT new rows
const childId = ref<string | undefined>(undefined)
const promptId = ref<string | undefined>(undefined)

const childName = ref('')
const ageMonths = ref(36)

const interests = ref<string[]>([])
const sensitivities = ref<string[]>([])

const interestInput = ref('')
const sensitivityInput = ref('')

const tone = ref<'calm' | 'funny' | 'adventurous' | 'cosy'>('calm')
const genre = ref<'bedtime' | 'animals' | 'space' | 'fantasy' | 'everyday'>('bedtime')
const minutes = ref(6)
const complexity = ref<'simple' | 'growing' | 'chaptery'>('growing')

function addChip(list: string[], raw: string) {
  const v = raw.trim()
  if (!v) return
  const norm = v.toLowerCase()
  if (!list.some((x) => x.toLowerCase() === norm)) list.push(v)
}

function removeChip(list: string[], v: string) {
  const i = list.indexOf(v)
  if (i >= 0) list.splice(i, 1)
}

// IMPORTANT: use .value, not the ref objects themselves
const promptRules = computed(() => {
  return {
    tone: tone.value,
    genre: genre.value,
    readingTimeMinutes: minutes.value,
    complexity: complexity.value,
    language: 'en-GB',
    structure: {
      segments: 'short paragraphs',
      perPage: '1-2',
      repetition: 'light',
    },
    constraints: {
      noViolence: true,
      noBullying: true,
      noScare: true,
      avoidTopics: sensitivities.value,
    },
    personalisation: {
      includeNickname: true,
      useInterests: true,
    },
  }
})

async function load() {
  errMsg.value = null
  try {
    const s = await getSettings()

    // Preserve IDs
    childId.value = s.child?.id
    promptId.value = s.prompt?.id

    if (s.child?.name) childName.value = s.child.name
    if (Number.isFinite(s.child?.ageMonths)) ageMonths.value = s.child.ageMonths || ageMonths.value
    interests.value = Array.isArray(s.child?.interests) ? [...s.child.interests] : []
    sensitivities.value = Array.isArray(s.child?.sensitivities) ? [...s.child.sensitivities] : []

    const rules: any = s.prompt?.rules || {}
    if (rules && typeof rules === 'object') {
      // tolerate older shapes if you ever stored { value: "calm" }
      const t = rules.tone?.value ?? rules.tone
      const g = rules.genre?.value ?? rules.genre
      const c = rules.complexity?.value ?? rules.complexity

      if (t) tone.value = t
      if (g) genre.value = g
      if (c) complexity.value = c
      if (rules.readingTimeMinutes != null) minutes.value = Number(rules.readingTimeMinutes)
    }
  } catch {
    // ok in v1 if no rows yet
  }
}

async function persist() {
  savedMsg.value = null
  errMsg.value = null

  if (!childName.value.trim()) {
    errMsg.value = 'Add a nickname/name first.'
    step.value = 1
    return
  }

  const payload: SettingsPayload = {
    child: {
      id: childId.value,
      name: childName.value.trim(),
      ageMonths: Math.max(0, Number(ageMonths.value) || 0),
      interests: interests.value ?? [],
      sensitivities: sensitivities.value ?? [],
    },
    prompt: {
      id: promptId.value,
      name: 'Default prompt v1',
      schemaVersion: 1,
      rules: promptRules.value,
    },
  }

  saving.value = true
  try {
    const saved = await saveSettings(payload)

    // Update IDs in case we inserted new rows
    childId.value = saved.child?.id
    promptId.value = saved.prompt?.id

    savedMsg.value = 'Saved.'
    step.value = 3
  } catch (e: any) {
    errMsg.value = e?.message || 'Save failed.'
  } finally {
    saving.value = false
  }
}

function goLibrary() {
  router.push('/library')
}

onMounted(load)
</script>

<template>
  <div class="min-h-dvh bg-[#0B1724] text-white">
    <div class="mx-auto max-w-xl px-4 py-6">
      <div class="flex items-center justify-between mb-4">
        <h1 class="text-xl font-semibold">Personalise stories</h1>
        <div class="text-xs opacity-70">Step {{ step }} / 3</div>
      </div>

      <div v-if="errMsg" class="mb-3 rounded-xl border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm">
        {{ errMsg }}
      </div>
      <div v-if="savedMsg" class="mb-3 rounded-xl border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-sm">
        {{ savedMsg }}
      </div>

      <!-- Step 1 -->
      <div v-if="step === 1" class="rounded-2xl bg-white/5 border border-white/10 p-4">
        <h2 class="text-base font-medium mb-3">Child basics</h2>

        <label class="block text-sm opacity-80 mb-1">Nickname</label>
        <input
          v-model="childName"
          class="w-full rounded-xl bg-black/30 border border-white/10 px-3 py-2 outline-none"
          placeholder="e.g. Ted"
          autocomplete="off"
        />

        <div class="mt-4">
          <label class="block text-sm opacity-80 mb-1">Age (months)</label>
          <input
            v-model.number="ageMonths"
            type="number"
            min="0"
            max="180"
            class="w-full rounded-xl bg-black/30 border border-white/10 px-3 py-2 outline-none"
          />
          <p class="mt-1 text-xs opacity-60">0 = newborn. 156 = 13 years.</p>
        </div>

        <div class="mt-5 flex justify-end">
          <button
            class="rounded-xl bg-white text-[#0B1724] px-4 py-2 font-medium disabled:opacity-60"
            :disabled="!childName.trim()"
            @click="step = 2"
          >
            Next
          </button>
        </div>
      </div>

      <!-- Step 2 -->
      <div v-else-if="step === 2" class="space-y-4">
        <div class="rounded-2xl bg-white/5 border border-white/10 p-4">
          <h2 class="text-base font-medium mb-3">Interests</h2>

          <div class="flex gap-2">
            <input
              v-model="interestInput"
              class="flex-1 rounded-xl bg-black/30 border border-white/10 px-3 py-2 outline-none"
              placeholder="space, animals, trains…"
              @keydown.enter.prevent="addChip(interests, interestInput); interestInput=''"
            />
            <button
              class="rounded-xl bg-white/10 border border-white/10 px-3 py-2"
              @click="addChip(interests, interestInput); interestInput=''"
            >
              Add
            </button>
          </div>

          <div class="mt-3 flex flex-wrap gap-2">
            <button
              v-for="x in interests"
              :key="x"
              class="rounded-full bg-white/10 border border-white/10 px-3 py-1 text-sm"
              @click="removeChip(interests, x)"
              title="Remove"
            >
              {{ x }} ✕
            </button>
            <div v-if="interests.length === 0" class="text-xs opacity-60">
              Add a few to make stories feel “about them”.
            </div>
          </div>
        </div>

        <div class="rounded-2xl bg-white/5 border border-white/10 p-4">
          <h2 class="text-base font-medium mb-3">Avoid / sensitivities</h2>

          <div class="flex gap-2">
            <input
              v-model="sensitivityInput"
              class="flex-1 rounded-xl bg-black/30 border border-white/10 px-3 py-2 outline-none"
              placeholder="no spiders, no monsters, no bullying…"
              @keydown.enter.prevent="addChip(sensitivities, sensitivityInput); sensitivityInput=''"
            />
            <button
              class="rounded-xl bg-white/10 border border-white/10 px-3 py-2"
              @click="addChip(sensitivities, sensitivityInput); sensitivityInput=''"
            >
              Add
            </button>
          </div>

          <div class="mt-3 flex flex-wrap gap-2">
            <button
              v-for="x in sensitivities"
              :key="x"
              class="rounded-full bg-white/10 border border-white/10 px-3 py-1 text-sm"
              @click="removeChip(sensitivities, x)"
              title="Remove"
            >
              {{ x }} ✕
            </button>
          </div>
        </div>

        <div class="rounded-2xl bg-white/5 border border-white/10 p-4">
          <h2 class="text-base font-medium mb-3">Story style</h2>

          <div class="grid grid-cols-2 gap-3">
            <label class="text-sm opacity-80">
              Tone
              <select v-model="tone" class="mt-1 w-full rounded-xl bg-black/30 border border-white/10 px-3 py-2">
                <option value="calm">Calm</option>
                <option value="cosy">Cosy</option>
                <option value="funny">Funny</option>
                <option value="adventurous">Adventurous</option>
              </select>
            </label>

            <label class="text-sm opacity-80">
              Genre
              <select v-model="genre" class="mt-1 w-full rounded-xl bg-black/30 border border-white/10 px-3 py-2">
                <option value="bedtime">Bedtime</option>
                <option value="animals">Animals</option>
                <option value="space">Space</option>
                <option value="fantasy">Fantasy</option>
                <option value="everyday">Everyday</option>
              </select>
            </label>

            <label class="text-sm opacity-80">
              Minutes
              <input
                v-model.number="minutes"
                type="number"
                min="2"
                max="20"
                class="mt-1 w-full rounded-xl bg-black/30 border border-white/10 px-3 py-2"
              />
            </label>

            <label class="text-sm opacity-80">
              Complexity
              <select
                v-model="complexity"
                class="mt-1 w-full rounded-xl bg-black/30 border border-white/10 px-3 py-2"
              >
                <option value="simple">Simple</option>
                <option value="growing">Growing</option>
                <option value="chaptery">Chaptery</option>
              </select>
            </label>
          </div>
        </div>

        <div class="flex items-center justify-between">
          <button class="rounded-xl bg-white/10 border border-white/10 px-4 py-2" @click="step = 1">
            Back
          </button>
          <button
            class="rounded-xl bg-white text-[#0B1724] px-4 py-2 font-medium disabled:opacity-60"
            :disabled="saving"
            @click="persist"
          >
            {{ saving ? 'Saving…' : 'Save' }}
          </button>
        </div>
      </div>

      <!-- Step 3 -->
      <div v-else class="rounded-2xl bg-white/5 border border-white/10 p-4">
        <h2 class="text-base font-medium mb-2">Done</h2>
        <p class="text-sm opacity-80">
          Stories can now use <strong>{{ childName }}</strong> + their preferences.
        </p>

        <div class="mt-4 flex gap-2">
          <button class="rounded-xl bg-white text-[#0B1724] px-4 py-2 font-medium" @click="goLibrary">
            Go to Library
          </button>
          <button class="rounded-xl bg-white/10 border border-white/10 px-4 py-2" @click="step = 2">
            Edit
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
