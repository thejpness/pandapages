<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import PandaAuthShell from "../components/app/PandaAuthShell.vue";
import {
  createReaderProfile,
  deleteReaderProfile,
  getAPIErrorStatus,
  listReaderProfiles,
  removeReaderProfilePIN,
  renameReaderProfile,
  setReaderProfilePIN,
  type ReaderProfile,
  verifyReaderProfilePIN,
} from "../lib/api";
import { currentAccountContext } from "../lib/account-context";
import {
  clearSelectedReaderProfile,
  reconcileReaderProfileSelection,
  selectReaderProfile,
  selectedReaderProfileID,
} from "../lib/reader-profile-selection";
import { enterChildMode, leaveChildMode } from "../lib/reader-mode";

const route = useRoute();
const router = useRouter();
const profiles = ref<readonly ReaderProfile[]>([]);
const selectedID = ref<string | null>(null);
const loading = ref(true);
const busy = ref(false);
const errorMessage = ref("");
const newName = ref("");
const editingID = ref<string | null>(null);
const editingName = ref("");
const deleteTarget = ref<ReaderProfile | null>(null);
const deleteConfirm = ref<HTMLButtonElement | null>(null);
const pinTarget = ref<ReaderProfile | null>(null);
const pinValue = ref("");
const pinAction = ref<"set" | "verify" | null>(null);
const removePINTarget = ref<ReaderProfile | null>(null);
const pinError = ref("");
const unavailable = computed(() => route.query.unavailable === '1');

function destination(): string {
  const next = route.query.next;
  return typeof next === "string" && /^\/(?!\/)/.test(next)
    ? next
    : "/library";
}

function messageFor(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  return "Profiles could not be updated. Please try again.";
}

function restoreSelection(): void {
  const selected = reconcileReaderProfileSelection(
    selectedReaderProfileID(),
    profiles.value,
  );
  if (selected === null) {
    clearSelectedReaderProfile();
    selectedID.value = null;
    return;
  }
  selectReaderProfile(selected.id);
  selectedID.value = selected.id;
}

async function refresh(): Promise<void> {
  const account = await currentAccountContext();
  profiles.value = await listReaderProfiles(account);
  restoreSelection();
}

async function choose(profile: ReaderProfile): Promise<void> {
  selectReaderProfile(profile.id);
  selectedID.value = profile.id;
  if (profile.pinEnabled) {
    pinTarget.value = profile;
    pinAction.value = "verify";
    pinValue.value = "";
    pinError.value = "";
    return;
  }
  enterChildMode(profile.id);
  await router.replace(destination());
}

function beginSetPIN(profile: ReaderProfile): void {
  pinTarget.value = profile;
  pinAction.value = "set";
  pinValue.value = "";
  pinError.value = "";
}

function closePINDialog(): void {
  pinTarget.value = null;
  pinAction.value = null;
  pinValue.value = "";
  pinError.value = "";
}

async function submitPIN(): Promise<void> {
  const profile = pinTarget.value;
  if (!profile || !pinAction.value || busy.value) return;
  busy.value = true;
  pinError.value = "";
  try {
    if (pinAction.value === "set") {
      await setReaderProfilePIN(profile.id, pinValue.value);
      closePINDialog();
      await refresh();
    } else {
      await verifyReaderProfilePIN(profile.id, pinValue.value);
      closePINDialog();
      enterChildMode(profile.id);
      await router.replace(destination());
    }
  } catch (error) {
    pinError.value = getAPIErrorStatus(error) === 429
      ? "Too many tries. Please wait before trying again."
      : pinAction.value === "verify"
        ? "That PIN is not right."
        : messageFor(error);
  } finally {
    busy.value = false;
  }
}

async function removePIN(): Promise<void> {
  const profile = removePINTarget.value;
  if (!profile || busy.value) return;
  busy.value = true;
  errorMessage.value = "";
  try {
    await removeReaderProfilePIN(profile.id);
    removePINTarget.value = null;
    await refresh();
  } catch (error) {
    errorMessage.value = messageFor(error);
  } finally {
    busy.value = false;
  }
}

async function createProfile(): Promise<void> {
  if (busy.value) return;
  busy.value = true;
  errorMessage.value = "";
  try {
    const created = await createReaderProfile(newName.value);
    newName.value = "";
    await refresh();
    await choose(created);
  } catch (error) {
    errorMessage.value = messageFor(error);
  } finally {
    busy.value = false;
  }
}

function startRename(profile: ReaderProfile): void {
  editingID.value = profile.id;
  editingName.value = profile.name;
  errorMessage.value = "";
}

async function renameProfile(profile: ReaderProfile): Promise<void> {
  if (busy.value) return;
  busy.value = true;
  errorMessage.value = "";
  try {
    await renameReaderProfile(profile.id, editingName.value);
    editingID.value = null;
    await refresh();
  } catch (error) {
    errorMessage.value = messageFor(error);
  } finally {
    busy.value = false;
  }
}

async function confirmDelete(profile: ReaderProfile): Promise<void> {
  deleteTarget.value = profile;
  await nextTick();
  deleteConfirm.value?.focus();
}

async function deleteProfile(): Promise<void> {
  const profile = deleteTarget.value;
  if (!profile || busy.value) return;
  busy.value = true;
  errorMessage.value = "";
  try {
    await deleteReaderProfile(profile.id);
    if (selectedID.value === profile.id) {
      clearSelectedReaderProfile();
      selectedID.value = null;
      leaveChildMode();
    }
    deleteTarget.value = null;
    await refresh();
  } catch (error) {
    errorMessage.value = messageFor(error);
  } finally {
    busy.value = false;
  }
}

onMounted(async () => {
  try {
    await refresh();
  } catch (error) {
    errorMessage.value = messageFor(error);
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <PandaAuthShell
    eyebrow="Panda Pages"
    title="Who’s reading?"
    description="Choose a reader, or add someone new."
  >
    <p v-if="loading" role="status">Loading readers…</p>
    <p v-else-if="errorMessage" class="error" role="alert">
      {{ errorMessage }}
    </p>

    <div v-if="!loading" class="profiles">
      <p v-if="unavailable" class="notice" role="status">
        Readers could not be checked just now. Choose one when the connection is available.
      </p>
      <p v-if="profiles.length === 0" class="empty">
        Add the first reader for this account.
      </p>
      <ul v-else aria-label="Readers">
        <li v-for="profile in profiles" :key="profile.id">
          <template v-if="editingID === profile.id">
            <form class="rename" @submit.prevent="renameProfile(profile)">
              <label :for="`profile-name-${profile.id}`">Reader name</label>
              <input
                :id="`profile-name-${profile.id}`"
                v-model="editingName"
                :disabled="busy"
                maxlength="80"
                required
              />
              <div class="actions">
                <button type="submit" :disabled="busy">Save name</button>
                <button type="button" :disabled="busy" @click="editingID = null">
                  Cancel
                </button>
              </div>
            </form>
          </template>
          <template v-else>
            <button
              type="button"
              class="choose"
              :aria-pressed="selectedID === profile.id"
              @click="choose(profile)"
            >
              <span>{{ profile.name }}</span>
              <small v-if="profile.pinEnabled">PIN protected</small>
              <small v-else-if="selectedID === profile.id">Selected</small>
            </button>
            <div class="actions">
              <button type="button" @click="startRename(profile)">Rename</button>
              <button type="button" @click="beginSetPIN(profile)">
                {{ profile.pinEnabled ? "Change PIN" : "Set PIN" }}
              </button>
              <button
                v-if="profile.pinEnabled"
                type="button"
                @click="removePINTarget = profile"
              >
                Remove PIN
              </button>
              <button type="button" class="danger" @click="confirmDelete(profile)">
                Delete
              </button>
            </div>
          </template>
        </li>
      </ul>

      <form class="create" @submit.prevent="createProfile">
        <label for="new-profile-name">New reader name</label>
        <div>
          <input
            id="new-profile-name"
            v-model="newName"
            :disabled="busy"
            maxlength="80"
            required
          />
          <button type="submit" :disabled="busy">Add reader</button>
        </div>
      </form>
    </div>

    <section
      v-if="deleteTarget"
      class="dialog"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="delete-reader-title"
    >
      <h2 id="delete-reader-title">Delete {{ deleteTarget.name }}?</h2>
      <p>This removes this reader from the account. It cannot be undone.</p>
      <div class="actions">
        <button type="button" :disabled="busy" @click="deleteTarget = null">
          Cancel
        </button>
        <button
          ref="deleteConfirm"
          type="button"
          class="danger"
          :disabled="busy"
          @click="deleteProfile"
        >
          Delete reader
        </button>
      </div>
    </section>

    <section
      v-if="pinTarget && pinAction"
      class="dialog"
      role="dialog"
      aria-modal="true"
      aria-labelledby="profile-pin-title"
    >
      <h2 id="profile-pin-title">
        {{ pinAction === "verify" ? `Enter ${pinTarget.name}’s PIN` : `Set a PIN for ${pinTarget.name}` }}
      </h2>
      <p v-if="pinAction === 'set'">Use four numbers. You can change or remove it later.</p>
      <form class="rename" @submit.prevent="submitPIN">
        <label for="profile-pin">Four-digit PIN</label>
        <input
          id="profile-pin"
          v-model="pinValue"
          type="password"
          inputmode="numeric"
          autocomplete="off"
          pattern="[0-9]{4}"
          minlength="4"
          maxlength="4"
          :disabled="busy"
          required
        />
        <p v-if="pinError" class="error" role="alert">{{ pinError }}</p>
        <div class="actions">
          <button type="button" :disabled="busy" @click="closePINDialog">Cancel</button>
          <button type="submit" :disabled="busy">
            {{ pinAction === "verify" ? "Continue" : "Save PIN" }}
          </button>
        </div>
      </form>
    </section>

    <section
      v-if="removePINTarget"
      class="dialog"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="remove-pin-title"
    >
      <h2 id="remove-pin-title">Remove {{ removePINTarget.name }}’s PIN?</h2>
      <p>Anyone using this account can enter this reader without a PIN.</p>
      <div class="actions">
        <button type="button" :disabled="busy" @click="removePINTarget = null">Cancel</button>
        <button type="button" class="danger" :disabled="busy" @click="removePIN">Remove PIN</button>
      </div>
    </section>
  </PandaAuthShell>
</template>

<style scoped>
.profiles,
.rename,
.create,
.dialog {
  display: grid;
  gap: 1rem;
}

.empty,
.notice,
.error {
  margin: 0;
}

.notice {
  color: var(--panda-muted);
}

.error {
  color: var(--panda-danger, #8b1e1e);
  font-weight: 700;
}

ul {
  display: grid;
  gap: 0.75rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

li,
.dialog {
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  padding: 0.9rem;
}

li {
  display: grid;
  gap: 0.7rem;
}

.choose {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
  width: 100%;
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--panda-ink);
  text-align: left;
  font: inherit;
  font-size: 1.1rem;
  font-weight: 750;
  cursor: pointer;
}

.choose small {
  color: var(--panda-muted);
  font-size: 0.8rem;
}

.actions,
.create > div {
  display: flex;
  flex-wrap: wrap;
  gap: 0.65rem;
}

label {
  font-weight: 700;
}

input {
  min-height: 2.7rem;
  flex: 1 1 12rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  padding: 0.55rem 0.7rem;
  font: inherit;
}

button {
  min-height: 2.7rem;
  border: 1px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.55rem 0.85rem;
  background: var(--panda-paper-raised);
  color: var(--panda-ink);
  font: inherit;
  font-weight: 750;
  cursor: pointer;
}

button:focus-visible,
input:focus-visible {
  outline: 3px solid var(--panda-focus, #1f6feb);
  outline-offset: 2px;
}

button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.danger {
  color: var(--panda-danger, #8b1e1e);
}

.dialog h2,
.dialog p {
  margin: 0;
}
</style>
