<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import ProfileIdentity from './ProfileIdentity.vue'

const props = defineProps<{
  open: boolean
  profileID: string
  profileName: string
  pinValue: string
  busy: boolean
  error: string
}>()

const emit = defineEmits<{
  cancel: []
  submit: []
  'update:pinValue': [value: string]
}>()

const pinInput = ref<HTMLInputElement | null>(null)
const pin = computed({
  get: () => props.pinValue,
  set: (value: string) => emit('update:pinValue', value),
})

function focusPIN(event: Event) {
  event.preventDefault()
  void nextTick(() => pinInput.value?.focus({ preventScroll: true }))
}

function updateOpen(open: boolean) {
  if (!open) emit('cancel')
}

watch(
  () => props.error,
  async (error) => {
    if (error) {
      await nextTick()
      pinInput.value?.focus({ preventScroll: true })
    }
  },
)
</script>

<template>
  <DialogRoot :open="open" :modal="true" @update:open="updateOpen">
    <DialogPortal>
      <DialogOverlay class="profile-pin-dialog__overlay" />
      <DialogContent
        class="profile-pin-dialog"
        @open-auto-focus="focusPIN"
        @pointer-down-outside="$event.preventDefault()"
        @escape-key-down="busy && $event.preventDefault()"
      >
        <div class="profile-pin-dialog__identity" aria-hidden="true">
          <ProfileIdentity :profileID="profileID" />
        </div>
        <div class="profile-pin-dialog__heading">
          <DialogTitle class="profile-pin-dialog__title">
            Enter {{ profileName }}’s PIN
          </DialogTitle>
          <DialogDescription class="profile-pin-dialog__description">
            A four-digit PIN is required to start reading as {{ profileName }}.
          </DialogDescription>
        </div>
        <form class="profile-pin-dialog__form" @submit.prevent="emit('submit')">
          <label for="profile-pin">Four-digit PIN</label>
          <input
            id="profile-pin"
            ref="pinInput"
            v-model="pin"
            type="password"
            inputmode="numeric"
            autocomplete="off"
            pattern="[0-9]{4}"
            minlength="4"
            maxlength="4"
            :disabled="busy"
            required
          />
          <p v-if="error" class="profile-pin-dialog__error" role="alert">
            {{ error }}
          </p>
          <div class="profile-pin-dialog__actions">
            <button type="button" :disabled="busy" @click="emit('cancel')">Cancel</button>
            <button type="submit" :disabled="busy">Continue</button>
          </div>
        </form>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
.profile-pin-dialog__overlay {
  position: fixed;
  z-index: 80;
  inset: 0;
  background: var(--panda-overlay);
}

.profile-pin-dialog {
  position: fixed;
  z-index: 81;
  top: 50%;
  left: 50%;
  display: grid;
  width: min(calc(100% - 2rem), 29rem);
  max-height: calc(100dvh - 2rem);
  grid-template-columns: auto minmax(0, 1fr);
  gap: 1rem 1.2rem;
  overflow-y: auto;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-card);
  padding: clamp(1.1rem, 5vw, 1.6rem);
  background: var(--panda-paper-raised);
  color: var(--panda-ink);
  box-shadow: var(--panda-shadow);
  transform: translate(-50%, -50%);
}

.profile-pin-dialog:focus-visible {
  outline: 3px solid var(--panda-focus);
  outline-offset: 4px;
}

.profile-pin-dialog__identity { --profile-identity-size: 5rem; }

.profile-pin-dialog__heading {
  align-self: center;
}

.profile-pin-dialog__title {
  display: block;
  color: var(--panda-ink);
  font-family: var(--panda-serif);
  font-size: clamp(1.55rem, 6vw, 2.1rem);
  font-weight: 680;
  letter-spacing: -0.04em;
  line-height: 1.08;
}

.profile-pin-dialog__description {
  display: block;
  margin-top: 0.45rem;
  color: var(--panda-muted);
  line-height: 1.5;
}

.profile-pin-dialog__form {
  display: grid;
  grid-column: 1 / -1;
  gap: 0.7rem;
}

.profile-pin-dialog__form label {
  font-weight: 800;
}

.profile-pin-dialog__form input {
  min-height: 2.75rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  padding: 0.6rem 0.75rem;
  background: var(--panda-white);
  color: var(--panda-ink);
  font: inherit;
  font-size: 1.2rem;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.22em;
}

.profile-pin-dialog__error {
  margin: 0;
  border-left: 0.25rem solid var(--panda-danger);
  padding-left: 0.7rem;
  color: var(--panda-danger);
  font-weight: 800;
  line-height: 1.45;
}

.profile-pin-dialog__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.65rem;
  margin-top: 0.15rem;
}

.profile-pin-dialog__actions button {
  min-height: 2.75rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.6rem 0.9rem;
  background: var(--panda-paper-raised);
  color: var(--panda-ink);
  font: inherit;
  font-weight: 800;
}

.profile-pin-dialog__actions button[type='submit'] {
  background: var(--panda-ink);
  color: var(--panda-white);
}

@media (max-width: 34rem) {
  .profile-pin-dialog {
    top: auto;
    bottom: 0;
    left: 0;
    width: 100%;
    max-height: min(42rem, calc(100dvh - 0.75rem));
    border-radius: var(--panda-radius-card) var(--panda-radius-card) 0 0;
    padding:
      1.1rem
      max(1rem, env(safe-area-inset-right))
      max(1.1rem, env(safe-area-inset-bottom))
      max(1rem, env(safe-area-inset-left));
    transform: none;
  }
}

@media (forced-colors: active) {
  .profile-pin-dialog__overlay { background: CanvasText; opacity: 0.65; }
  .profile-pin-dialog { border-color: CanvasText; background: Canvas; color: CanvasText; box-shadow: none; }
  .profile-pin-dialog__form input { border-color: CanvasText; background: Canvas; color: CanvasText; }
  .profile-pin-dialog__actions button { border-color: CanvasText; background: Canvas; color: CanvasText; }
}
</style>
