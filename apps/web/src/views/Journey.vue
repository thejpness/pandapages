<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  classifySettingsRequestFailure,
  getSettings,
  isJsonObject,
  saveSettings,
  type SettingsPayload,
} from '../lib/api'
import { authState } from '../lib/session'
import { navigationDidFail } from '../lib/session-transitions'

const router = useRouter()

const defaultLoadUnavailableMessage =
  'Panda Pages could not load this reading profile. The connection, server or database may be temporarily unavailable.'
const unlockNavigationMessage =
  'Your session ended, but Panda Pages could not open the unlock page. Try again.'
const nameValidationMessage = 'Add a nickname/name first.'

function storedRuleValue(value: unknown): unknown {
  return isJsonObject(value) && value.value != null ? value.value : value
}

const saving = ref(false)
const savedMsg = ref<string | null>(null)
const errMsg = ref<string | null>(null)
const saveFailed = ref(false)
const loading = ref(false)
const loadState = ref<'loading' | 'ready' | 'unavailable'>('loading')
const loadUnavailableMessage = ref(defaultLoadUnavailableMessage)

const step = ref<1 | 2 | 3>(1)

// Keep IDs so we UPDATE rather than always INSERT new rows
const childId = ref<string | undefined>(undefined)
const promptId = ref<string | undefined>(undefined)

const childName = ref('')
const ageMonths = ref(36)
const nicknameInvalid = computed(
  () => errMsg.value === nameValidationMessage && !childName.value.trim(),
)

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

function applySettings(settings: SettingsPayload) {
  childId.value = settings.child.id
  promptId.value = settings.prompt.id
  childName.value = settings.child.name
  ageMonths.value = settings.child.id === undefined ? 36 : settings.child.ageMonths
  interests.value = [...settings.child.interests]
  sensitivities.value = [...settings.child.sensitivities]

  // The prompt half can be absent independently of the child half.
  tone.value = 'calm'
  genre.value = 'bedtime'
  minutes.value = 6
  complexity.value = 'growing'

  const rules = settings.prompt.rules
  // Tolerate older shapes such as { value: "calm" }.
  const t = storedRuleValue(rules.tone)
  const g = storedRuleValue(rules.genre)
  const c = storedRuleValue(rules.complexity)

  if (t === 'calm' || t === 'funny' || t === 'adventurous' || t === 'cosy') {
    tone.value = t
  }
  if (
    g === 'bedtime' ||
    g === 'animals' ||
    g === 'space' ||
    g === 'fantasy' ||
    g === 'everyday'
  ) {
    genre.value = g
  }
  if (c === 'simple' || c === 'growing' || c === 'chaptery') {
    complexity.value = c
  }
  const readingTime = rules.readingTimeMinutes
  if (typeof readingTime === 'number' || typeof readingTime === 'string') {
    const parsed = Number(readingTime)
    if (Number.isFinite(parsed)) minutes.value = parsed
  }
}

async function moveToUnlock(): Promise<boolean> {
  authState.confirmLocked()
  try {
    const result = await router.replace({
      path: '/unlock',
      query: { next: '/journey' },
    })
    return !navigationDidFail(result)
  } catch {
    return false
  }
}

async function load() {
  if (loading.value) return

  loading.value = true
  errMsg.value = null
  savedMsg.value = null
  loadUnavailableMessage.value = defaultLoadUnavailableMessage
  try {
    const settings = await getSettings()
    applySettings(settings)
    loadState.value = 'ready'
  } catch (error) {
    if (classifySettingsRequestFailure(error, 'load') === 'unauthorized') {
      if (!(await moveToUnlock())) {
        loadUnavailableMessage.value = unlockNavigationMessage
        loadState.value = 'unavailable'
      }
      return
    }
    loadState.value = 'unavailable'
  } finally {
    loading.value = false
  }
}

async function persist() {
  if (saving.value || loadState.value !== 'ready') return

  savedMsg.value = null
  errMsg.value = null
  saveFailed.value = false

  if (!childName.value.trim()) {
    errMsg.value = nameValidationMessage
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
    childId.value = saved.child.id
    promptId.value = saved.prompt.id

    savedMsg.value = 'Saved.'
    step.value = 3
  } catch (error) {
    const failure = classifySettingsRequestFailure(error, 'save')
    if (failure === 'unauthorized') {
      if (!(await moveToUnlock())) {
        loadUnavailableMessage.value = unlockNavigationMessage
        loadState.value = 'unavailable'
      }
      return
    }
    saveFailed.value = true
    errMsg.value =
      failure === 'validation'
        ? 'Some reading profile details could not be saved. Check them and try again.'
        : 'Panda Pages could not save the reading profile. Your changes are still here. Try again.'
  } finally {
    saving.value = false
  }
}

function goLibrary() {
  void router.push('/library')
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="journey-shell panda-print-surface">
    <a class="journey-skip" href="#journey-main">Skip to reading profile</a>

    <header class="journey-header">
      <div class="journey-header__inner">
        <button
          class="journey-brand"
          type="button"
          aria-label="Panda Pages Library"
          @click="goLibrary"
        >
          <img src="/logo.png" alt="" aria-hidden="true" />
          <span>
            <strong>Panda Pages</strong>
            <small>Parent settings</small>
          </span>
        </button>

        <button
          class="journey-button journey-button--secondary journey-return"
          type="button"
          @click="goLibrary"
        >
          <span aria-hidden="true">←</span>
          Return to Library
        </button>
      </div>
    </header>

    <main id="journey-main" class="journey-main" tabindex="-1">
      <section
        v-if="loadState === 'loading'"
        class="journey-card journey-state"
        aria-labelledby="journey-loading-title"
      >
        <p class="journey-card__kicker">Parent settings</p>
        <h1 id="journey-loading-title">Reading profile</h1>
        <p role="status">Loading the reading profile…</p>
      </section>

      <section
        v-else-if="loadState === 'unavailable'"
        class="journey-card journey-state"
        aria-labelledby="journey-unavailable-title"
      >
        <p class="journey-card__kicker">Parent settings</p>
        <h1 id="journey-unavailable-title">Reading profile unavailable</h1>
        <p role="alert">{{ loadUnavailableMessage }}</p>
        <button
          class="journey-button journey-button--primary"
          type="button"
          :disabled="loading"
          @click="load"
        >
          {{ loading ? 'Retrying…' : 'Try again' }}
        </button>
      </section>

      <template v-else>
        <section class="journey-intro" aria-labelledby="journey-title">
        <p class="journey-eyebrow">Reading profile</p>
        <div class="journey-title-row">
          <div>
            <h1 id="journey-title">Reading profile</h1>
            <p>
              These parent notes are stored with the profile. They do not change
              the published stories in the library.
            </p>
          </div>
          <p class="journey-step" aria-live="polite">Step {{ step }} of 3</p>
        </div>

        <ol class="journey-progress" aria-label="Reading profile progress">
          <li
            :class="{
              'journey-progress__item--current': step === 1,
              'journey-progress__item--complete': step > 1,
            }"
            :aria-current="step === 1 ? 'step' : undefined"
          >
            <span aria-hidden="true">1</span>
            Basics
          </li>
          <li
            :class="{
              'journey-progress__item--current': step === 2,
              'journey-progress__item--complete': step > 2,
            }"
            :aria-current="step === 2 ? 'step' : undefined"
          >
            <span aria-hidden="true">2</span>
            Preferences
          </li>
          <li
            :class="{ 'journey-progress__item--current': step === 3 }"
            :aria-current="step === 3 ? 'step' : undefined"
          >
            <span aria-hidden="true">3</span>
            Done
          </li>
        </ol>
      </section>

      <div
        v-if="errMsg"
        id="journey-error"
        class="journey-notice journey-notice--error"
        role="alert"
        aria-live="assertive"
      >
        <span aria-hidden="true">!</span>
        <p>{{ errMsg }}</p>
      </div>
      <div
        v-if="savedMsg"
        class="journey-notice journey-notice--success"
        role="status"
        aria-live="polite"
      >
        <span aria-hidden="true">✓</span>
        <p>{{ savedMsg }}</p>
      </div>

      <section
        v-if="step === 1"
        class="journey-card"
        aria-labelledby="journey-basics-title"
      >
        <div class="journey-card__heading">
          <p class="journey-card__kicker">Step one</p>
          <h2 id="journey-basics-title">Child basics</h2>
        </div>

        <div class="journey-field">
          <label for="journey-nickname">Nickname</label>
          <input
            id="journey-nickname"
            v-model="childName"
            placeholder="e.g. Ted"
            autocomplete="off"
            :aria-describedby="nicknameInvalid ? 'journey-error' : undefined"
            :aria-invalid="nicknameInvalid ? 'true' : undefined"
          />
        </div>

        <div class="journey-field">
          <label for="journey-age">Age (months)</label>
          <input
            id="journey-age"
            v-model.number="ageMonths"
            type="number"
            min="0"
            max="180"
            aria-describedby="journey-age-hint"
          />
          <p id="journey-age-hint" class="journey-field__hint">
            0 = newborn. 156 = 13 years.
          </p>
        </div>

        <div class="journey-actions journey-actions--end">
          <button
            class="journey-button journey-button--primary"
            type="button"
            :disabled="!childName.trim()"
            @click="step = 2"
          >
            Next
            <span aria-hidden="true">→</span>
          </button>
        </div>
      </section>

      <div v-else-if="step === 2" class="journey-step-two">
        <section class="journey-card" aria-labelledby="journey-interests-title">
          <div class="journey-card__heading">
            <p class="journey-card__kicker">Keep close</p>
            <h2 id="journey-interests-title">Interests</h2>
          </div>

          <div class="journey-add-row">
            <div class="journey-field">
              <label for="journey-interest">Add an interest</label>
              <input
                id="journey-interest"
                v-model="interestInput"
                placeholder="space, animals, trains…"
                @keydown.enter.prevent="addChip(interests, interestInput); interestInput=''"
              />
            </div>
            <button
              class="journey-button journey-button--secondary journey-add-button"
              type="button"
              @click="addChip(interests, interestInput); interestInput=''"
            >
              Add
            </button>
          </div>

          <div class="journey-chips" aria-label="Saved interests">
            <button
              v-for="x in interests"
              :key="x"
              class="journey-chip"
              type="button"
              :aria-label="`Remove interest ${x}`"
              @click="removeChip(interests, x)"
            >
              {{ x }} <span aria-hidden="true">×</span>
            </button>
            <p v-if="interests.length === 0" class="journey-empty-note">
              Add notes you want to keep with this reading profile.
            </p>
          </div>
        </section>

        <section class="journey-card" aria-labelledby="journey-sensitivities-title">
          <div class="journey-card__heading">
            <p class="journey-card__kicker">Handle gently</p>
            <h2 id="journey-sensitivities-title">Avoid / sensitivities</h2>
          </div>

          <div class="journey-add-row">
            <div class="journey-field">
              <label for="journey-sensitivity">Add a sensitivity</label>
              <input
                id="journey-sensitivity"
                v-model="sensitivityInput"
                placeholder="no spiders, no monsters, no bullying…"
                @keydown.enter.prevent="addChip(sensitivities, sensitivityInput); sensitivityInput=''"
              />
            </div>
            <button
              class="journey-button journey-button--secondary journey-add-button"
              type="button"
              @click="addChip(sensitivities, sensitivityInput); sensitivityInput=''"
            >
              Add
            </button>
          </div>

          <div class="journey-chips" aria-label="Saved sensitivities">
            <button
              v-for="x in sensitivities"
              :key="x"
              class="journey-chip"
              type="button"
              :aria-label="`Remove sensitivity ${x}`"
              @click="removeChip(sensitivities, x)"
            >
              {{ x }} <span aria-hidden="true">×</span>
            </button>
            <p v-if="sensitivities.length === 0" class="journey-empty-note">
              No sensitivities have been added.
            </p>
          </div>
        </section>

        <section class="journey-card" aria-labelledby="journey-preferences-title">
          <div class="journey-card__heading">
            <p class="journey-card__kicker">Story rhythm</p>
            <h2 id="journey-preferences-title">Saved reading preferences</h2>
          </div>

          <div class="journey-grid">
            <div class="journey-field">
              <label for="journey-tone">Tone</label>
              <select id="journey-tone" v-model="tone">
                <option value="calm">Calm</option>
                <option value="cosy">Cosy</option>
                <option value="funny">Funny</option>
                <option value="adventurous">Adventurous</option>
              </select>
            </div>

            <div class="journey-field">
              <label for="journey-genre">Genre</label>
              <select id="journey-genre" v-model="genre">
                <option value="bedtime">Bedtime</option>
                <option value="animals">Animals</option>
                <option value="space">Space</option>
                <option value="fantasy">Fantasy</option>
                <option value="everyday">Everyday</option>
              </select>
            </div>

            <div class="journey-field">
              <label for="journey-minutes">Minutes</label>
              <input
                id="journey-minutes"
                v-model.number="minutes"
                type="number"
                min="2"
                max="20"
              />
            </div>

            <div class="journey-field">
              <label for="journey-complexity">Complexity</label>
              <select id="journey-complexity" v-model="complexity">
                <option value="simple">Simple</option>
                <option value="growing">Growing</option>
                <option value="chaptery">Chaptery</option>
              </select>
            </div>
          </div>
        </section>

        <div class="journey-actions journey-actions--between">
          <button
            class="journey-button journey-button--secondary"
            type="button"
            @click="step = 1"
          >
            <span aria-hidden="true">←</span>
            Back
          </button>
          <button
            class="journey-button journey-button--primary"
            type="button"
            :disabled="saving"
            @click="persist"
          >
            {{ saving ? 'Saving…' : saveFailed ? 'Try saving again' : 'Save' }}
          </button>
        </div>
      </div>

      <section v-else class="journey-card journey-card--done" aria-labelledby="journey-done-title">
        <div class="journey-done-mark" aria-hidden="true">✓</div>
        <div>
          <p class="journey-card__kicker">Profile saved</p>
          <h2 id="journey-done-title">Done</h2>
          <p>
            The reading profile for <strong>{{ childName }}</strong> has been
            saved. It does not change the published stories in the library.
          </p>
        </div>

        <div class="journey-actions">
          <button
            class="journey-button journey-button--primary"
            type="button"
            @click="goLibrary"
          >
            Go to Library
          </button>
          <button
            class="journey-button journey-button--secondary"
            type="button"
            @click="step = 2"
          >
            Edit
          </button>
        </div>
        </section>
      </template>
    </main>
  </div>
</template>

<style scoped>
.journey-shell {
  min-height: 100dvh;
  overflow-x: clip;
  background: var(--panda-paper);
  color: var(--panda-ink);
  color-scheme: light;
  font-family: var(--panda-sans);
}

.journey-skip {
  position: fixed;
  z-index: 100;
  top: 0.75rem;
  left: 0.75rem;
  transform: translateY(-180%);
  border: 2px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.65rem 0.85rem;
  background: var(--panda-white);
  color: var(--panda-ink);
  font-weight: 800;
}

.journey-skip:focus {
  transform: translateY(0);
}

.journey-header {
  border-bottom: 1px solid var(--panda-line-strong);
  background: color-mix(in srgb, var(--panda-paper-raised) 94%, transparent);
  box-shadow: 0 0.2rem 0.8rem color-mix(in srgb, var(--panda-ink) 5%, transparent);
}

.journey-header__inner {
  display: flex;
  width: min(58rem, 100%);
  min-height: 4.5rem;
  margin-inline: auto;
  padding: var(--panda-safe-top) var(--panda-safe-right) 1rem var(--panda-safe-left);
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.journey-brand {
  display: flex;
  min-width: 0;
  min-height: 2.75rem;
  align-items: center;
  gap: 0.7rem;
  border: 0;
  padding: 0;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.journey-brand img {
  width: 2.8rem;
  height: 2.8rem;
  flex: 0 0 auto;
  object-fit: contain;
}

.journey-brand span {
  display: grid;
  min-width: 0;
}

.journey-brand strong {
  font-family: var(--panda-serif);
  font-size: 1.05rem;
  font-weight: 780;
  letter-spacing: -0.025em;
}

.journey-brand small {
  color: var(--panda-muted);
  font-size: 0.72rem;
  font-weight: 750;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.journey-main {
  width: min(48rem, 100%);
  margin-inline: auto;
  padding: clamp(1.75rem, 5vw, 3.5rem) var(--panda-safe-right) var(--panda-safe-bottom) var(--panda-safe-left);
}

.journey-main:focus {
  outline: none;
}

.journey-state {
  display: grid;
  justify-items: start;
  gap: 1rem;
}

.journey-state h1 {
  margin: 0;
  font-family: var(--panda-serif);
  font-size: clamp(1.8rem, 7vw, 2.8rem);
  line-height: 1;
}

.journey-state p:not(.journey-card__kicker) {
  max-width: 38rem;
  margin: 0;
  color: var(--panda-soft-ink);
  line-height: 1.55;
}

.journey-intro {
  margin-bottom: 1.5rem;
}

.journey-eyebrow,
.journey-card__kicker {
  margin: 0;
  color: var(--panda-muted);
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0.11em;
  text-transform: uppercase;
}

.journey-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1.5rem;
}

.journey-title-row > div {
  min-width: 0;
}

.journey-title-row h1 {
  max-width: 14ch;
  margin: 0.2rem 0 0.65rem;
  font-family: var(--panda-serif);
  font-size: clamp(2rem, 8vw, 3.35rem);
  font-weight: 720;
  letter-spacing: -0.055em;
  line-height: 0.98;
  overflow-wrap: anywhere;
}

.journey-title-row p:not(.journey-step) {
  max-width: 42rem;
  margin: 0;
  color: var(--panda-soft-ink);
  font-size: 1rem;
  line-height: 1.55;
}

.journey-step {
  flex: 0 0 auto;
  margin: 0.4rem 0 0;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-pill);
  padding: 0.45rem 0.7rem;
  background: var(--panda-paper-raised);
  color: var(--panda-soft-ink);
  font-size: 0.78rem;
  font-weight: 800;
}

.journey-progress {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin: 1.35rem 0 0;
  padding: 0;
  list-style: none;
}

.journey-progress li {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.45rem;
  border-top: 2px solid var(--panda-line-strong);
  padding: 0.7rem 0.4rem 0 0;
  color: var(--panda-muted);
  font-size: 0.78rem;
  font-weight: 750;
  overflow-wrap: anywhere;
}

.journey-progress li + li {
  padding-left: 0.4rem;
}

.journey-progress li span {
  display: grid;
  width: 1.45rem;
  height: 1.45rem;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid var(--panda-line-strong);
  border-radius: 50%;
  background: var(--panda-paper-raised);
  font-size: 0.68rem;
}

.journey-progress .journey-progress__item--current,
.journey-progress .journey-progress__item--complete {
  border-color: var(--panda-ink);
  color: var(--panda-ink);
}

.journey-progress .journey-progress__item--current span,
.journey-progress .journey-progress__item--complete span {
  border-color: var(--panda-ink);
  background: var(--panda-ink);
  color: var(--panda-paper-raised);
}

.journey-card {
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-card);
  padding: clamp(1rem, 4vw, 1.5rem);
  background: var(--panda-paper-raised);
  box-shadow: var(--panda-shadow-soft);
}

.journey-card__heading {
  margin-bottom: 1.15rem;
}

.journey-card h2 {
  margin: 0.18rem 0 0;
  font-family: var(--panda-serif);
  font-size: 1.35rem;
  font-weight: 720;
  letter-spacing: -0.025em;
  line-height: 1.15;
  overflow-wrap: anywhere;
}

.journey-field {
  display: grid;
  min-width: 0;
  gap: 0.4rem;
}

.journey-field + .journey-field {
  margin-top: 1rem;
}

.journey-field label {
  color: var(--panda-soft-ink);
  font-size: 0.86rem;
  font-weight: 800;
}

.journey-field input,
.journey-field select {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  min-height: 2.85rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  padding: 0.65rem 0.75rem;
  background: var(--panda-white);
  color: var(--panda-ink);
  font: inherit;
}

.journey-field input::placeholder {
  color: var(--panda-muted);
  opacity: 0.82;
}

.journey-field__hint,
.journey-empty-note {
  margin: 0;
  color: var(--panda-muted);
  font-size: 0.76rem;
  line-height: 1.4;
}

.journey-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.7rem;
  margin-top: 1.3rem;
}

.journey-actions--end {
  justify-content: flex-end;
}

.journey-actions--between {
  justify-content: space-between;
}

.journey-button {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  gap: 0.45rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-pill);
  padding: 0.65rem 1rem;
  font: inherit;
  font-weight: 800;
  line-height: 1.1;
  cursor: pointer;
}

.journey-button--primary {
  background: var(--panda-ink);
  color: var(--panda-paper-raised);
}

.journey-button--secondary {
  background: var(--panda-paper-raised);
  color: var(--panda-ink);
}

.journey-button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.journey-step-two {
  display: grid;
  gap: 1rem;
}

.journey-add-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 0.65rem;
}

.journey-add-button {
  border-radius: var(--panda-radius-compact);
}

.journey-chips {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 0.55rem;
  margin-top: 0.85rem;
}

.journey-chip {
  min-height: 2.25rem;
  max-width: 100%;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-pill);
  padding: 0.4rem 0.7rem;
  background: var(--panda-mist);
  color: var(--panda-ink);
  font: inherit;
  font-size: 0.82rem;
  font-weight: 750;
  overflow-wrap: anywhere;
  cursor: pointer;
}

.journey-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 12rem), 1fr));
  gap: 1rem;
}

.journey-grid .journey-field + .journey-field {
  margin-top: 0;
}

.journey-notice {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 0.65rem;
  align-items: start;
  margin: 0 0 1rem;
  border: 1px solid currentColor;
  border-radius: var(--panda-radius-compact);
  padding: 0.8rem 0.9rem;
  font-weight: 700;
}

.journey-notice > span {
  display: grid;
  width: 1.45rem;
  height: 1.45rem;
  place-items: center;
  border: 1px solid currentColor;
  border-radius: 50%;
  font-size: 0.78rem;
}

.journey-notice p {
  margin: 0.08rem 0 0;
  overflow-wrap: anywhere;
}

.journey-notice--error {
  background: var(--panda-danger-surface);
  color: var(--panda-danger);
}

.journey-notice--success {
  background: var(--panda-success-surface);
  color: var(--panda-success);
}

.journey-card--done {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: start;
  gap: 1rem;
}

.journey-card--done p:not(.journey-card__kicker) {
  margin: 0.65rem 0 0;
  color: var(--panda-soft-ink);
  line-height: 1.55;
}

.journey-card--done .journey-actions {
  grid-column: 1 / -1;
}

.journey-done-mark {
  display: grid;
  width: 2.8rem;
  height: 2.8rem;
  place-items: center;
  border: 1px solid var(--panda-success);
  border-radius: 50%;
  background: var(--panda-success-surface);
  color: var(--panda-success);
  font-size: 1.15rem;
  font-weight: 900;
}

.journey-shell :where(button, input, select, a):focus-visible {
  outline: 3px solid var(--panda-focus);
  outline-offset: 3px;
}

@media (max-width: 38rem) {
  .journey-header__inner,
  .journey-title-row,
  .journey-actions--between {
    align-items: stretch;
  }

  .journey-header__inner {
    flex-wrap: wrap;
  }

  .journey-return {
    margin-left: auto;
  }

  .journey-title-row {
    flex-direction: column;
    gap: 0.7rem;
  }

  .journey-step {
    align-self: flex-start;
    margin-top: 0;
  }

  .journey-actions--between {
    flex-direction: column-reverse;
  }

  .journey-actions--between .journey-button {
    width: 100%;
  }
}

@media (max-width: 26rem) {
  .journey-header__inner {
    gap: 0.65rem;
  }

  .journey-return {
    width: 100%;
  }

  .journey-add-row {
    grid-template-columns: minmax(0, 1fr);
  }

  .journey-add-button {
    width: 100%;
  }

  .journey-progress li {
    align-items: flex-start;
    flex-direction: column;
  }
}

@media (max-height: 32rem) {
  .journey-header__inner {
    min-height: 0;
    padding-top: max(0.65rem, env(safe-area-inset-top));
    padding-bottom: 0.65rem;
  }

  .journey-main {
    padding-top: 1.25rem;
  }
}

@media (forced-colors: active) {
  .journey-progress .journey-progress__item--current span,
  .journey-progress .journey-progress__item--complete span,
  .journey-button--primary {
    background: ButtonText;
    color: ButtonFace;
  }
}
</style>
