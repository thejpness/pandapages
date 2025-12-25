<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { unlock } from '../lib/api'
import { haptic } from '../lib/haptics'

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
  otpEl.value?.focus({ preventScroll: true } as any)
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
    haptic('medium')
    const next = typeof route.query.next === 'string' ? route.query.next : '/library'
    router.replace(next)
  } catch {
    haptic('heavy')
    setError('Wrong passcode')

    // After the shake finishes, clear so the next attempt is quick.
    window.setTimeout(() => {
      if (!busy.value) code.value = ''
    }, 480)
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
  window.removeEventListener('keydown', onKeydown as any)
})

const keypad = [
  ['1', '2', '3'],
  ['4', '5', '6'],
  ['7', '8', '9'],
  ['clear', '0', 'back'],
]
</script>

<template>
  <div class="min-h-dvh bg-[#0B1724] text-white relative overflow-hidden">
    <!-- Animated panda patch backdrop -->
    <div class="pp-backdrop pointer-events-none absolute inset-0">
      <div class="pp-vignette absolute inset-0"></div>
      <div class="pp-dots absolute inset-0 opacity-[0.35]"></div>

      <div class="pp-patch pp-patch--a"></div>
      <div class="pp-patch pp-patch--b"></div>
      <div class="pp-patch pp-patch--c"></div>
      <div class="pp-patch pp-patch--d"></div>
    </div>

    <div
      class="relative mx-auto flex min-h-dvh max-w-lg flex-col px-4 pt-[calc(1.25rem+env(safe-area-inset-top))] pb-[calc(1.25rem+env(safe-area-inset-bottom))]"
    >
      <!-- Header -->
      <header class="mt-2">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-3">
            <div
              class="grid h-12 w-12 place-items-center rounded-2xl border border-white/10 bg-white/5 shadow-sm overflow-hidden"
            >
              <img
                src="/logo.png"
                alt="Panda Pages"
                class="h-12 w-12"
                loading="lazy"
                decoding="async"
              />
            </div>

            <div>
              <h1 class="text-xl font-semibold leading-tight">Panda Pages</h1>
              <p class="text-sm opacity-80">Enter your secret passcode</p>
            </div>
          </div>

          <div class="text-xs opacity-80 rounded-full border border-white/10 bg-white/5 px-3 py-1">
            Parent lock
          </div>
        </div>
      </header>

      <!-- Card -->
      <main class="mt-6 flex-1">
        <div
          class="relative rounded-3xl border border-white/10 bg-white/5 p-5
                 shadow-[0_0_0_1px_rgba(255,255,255,0.02)_inset]
                 backdrop-blur"
          @pointerdown="focusOtp"
        >
          <div class="pp-watermark pointer-events-none absolute -right-10 -top-10 opacity-[0.12]"></div>

          <!-- Big “bubbles” -->
          <div
            class="mx-auto mt-2 w-full select-none"
            :class="shake ? 'pp-shake' : ''"
            aria-label="Passcode entry"
            role="button"
            tabindex="0"
            @click="focusOtp"
            @keydown.enter.prevent="focusOtp"
          >
            <div class="flex justify-between gap-2">
              <div
                v-for="(d, i) in digits"
                :key="i"
                class="flex h-14 flex-1 items-center justify-center rounded-2xl border border-white/10 bg-black/20 text-xl font-semibold"
                :class="d ? 'ring-2 ring-white/25' : 'opacity-95'"
              >
                <span v-if="d" class="translate-y-px">•</span>
                <span v-else class="text-white/15">•</span>
              </div>
            </div>

            <p v-if="err" class="mt-3 text-sm text-red-300">{{ err }}</p>
            <p v-else class="mt-3 text-sm opacity-70">
              Tip: tap here to paste / use OTP autofill, or type with your keyboard.
            </p>
          </div>

          <!-- Hidden input (paste / OTP autofill) -->
          <input
            ref="otpEl"
            v-model="code"
            inputmode="numeric"
            pattern="[0-9]*"
            autocomplete="one-time-code"
            class="sr-only"
          />

          <!-- Actions -->
          <div class="mt-5 flex gap-2">
            <button
              type="button"
              class="flex-1 rounded-2xl bg-white text-black py-3 font-semibold
                     disabled:opacity-60 active:scale-[0.99] transition"
              :disabled="busy || code.length !== 6"
              @click="submit"
            >
              {{ busy ? 'Unlocking…' : 'Unlock' }}
            </button>

            <button
              type="button"
              class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3 text-sm
                     hover:bg-white/10 active:scale-[0.99] transition disabled:opacity-60"
              :disabled="busy || code.length === 0"
              @click="clearAll"
              aria-label="Clear code"
              title="Clear"
            >
              Clear
            </button>
          </div>
        </div>

        <!-- Keypad -->
        <div class="mt-5 rounded-3xl border border-white/10 bg-white/5 p-4 backdrop-blur">
          <div class="grid grid-cols-3 gap-3">
            <button
              v-for="key in keypad.flat()"
              :key="key"
              type="button"
              class="pp-key h-14 rounded-2xl border border-white/10 bg-black/20
                     text-lg font-semibold transition disabled:opacity-60"
              :class="
                key === 'clear'
                  ? 'text-sm font-medium'
                  : key === 'back'
                    ? 'text-sm font-medium'
                    : ''
              "
              :disabled="busy"
              @click="
                key === 'clear'
                  ? clearAll()
                  : key === 'back'
                    ? backspace()
                    : pressDigit(String(key))
              "
            >
              <span v-if="key === 'clear'">🧼 Clear</span>
              <span v-else-if="key === 'back'">⌫ Back</span>
              <span v-else>{{ key }}</span>
            </button>
          </div>

          <div class="mt-4 flex items-center justify-between text-xs opacity-70">
            <span>Made for tiny readers 🐾</span>
            <span class="rounded-full border border-white/10 bg-white/5 px-2 py-1">6 digits</span>
          </div>
        </div>
      </main>

      <!-- Footer branding -->
      <footer class="mt-6 text-center text-xs opacity-70">
        <div>Keep stories safe from curious paws 🐼</div>
        <a
          class="mt-2 inline-flex items-center justify-center gap-2 rounded-full border border-white/10 bg-white/5 px-3 py-1 hover:bg-white/10 transition"
          href="https://southcoastapps.co.uk"
          target="_blank"
          rel="noreferrer"
          aria-label="South Coast Apps"
          title="South Coast Apps"
        >
          <span class="opacity-90">A South Coast App</span>
          <span class="opacity-60">·</span>
          <span class="opacity-90">southcoastapps.co.uk</span>
        </a>
      </footer>
    </div>
  </div>
</template>

<style scoped>
/* unchanged styles… (keep your existing style block) */
.pp-shake { animation: pp-shake 420ms ease-in-out; }
@keyframes pp-shake {
  0% { transform: translateX(0); }
  20% { transform: translateX(-10px); }
  40% { transform: translateX(10px); }
  60% { transform: translateX(-6px); }
  80% { transform: translateX(6px); }
  100% { transform: translateX(0); }
}

.pp-key {
  transform: translateZ(0);
  box-shadow:
    0 0 0 1px rgba(255,255,255,0.02) inset,
    0 10px 30px rgba(0,0,0,0.25);
}
.pp-key:hover { background-color: rgba(255,255,255,0.08); }
.pp-key:active {
  transform: scale(0.99);
  box-shadow:
    0 0 0 1px rgba(255,255,255,0.05) inset,
    0 6px 18px rgba(0,0,0,0.22);
}

/* rest of your existing CSS kept as-is */
.pp-vignette {
  background: radial-gradient(900px 460px at 50% 10%, rgba(255,255,255,0.06), transparent 65%),
              radial-gradient(900px 520px at 50% 110%, rgba(0,0,0,0.35), transparent 60%);
}

.pp-dots {
  background-image:
    radial-gradient(rgba(255,255,255,0.18) 1px, transparent 1px),
    radial-gradient(rgba(255,255,255,0.10) 1px, transparent 1px);
  background-size: 42px 42px, 64px 64px;
  background-position: 0 0, 20px 10px;
  animation: pp-drift 18s linear infinite;
  mask-image: radial-gradient(closest-side, rgba(0,0,0,1), rgba(0,0,0,0));
  mask-position: 50% 0%;
  mask-size: 140% 100%;
  mask-repeat: no-repeat;
}
@keyframes pp-drift {
  from { transform: translate3d(0, 0, 0); }
  to   { transform: translate3d(0, -120px, 0); }
}

/* Panda patch (geometric) */
.pp-patch,
.pp-watermark {
  position: absolute;
  width: 240px;
  height: 240px;
  border-radius: 48px;
  background:
    radial-gradient(40px 40px at 22% 18%, rgba(0,0,0,0.75), transparent 62%),
    radial-gradient(40px 40px at 78% 18%, rgba(0,0,0,0.75), transparent 62%),
    radial-gradient(140px 120px at 50% 55%, rgba(255,255,255,0.10), transparent 60%),
    linear-gradient(135deg, rgba(255,255,255,0.10), rgba(255,255,255,0.03));
  border: 1px solid rgba(255,255,255,0.08);
  box-shadow:
    0 0 0 1px rgba(0,0,0,0.18) inset,
    0 30px 80px rgba(0,0,0,0.35);
}

.pp-patch::before,
.pp-patch::after,
.pp-watermark::before,
.pp-watermark::after {
  content: '';
  position: absolute;
  width: 72px;
  height: 72px;
  border-radius: 999px;
  background: rgba(0,0,0,0.70);
  top: -22px;
  box-shadow: 0 0 0 1px rgba(255,255,255,0.06) inset;
}
.pp-patch::before,
.pp-watermark::before { left: 22px; }
.pp-patch::after,
.pp-watermark::after { right: 22px; }

.pp-patch--a { left: -60px; top: 90px; opacity: 0.20; transform: rotate(-8deg); animation: pp-float-a 14s ease-in-out infinite; }
.pp-patch--b { right: -90px; top: 40px; opacity: 0.14; transform: rotate(10deg); animation: pp-float-b 16s ease-in-out infinite; }
.pp-patch--c { left: -80px; bottom: -40px; opacity: 0.12; transform: rotate(14deg); animation: pp-float-c 18s ease-in-out infinite; }
.pp-patch--d { right: -70px; bottom: 140px; opacity: 0.10; transform: rotate(-14deg); animation: pp-float-d 20s ease-in-out infinite; }

@keyframes pp-float-a { 0%,100% { transform: translate3d(0,0,0) rotate(-8deg);} 50% { transform: translate3d(14px,-10px,0) rotate(-4deg);} }
@keyframes pp-float-b { 0%,100% { transform: translate3d(0,0,0) rotate(10deg);} 50% { transform: translate3d(-16px,12px,0) rotate(6deg);} }
@keyframes pp-float-c { 0%,100% { transform: translate3d(0,0,0) rotate(14deg);} 50% { transform: translate3d(18px,10px,0) rotate(18deg);} }
@keyframes pp-float-d { 0%,100% { transform: translate3d(0,0,0) rotate(-14deg);} 50% { transform: translate3d(-12px,-14px,0) rotate(-10deg);} }

.pp-watermark { width: 220px; height: 220px; box-shadow: none; border-color: rgba(255,255,255,0.06); }

@media (prefers-reduced-motion: reduce) {
  .pp-dots,
  .pp-patch--a,
  .pp-patch--b,
  .pp-patch--c,
  .pp-patch--d {
    animation: none !important;
  }
}
</style>
