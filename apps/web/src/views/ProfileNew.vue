<script setup lang="ts">
import { ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import PandaAuthShell from "../components/app/PandaAuthShell.vue";
import ProfileFields from "../components/profile/ProfileFields.vue";
import { createReaderProfile, type ReaderEditionKey } from "../lib/api";
import { profileCreateReturnDestination } from "../lib/profile-destination";
const route = useRoute(); const router = useRouter(); const name = ref(""); const readingLevel = ref<ReaderEditionKey>("classic"); const busy = ref(false); const errorMessage = ref("");
async function createProfile() { if (busy.value) return; busy.value = true; errorMessage.value = ""; try { await createReaderProfile(name.value, readingLevel.value); await router.replace(profileCreateReturnDestination(route.query.from)); } catch (error) { errorMessage.value = error instanceof Error && error.message ? error.message : "Profile could not be created. Please try again."; } finally { busy.value = false; } }
</script>
<template>
  <PandaAuthShell eyebrow="Profiles" title="Add profile" description="Create a reader without starting a reading session.">
    <form class="profile-form" @submit.prevent="createProfile">
      <div class="profile-form__intro">
        <p>A reader profile keeps each person’s reading level and PIN settings separate.</p>
      </div>
      <div class="profile-form__surface">
        <ProfileFields
          v-model:name="name"
          v-model:reading-level="readingLevel"
          name-id="new-profile-name"
          level-id="new-profile-level"
          :disabled="busy"
        />
      </div>
      <p v-if="errorMessage" class="profile-form__error" role="alert">{{ errorMessage }}</p>
      <div class="profile-form__actions">
        <button class="profile-form__button profile-form__button--quiet" type="button" :disabled="busy" @click="router.push(profileCreateReturnDestination(route.query.from))">
          Cancel
        </button>
        <button class="profile-form__button profile-form__button--primary" type="submit" :disabled="busy">
          {{ busy ? 'Creating…' : 'Create profile' }}
        </button>
      </div>
    </form>
  </PandaAuthShell>
</template>

<style scoped>
.profile-form { display: grid; gap: 1rem; }
.profile-form__intro { border-left: 0.25rem solid var(--panda-success); padding-left: 0.8rem; color: var(--panda-soft-ink); line-height: 1.5; }
.profile-form__intro p { margin: 0; }
.profile-form__surface { border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-card); padding: clamp(1rem, 4vw, 1.25rem); background: var(--panda-white); box-shadow: var(--panda-shadow-soft); }
.profile-form__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 0.65rem; }
.profile-form__button { min-height: 2.75rem; border: 1px solid var(--panda-ink); border-radius: var(--panda-radius-compact); padding: 0.58rem 0.9rem; background: var(--panda-paper-raised); color: var(--panda-ink); font: inherit; font-weight: 800; }
.profile-form__button--primary { background: var(--panda-ink); color: var(--panda-white); }
.profile-form__button--quiet { border-color: var(--panda-line-strong); }
.profile-form__error { margin: 0; border: 1px solid var(--panda-danger); border-radius: var(--panda-radius-compact); padding: 0.75rem 0.85rem; background: var(--panda-danger-surface); color: var(--panda-danger); font-weight: 750; line-height: 1.45; }
@media (max-width: 28rem) {
  .profile-form__actions { display: grid; grid-template-columns: 1fr; }
  .profile-form__button { width: 100%; }
}

@media (forced-colors: active) {
  .profile-form__surface { border-color: CanvasText; background: Canvas; box-shadow: none; }
  .profile-form__button { border-color: CanvasText; background: Canvas; color: CanvasText; }
}
</style>
