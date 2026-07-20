<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PandaAuthShell from '../components/app/PandaAuthShell.vue'
import { getAPIErrorStatus, unlock } from '../lib/api'
import { haptic } from '../lib/haptics'
import { authState } from '../lib/session'
import { safeNextPath } from '../lib/session-navigation'
import { navigationDidFail } from '../lib/session-transitions'

const router = useRouter()
const route = useRoute()

const code = ref('')
const err = ref('')
const busy = ref(false)
const shake = ref(false)
const otpEl = ref<HTMLInputElement | null>(null)

function onlyDigits6(v: string) {
  return v.replace(/\D/g, '').slice(0, 6)
}

const digits = computed(() => {
  const s = code.value
  return Array.from({ length: 6 }, (_, i) => s[i] ?? '')
})

const canSubmit = computed(() => code.value.length === 6 && !busy.value)

function focusOtp() {
  // Don't autofocus on mount; only focus on intent (tap / key)
  otpEl.value?.focus({ preventScroll: true })
}

function setError(message: string) {
  err.value = message
  shake.value = true
  window.setTimeout(() => (shake.value = false), 450)
}

function clearAll() {
  if (busy.value) return
  haptic('select')
  err.value = ''
  code.value = ''
  focusOtp()
}

function backspace() {
  if (busy.value) return
  haptic('select')
  err.value = ''
  code.value = code.value.slice(0, -1)
  focusOtp()
}

async function submit() {
  if (busy.value) return

  err.value = ''
  if (code.value.length !== 6) {
    haptic('heavy')
    setError('Enter 6 digits')
    return
  }

  haptic('select')
  busy.value = true

  try {
    await unlock(code.value)
    authState.confirmUnlocked()
  } catch (error) {
    haptic('heavy')
    if (getAPIErrorStatus(error) === 401) {
      setError('Wrong passcode')

      // After the shake finishes, clear so the next attempt is quick.
      window.setTimeout(() => {
        if (!busy.value) code.value = ''
      }, 480)
    } else {
      setError('Could not unlock Panda Pages. Try again.')
    }
    busy.value = false
    return
  }

  haptic('medium')
  try {
    const result = await router.replace(safeNextPath(route.query.next))
    if (navigationDidFail(result)) {
      setError('Unlocked, but Panda Pages could not open the next page. Try again.')
    }
  } catch {
    setError('Unlocked, but Panda Pages could not open the next page. Try again.')
  } finally {
    busy.value = false
  }
}

function maybeAutoSubmit() {
  // centralised auto-submit gate
  if (!canSubmit.value) return
  void submit()
}

function pressDigit(d: string) {
  if (busy.value) return
  focusOtp()
  if (code.value.length >= 6) return

  haptic('light')
  err.value = ''
  code.value = `${code.value}${d}`

  // Auto-submit immediately on 6th digit
  if (code.value.length === 6) maybeAutoSubmit()
}

// Keep paste / OTP autofill clean and still auto-submit
watch(code, (v) => {
  const cleaned = onlyDigits6(v)
  if (cleaned !== v) code.value = cleaned
  if (cleaned.length === 6) maybeAutoSubmit()
})

function onKeydown(e: KeyboardEvent) {
  if (busy.value) return

  // Let normal browser shortcuts through
  if (e.metaKey || e.ctrlKey || e.altKey) return

  const k = e.key
  if (k >= '0' && k <= '9') {
    e.preventDefault()
    pressDigit(k)
    return
  }
  if (k === 'Backspace') {
    e.preventDefault()
    backspace()
    return
  }
  if (k === 'Escape') {
    e.preventDefault()
    clearAll()
    return
  }
  if (k === 'Enter') {
    if (code.value.length === 6) {
      e.preventDefault()
      void submit()
    }
  }
}

onMounted(() => {
  // Keypad-first UX; don't autofocus
  window.addEventListener('keydown', onKeydown, { passive: false })
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})

const keypad = [
  ['1', '2', '3'],
  ['4', '5', '6'],
  ['7', '8', '9'],
  ['clear', '0', 'back'],
]
</script>

<template>
  <PandaAuthShell
    eyebrow="Parent lock"
    title="Unlock Panda Pages"
    description="Enter your secret passcode"
  >
    <div class="unlock-entry">
      <div
        class="unlock-code"
        :class="{ 'unlock-code--shake': shake }"
        :aria-label="`Passcode entry, ${code.length} of 6 digits entered`"
        role="button"
        tabindex="0"
        @click="focusOtp"
        @keydown.enter.prevent="focusOtp"
      >
        <span
          v-for="(digit, index) in digits"
          :key="index"
          class="unlock-code__digit"
          :class="{ 'unlock-code__digit--filled': digit }"
          aria-hidden="true"
        >
          •
        </span>
      </div>

      <label class="unlock-visually-hidden" for="unlock-passcode">Six-digit passcode</label>
      <input
        id="unlock-passcode"
        ref="otpEl"
        v-model="code"
        inputmode="numeric"
        pattern="[0-9]*"
        autocomplete="one-time-code"
        class="unlock-visually-hidden"
        :aria-invalid="err ? 'true' : undefined"
        :aria-describedby="err ? 'unlock-passcode-error' : 'unlock-passcode-help'"
      />

      <p v-if="err" id="unlock-passcode-error" class="unlock-message unlock-message--error" role="alert">
        {{ err }}
      </p>
      <p v-else id="unlock-passcode-help" class="unlock-message">
        Tap the passcode row to paste or use one-time-code autofill, or type with your keyboard.
      </p>

      <div class="unlock-actions">
        <button
          type="button"
          class="unlock-button unlock-button--primary"
          :disabled="busy || code.length !== 6"
          @click="submit"
        >
          {{ busy ? 'Unlocking…' : 'Unlock' }}
        </button>

        <button
          type="button"
          class="unlock-button unlock-button--secondary"
          :disabled="busy || code.length === 0"
          aria-label="Clear code"
          title="Clear"
          @click="clearAll"
        >
          Clear
        </button>
      </div>
    </div>

    <div class="unlock-keypad" role="group" aria-label="Passcode keypad">
      <div class="unlock-keypad__grid">
        <button
          v-for="key in keypad.flat()"
          :key="key"
          type="button"
          class="unlock-key"
          :class="{ 'unlock-key--word': key === 'clear' || key === 'back' }"
          :disabled="busy"
          :aria-label="key === 'clear' ? 'Clear code' : key === 'back' ? 'Backspace' : `Digit ${key}`"
          @click="
            key === 'clear'
              ? clearAll()
              : key === 'back'
                ? backspace()
                : pressDigit(String(key))
          "
        >
          <template v-if="key === 'clear'">Clear</template>
          <template v-else-if="key === 'back'"><span aria-hidden="true">⌫</span> Back</template>
          <template v-else>{{ key }}</template>
        </button>
      </div>

      <div class="unlock-keypad__note">
        <span>Made for tiny readers</span>
        <span class="unlock-keypad__count">6 digits</span>
      </div>
    </div>

    <template #footer>
      <p class="unlock-footer-copy">Keep stories safe from curious paws.</p>
      <a
        class="unlock-footer-link"
        href="https://southcoastapps.co.uk"
        target="_blank"
        rel="noreferrer"
        aria-label="South Coast Apps"
        title="South Coast Apps"
      >
        <span>A South Coast App</span>
        <span aria-hidden="true">·</span>
        <span>southcoastapps.co.uk</span>
      </a>
    </template>
  </PandaAuthShell>
</template>

<style scoped>
.unlock-entry,
.unlock-keypad {
  border: 1px solid var(--panda-line);
  border-radius: var(--panda-radius-compact);
  padding: clamp(0.8rem, 4vw, 1rem);
  background: var(--panda-white);
}

.unlock-code {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: clamp(0.25rem, 2vw, 0.5rem);
  width: 100%;
  cursor: text;
  user-select: none;
}

.unlock-code:focus-visible {
  border-radius: var(--panda-radius-compact);
  outline: 3px solid var(--panda-focus);
  outline-offset: 0.35rem;
}

.unlock-entry:has(#unlock-passcode:focus-visible) .unlock-code {
  outline: 3px solid var(--panda-focus);
  outline-offset: 0.35rem;
}

.unlock-code--shake {
  animation: unlock-shake 420ms ease-in-out;
}

@keyframes unlock-shake {
  0% { transform: translateX(0); }
  20% { transform: translateX(-10px); }
  40% { transform: translateX(10px); }
  60% { transform: translateX(-6px); }
  80% { transform: translateX(6px); }
  100% { transform: translateX(0); }
}

.unlock-code__digit {
  display: grid;
  min-width: 0;
  min-height: 3.5rem;
  place-items: center;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-paper);
  color: color-mix(in srgb, var(--panda-muted) 34%, transparent);
  font-size: 1.35rem;
  font-weight: 800;
}

.unlock-code__digit--filled {
  border-color: var(--panda-ink);
  background: var(--panda-white);
  color: var(--panda-ink);
  box-shadow: inset 0 0 0 1px var(--panda-ink);
}

.unlock-visually-hidden {
  position: absolute !important;
  width: 1px !important;
  height: 1px !important;
  overflow: hidden !important;
  clip: rect(0 0 0 0) !important;
  clip-path: inset(50%) !important;
  white-space: nowrap !important;
}

.unlock-message {
  min-height: 2.5rem;
  margin: 0.75rem 0 0;
  color: var(--panda-muted);
  font-size: 0.82rem;
  line-height: 1.45;
}

.unlock-message--error {
  border: 1px solid color-mix(in srgb, var(--panda-danger) 45%, transparent);
  border-radius: var(--panda-radius-compact);
  padding: 0.55rem 0.7rem;
  background: var(--panda-danger-surface);
  color: var(--panda-danger);
  font-weight: 700;
}

.unlock-actions {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.6rem;
  margin-top: 0.9rem;
}

.unlock-button,
.unlock-key {
  min-height: 2.75rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  font: inherit;
  font-weight: 800;
  cursor: pointer;
}

.unlock-button--primary {
  padding: 0.7rem 1rem;
  background: var(--panda-ink);
  color: var(--panda-paper-raised);
}

.unlock-button--secondary {
  padding: 0.7rem 0.9rem;
  background: var(--panda-paper);
  color: var(--panda-ink);
}

.unlock-button:hover:not(:disabled),
.unlock-key:hover:not(:disabled) {
  box-shadow: inset 0 0 0 2px var(--panda-ink);
}

.unlock-button:active:not(:disabled),
.unlock-key:active:not(:disabled) {
  transform: translateY(1px);
}

.unlock-button:disabled,
.unlock-key:disabled {
  cursor: not-allowed;
  opacity: 0.48;
}

.unlock-keypad {
  margin-top: 0.8rem;
  background: var(--panda-paper);
}

.unlock-keypad__grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.6rem;
}

.unlock-key {
  min-width: 0;
  min-height: 3.3rem;
  padding: 0.45rem;
  background: var(--panda-white);
  color: var(--panda-ink);
  font-size: 1.05rem;
  box-shadow: 0 0.18rem 0 var(--panda-line-strong);
}

.unlock-key--word {
  font-size: 0.78rem;
}

.unlock-keypad__note {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-top: 0.8rem;
  color: var(--panda-muted);
  font-size: 0.72rem;
}

.unlock-keypad__count {
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-pill);
  padding: 0.2rem 0.55rem;
  background: var(--panda-paper-raised);
  color: var(--panda-soft-ink);
  font-weight: 750;
}

.unlock-footer-copy {
  margin: 0;
}

.unlock-footer-link {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  min-height: 2.75rem;
  margin-top: 0.25rem;
  border-radius: var(--panda-radius-pill);
  padding: 0.45rem 0.7rem;
  color: var(--panda-soft-ink);
  font-weight: 700;
  text-decoration: none;
}

.unlock-footer-link:hover {
  text-decoration: underline;
  text-underline-offset: 0.18em;
}

@media (max-width: 24rem) {
  .unlock-actions {
    grid-template-columns: 1fr;
  }
}

@media (prefers-reduced-motion: reduce) {
  .unlock-code--shake {
    animation: none !important;
  }
}

@media (forced-colors: active) {
  .unlock-code__digit,
  .unlock-entry,
  .unlock-keypad,
  .unlock-button,
  .unlock-key {
    border-color: CanvasText;
    background: Canvas;
    color: CanvasText;
    box-shadow: none;
  }

  .unlock-code__digit--filled {
    forced-color-adjust: none;
    background: Highlight;
    color: HighlightText;
  }
}
</style>
