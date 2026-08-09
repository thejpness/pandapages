<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from "reka-ui";
import PandaAuthShell from "../components/app/PandaAuthShell.vue";
import ProfileFields from "../components/profile/ProfileFields.vue";
import ProfileIdentity from "../components/profile/ProfileIdentity.vue";
import { deleteReaderProfile, getAPIErrorStatus, listReaderProfiles, removeReaderProfilePIN, renameReaderProfile, setReaderProfilePIN, type ReaderEditionKey, type ReaderProfile } from "../lib/api";
import { currentAccountContext } from "../lib/account-context";
import { invalidateReaderProfileSession } from "../lib/profile-session";

const route = useRoute();
const router = useRouter();
const profile = ref<ReaderProfile | null>(null);
const name = ref("");
const readingLevel = ref<ReaderEditionKey>("classic");
const loading = ref(true);
const busy = ref(false);
const errorMessage = ref("");
const pinDialog = ref<"set" | "remove" | null>(null);
const pinValue = ref("");
const pinError = ref("");
const deleteOpen = ref(false);
const deleteError = ref("");
const pinInput = ref<HTMLInputElement | null>(null);
const removeCancel = ref<HTMLButtonElement | null>(null);
const deleteCancel = ref<HTMLButtonElement | null>(null);
let returnFocus: HTMLElement | null = null;

const id = () => typeof route.params.profileID === "string" ? route.params.profileID : "";
const message = (error: unknown) => error instanceof Error && error.message ? error.message : "Profile could not be updated. Please try again.";

function rememberTrigger(trigger?: EventTarget | null) {
  returnFocus = trigger instanceof HTMLElement ? trigger : null;
}

async function restoreTriggerFocus() {
  const trigger = returnFocus;
  returnFocus = null;
  await nextTick();
  trigger?.focus({ preventScroll: true });
}

function focusInput(event: Event) {
  event.preventDefault();
  void nextTick(() => pinInput.value?.focus({ preventScroll: true }));
}

function focusRemoveCancel(event: Event) {
  event.preventDefault();
  void nextTick(() => removeCancel.value?.focus({ preventScroll: true }));
}

function focusDeleteCancel(event: Event) {
  event.preventDefault();
  void nextTick(() => deleteCancel.value?.focus({ preventScroll: true }));
}

async function load() {
  const account = await currentAccountContext();
  const found = (await listReaderProfiles(account)).find((candidate) => candidate.id === id());
  if (!found) {
    await router.replace("/profiles/manage");
    return;
  }
  profile.value = found;
  name.value = found.name;
  readingLevel.value = found.readingLevel;
}

async function save() {
  if (!profile.value || busy.value) return;
  busy.value = true;
  errorMessage.value = "";
  try {
    await renameReaderProfile(profile.value.id, name.value, readingLevel.value);
    await router.replace("/profiles/manage");
  } catch (error) {
    errorMessage.value = message(error);
  } finally {
    busy.value = false;
  }
}

function openPIN(kind: "set" | "remove", trigger?: EventTarget | null) {
  rememberTrigger(trigger);
  pinDialog.value = kind;
  pinValue.value = "";
  pinError.value = "";
}

function closePIN(restoreFocus = true) {
  pinDialog.value = null;
  pinValue.value = "";
  pinError.value = "";
  if (restoreFocus) void restoreTriggerFocus();
  else returnFocus = null;
}

async function submitPIN() {
  if (!profile.value || busy.value) return;
  busy.value = true;
  pinError.value = "";
  try {
    await setReaderProfilePIN(profile.value.id, pinValue.value);
    await load();
    closePIN();
  } catch (error) {
    pinError.value = getAPIErrorStatus(error) === 429 ? "Too many tries. Please wait before trying again." : message(error);
  } finally {
    busy.value = false;
  }
}

async function removePIN() {
  if (!profile.value || busy.value) return;
  busy.value = true;
  pinError.value = "";
  try {
    await removeReaderProfilePIN(profile.value.id);
    await load();
    closePIN();
  } catch (error) {
    pinError.value = message(error);
  } finally {
    busy.value = false;
  }
}

function openDelete(trigger?: EventTarget | null) {
  rememberTrigger(trigger);
  deleteError.value = "";
  deleteOpen.value = true;
}

function closeDelete(restoreFocus = true) {
  deleteOpen.value = false;
  deleteError.value = "";
  if (restoreFocus) void restoreTriggerFocus();
  else returnFocus = null;
}

async function deleteProfile() {
  if (!profile.value || busy.value) return;
  busy.value = true;
  deleteError.value = "";
  try {
    await deleteReaderProfile(profile.value.id);
    invalidateReaderProfileSession(profile.value.id);
    closeDelete(false);
    await router.replace("/profiles/manage");
  } catch (error) {
    deleteError.value = message(error);
  } finally {
    busy.value = false;
  }
}

watch(
  () => pinError.value,
  async (error) => {
    if (error && pinDialog.value === "set") {
      await nextTick();
      pinInput.value?.focus({ preventScroll: true });
    }
  },
);

onMounted(async () => {
  try {
    await load();
  } catch (error) {
    errorMessage.value = message(error);
  } finally {
    loading.value = false;
  }
});
</script>
<template>
  <PandaAuthShell eyebrow="Profiles" :title="profile ? 'Edit ' + profile.name : 'Edit profile'" description="Update reader details, access settings, or remove this reader.">
    <p v-if="loading" class="profile-edit__state" role="status">Loading profile…</p>
    <p v-else-if="errorMessage && !profile" class="profile-edit__state profile-edit__state--error" role="alert">{{ errorMessage }}</p>
    <div v-else-if="profile" class="profile-edit">
      <div class="profile-edit__summary">
        <ProfileIdentity class="profile-edit__identity" :profileID="profile.id" />
        <div>
          <p class="profile-edit__summary-label">Editing reader</p>
          <p class="profile-edit__summary-name">{{ profile.name }}</p>
        </div>
      </div>

      <form class="profile-edit__form" @submit.prevent="save">
        <section class="profile-edit__surface" aria-label="Reader details">
          <ProfileFields
            v-model:name="name"
            v-model:reading-level="readingLevel"
            name-id="profile-name"
            level-id="profile-level"
            :disabled="busy"
          />
        </section>
        <p v-if="errorMessage" class="profile-edit__error" role="alert">{{ errorMessage }}</p>
        <div class="profile-edit__actions">
          <button class="profile-edit__button profile-edit__button--quiet" type="button" :disabled="busy" @click="router.push('/profiles/manage')">Cancel</button>
          <button class="profile-edit__button profile-edit__button--primary" type="submit" :disabled="busy">{{ busy ? 'Saving…' : 'Save profile' }}</button>
        </div>
      </form>

      <section class="profile-edit__section" aria-labelledby="profile-pin-heading">
        <div class="profile-edit__section-heading">
          <div>
            <h2 id="profile-pin-heading">PIN and access</h2>
            <p>{{ profile.pinEnabled ? 'This reader needs a PIN to enter.' : 'This reader has no PIN.' }}</p>
          </div>
          <span class="profile-edit__status">{{ profile.pinEnabled ? 'PIN protected' : 'No PIN' }}</span>
        </div>
        <div class="profile-edit__actions">
          <button class="profile-edit__button" type="button" :disabled="busy" @click="openPIN('set', $event.currentTarget)">
            {{ profile.pinEnabled ? 'Change PIN' : 'Set PIN' }}
          </button>
          <button v-if="profile.pinEnabled" class="profile-edit__button profile-edit__button--quiet" type="button" :disabled="busy" @click="openPIN('remove', $event.currentTarget)">Remove PIN</button>
        </div>
      </section>

      <section class="profile-edit__section profile-edit__section--danger" aria-labelledby="delete-profile-heading">
        <div class="profile-edit__section-heading">
          <div>
            <h2 id="delete-profile-heading">Delete profile</h2>
            <p>Remove {{ profile.name }} from this account. This cannot be undone.</p>
          </div>
        </div>
        <button class="profile-edit__button profile-edit__button--danger" type="button" :disabled="busy" @click="openDelete($event.currentTarget)">Delete profile</button>
      </section>
    </div>

    <DialogRoot :open="pinDialog === 'set'" :modal="true" @update:open="!$event && closePIN()">
      <DialogPortal>
        <DialogOverlay class="profile-edit-dialog__overlay" />
        <DialogContent class="profile-edit-dialog" @open-auto-focus="focusInput" @pointer-down-outside="$event.preventDefault()" @escape-key-down="busy && $event.preventDefault()">
          <DialogTitle class="profile-edit-dialog__title">{{ profile?.pinEnabled ? 'Change' : 'Set' }} {{ profile?.name }}’s PIN</DialogTitle>
          <DialogDescription class="profile-edit-dialog__description">
            Use a four-digit PIN to protect this reader’s entry.
          </DialogDescription>
          <form class="profile-edit-dialog__form" @submit.prevent="submitPIN">
            <label for="profile-pin">Four-digit PIN</label>
            <input id="profile-pin" ref="pinInput" v-model="pinValue" type="password" inputmode="numeric" autocomplete="off" pattern="[0-9]{4}" minlength="4" maxlength="4" :disabled="busy" required />
            <p v-if="pinError" class="profile-edit-dialog__error" role="alert">{{ pinError }}</p>
            <div class="profile-edit-dialog__actions">
              <button type="button" :disabled="busy" @click="closePIN()">Cancel</button>
              <button type="submit" :disabled="busy">Save PIN</button>
            </div>
          </form>
        </DialogContent>
      </DialogPortal>
    </DialogRoot>

    <DialogRoot :open="pinDialog === 'remove'" :modal="true" @update:open="!$event && closePIN()">
      <DialogPortal>
        <DialogOverlay class="profile-edit-dialog__overlay" />
        <DialogContent class="profile-edit-dialog" role="alertdialog" @open-auto-focus="focusRemoveCancel" @pointer-down-outside="$event.preventDefault()" @escape-key-down="busy && $event.preventDefault()">
          <DialogTitle class="profile-edit-dialog__title">Remove {{ profile?.name }}’s PIN?</DialogTitle>
          <DialogDescription class="profile-edit-dialog__description">
            Anyone using this account can enter this reader without a PIN.
          </DialogDescription>
          <p v-if="pinError" class="profile-edit-dialog__error" role="alert">{{ pinError }}</p>
          <div class="profile-edit-dialog__actions">
            <button ref="removeCancel" type="button" :disabled="busy" @click="closePIN()">Cancel</button>
            <button class="profile-edit-dialog__button--danger" type="button" :disabled="busy" @click="removePIN">Remove PIN</button>
          </div>
        </DialogContent>
      </DialogPortal>
    </DialogRoot>

    <DialogRoot :open="deleteOpen" :modal="true" @update:open="!$event && closeDelete()">
      <DialogPortal>
        <DialogOverlay class="profile-edit-dialog__overlay" />
        <DialogContent class="profile-edit-dialog" role="alertdialog" @open-auto-focus="focusDeleteCancel" @pointer-down-outside="$event.preventDefault()" @escape-key-down="busy && $event.preventDefault()">
          <DialogTitle class="profile-edit-dialog__title">Delete {{ profile?.name }}?</DialogTitle>
          <DialogDescription class="profile-edit-dialog__description">
            This removes this reader from the account. It cannot be undone.
          </DialogDescription>
          <p v-if="deleteError" class="profile-edit-dialog__error" role="alert">{{ deleteError }}</p>
          <div class="profile-edit-dialog__actions">
            <button ref="deleteCancel" type="button" :disabled="busy" @click="closeDelete()">Cancel</button>
            <button class="profile-edit-dialog__button--danger" type="button" :disabled="busy" @click="deleteProfile">Delete profile</button>
          </div>
        </DialogContent>
      </DialogPortal>
    </DialogRoot>
  </PandaAuthShell>
</template>

<style scoped>
.profile-edit { display: grid; gap: 1.35rem; }
.profile-edit__summary { display: flex; align-items: center; gap: 0.85rem; padding-bottom: 0.2rem; }
.profile-edit__identity { --profile-identity-size: 4rem; }
.profile-edit__summary-label,
.profile-edit__summary-name { margin: 0; }
.profile-edit__summary-label { color: var(--panda-muted); font-size: 0.78rem; font-weight: 800; letter-spacing: 0.07em; text-transform: uppercase; }
.profile-edit__summary-name { margin-top: 0.12rem; font-family: var(--panda-serif); font-size: 1.25rem; font-weight: 680; overflow-wrap: anywhere; }
.profile-edit__form { display: grid; gap: 1rem; }
.profile-edit__surface,
.profile-edit__section { border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-card); padding: clamp(1rem, 4vw, 1.25rem); background: var(--panda-white); box-shadow: var(--panda-shadow-soft); }
.profile-edit__section { display: grid; gap: 1rem; background: var(--panda-paper-raised); box-shadow: none; }
.profile-edit__section--danger { border-color: color-mix(in srgb, var(--panda-danger) 48%, var(--panda-line-strong)); background: var(--panda-danger-surface); }
.profile-edit__section-heading { display: flex; align-items: start; justify-content: space-between; gap: 1rem; }
.profile-edit__section h2 { margin: 0; font-family: var(--panda-serif); font-size: 1.2rem; font-weight: 680; }
.profile-edit__section p { margin: 0.35rem 0 0; color: var(--panda-muted); line-height: 1.5; }
.profile-edit__status { flex: 0 0 auto; border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-pill); padding: 0.28rem 0.55rem; color: var(--panda-soft-ink); font-size: 0.78rem; font-weight: 800; }
.profile-edit__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.65rem; }
.profile-edit__section .profile-edit__actions { justify-content: flex-start; }
.profile-edit__button { min-height: 2.75rem; border: 1px solid var(--panda-ink); border-radius: var(--panda-radius-compact); padding: 0.58rem 0.9rem; background: var(--panda-paper-raised); color: var(--panda-ink); font: inherit; font-weight: 800; }
.profile-edit__button--primary { background: var(--panda-ink); color: var(--panda-white); }
.profile-edit__button--quiet { border-color: var(--panda-line-strong); }
.profile-edit__button--danger { border-color: var(--panda-danger); background: var(--panda-danger); color: var(--panda-white); }
.profile-edit__error,
.profile-edit__state { margin: 0; border: 1px solid var(--panda-danger); border-radius: var(--panda-radius-compact); padding: 0.75rem 0.85rem; background: var(--panda-danger-surface); color: var(--panda-danger); font-weight: 750; line-height: 1.45; }
.profile-edit__state { border-color: var(--panda-line-strong); background: var(--panda-paper-raised); color: var(--panda-muted); }
.profile-edit__state--error { border-color: var(--panda-danger); background: var(--panda-danger-surface); color: var(--panda-danger); }
.profile-edit-dialog__overlay { position: fixed; z-index: 80; inset: 0; background: var(--panda-overlay); }
.profile-edit-dialog { position: fixed; z-index: 81; top: 50%; left: 50%; display: grid; width: min(calc(100% - 2rem), 29rem); max-height: calc(100dvh - 2rem); gap: 0.9rem; overflow-y: auto; border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-card); padding: clamp(1.1rem, 5vw, 1.5rem); background: var(--panda-paper-raised); color: var(--panda-ink); box-shadow: var(--panda-shadow); transform: translate(-50%, -50%); }
.profile-edit-dialog__title { color: var(--panda-ink); font-family: var(--panda-serif); font-size: clamp(1.45rem, 6vw, 1.9rem); font-weight: 680; letter-spacing: -0.035em; line-height: 1.12; }
.profile-edit-dialog__description { color: var(--panda-muted); line-height: 1.5; }
.profile-edit-dialog__form { display: grid; gap: 0.7rem; }
.profile-edit-dialog__form label { font-weight: 800; }
.profile-edit-dialog__form input { min-height: 2.75rem; border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-compact); padding: 0.6rem 0.75rem; background: var(--panda-white); color: var(--panda-ink); font: inherit; font-size: 1.2rem; font-variant-numeric: tabular-nums; letter-spacing: 0.22em; }
.profile-edit-dialog__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.65rem; margin-top: 0.2rem; }
.profile-edit-dialog__actions button { min-height: 2.75rem; border: 1px solid var(--panda-ink); border-radius: var(--panda-radius-compact); padding: 0.58rem 0.9rem; background: var(--panda-paper-raised); color: var(--panda-ink); font: inherit; font-weight: 800; }
.profile-edit-dialog__actions button[type="submit"] { background: var(--panda-ink); color: var(--panda-white); }
.profile-edit-dialog__button--danger { border-color: var(--panda-danger) !important; background: var(--panda-danger) !important; color: var(--panda-white) !important; }
.profile-edit-dialog__error { margin: 0; border-left: 0.25rem solid var(--panda-danger); padding-left: 0.7rem; color: var(--panda-danger); font-weight: 800; line-height: 1.45; }
@media (max-width: 31rem) {
  .profile-edit__section-heading { align-items: stretch; flex-direction: column; }
  .profile-edit__status { width: fit-content; }
  .profile-edit__actions { display: grid; grid-template-columns: 1fr; }
  .profile-edit__button { width: 100%; }
  .profile-edit-dialog { top: auto; bottom: 0; left: 0; width: 100%; max-height: min(42rem, calc(100dvh - 0.75rem)); border-radius: var(--panda-radius-card) var(--panda-radius-card) 0 0; padding: 1.1rem max(1rem, env(safe-area-inset-right)) max(1.1rem, env(safe-area-inset-bottom)) max(1rem, env(safe-area-inset-left)); transform: none; }
  .profile-edit-dialog__actions { display: grid; grid-template-columns: 1fr; }
  .profile-edit-dialog__actions button { width: 100%; }
}

@media (forced-colors: active) {
  .profile-edit__surface, .profile-edit__section, .profile-edit-dialog { border-color: CanvasText; background: Canvas; color: CanvasText; box-shadow: none; }
  .profile-edit__button, .profile-edit-dialog__actions button, .profile-edit-dialog__form input { border-color: CanvasText; background: Canvas; color: CanvasText; }
}
</style>
