<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import PandaAuthShell from "../components/app/PandaAuthShell.vue";
import ProfileIdentity from "../components/profile/ProfileIdentity.vue";
import { listReaderProfiles, logout as logoutSession, type ReaderProfile } from "../lib/api";
import { currentAccountContext } from "../lib/account-context";
import { readerEditionLabel } from "../lib/reader-editions";
const router=useRouter(); const profiles=ref<readonly ReaderProfile[]>([]); const loading=ref(true); const errorMessage=ref(""); const signingOut=ref(false); const canSwitchAccount=ref(false); const canOpenStoryStudio=ref(false); const accountName=ref(""); const principalName=ref(""); const accountRole=ref<"owner"|"adult"|null>(null); const accountRoleLabel=computed(()=>accountRole.value==="owner"?"Owner":"Adult member");
async function refresh(){const account=await currentAccountContext(); profiles.value=await listReaderProfiles(account);canSwitchAccount.value=account.identity.memberships.length>1;canOpenStoryStudio.value=account.membership.role==="owner";accountName.value=account.membership.accountName;principalName.value=account.identity.principal.displayName;accountRole.value=account.membership.role;}
async function signOut(){if(signingOut.value)return;signingOut.value=true;try{await logoutSession();await router.replace("/account/login");}catch{errorMessage.value="Sign-out could not be completed. Your Panda Pages account is still open.";}finally{signingOut.value=false;}}
onMounted(async()=>{try{await refresh();}catch(error){errorMessage.value=error instanceof Error&&error.message?error.message:"Profiles could not be loaded. Please try again.";}finally{loading.value=false;}});
</script>
<template>
  <PandaAuthShell eyebrow="Parent area" title="Manage profiles" description="Update reader settings without starting a reading session.">
    <template #navigation>
      <nav class="profile-manage__return" aria-label="Profile navigation">
        <button class="profile-manage__back" type="button" @click="router.push('/profiles')">
          <span aria-hidden="true">←</span> Who’s reading?
        </button>
      </nav>
    </template>
    <div class="profile-manage">
      <p v-if="loading" class="profile-manage__state" role="status">Loading profiles…</p>
      <p v-else-if="errorMessage" class="profile-manage__state profile-manage__state--error" role="alert">
        {{ errorMessage }}
      </p>
      <template v-else>
        <section class="profile-manage__section" aria-labelledby="reader-profiles-heading">
          <div class="profile-manage__section-heading">
            <div>
              <h2 id="reader-profiles-heading">Reader profiles</h2>
              <p>Choose a reader to update their settings. Editing never starts a reading session.</p>
            </div>
            <button class="profile-manage__button profile-manage__button--primary" type="button" @click="router.push({ path: '/profiles/new', query: { from: 'manage' } })">
              <span aria-hidden="true">+</span> Add profile
            </button>
          </div>

          <p v-if="profiles.length === 0" class="profile-manage__empty">
            No reader profiles yet. Add one when you are ready to set up a reading space.
          </p>
          <ul v-else class="profile-manage__list" aria-label="Reader profiles">
            <li v-for="profile in profiles" :key="profile.id" class="profile-manage__profile">
              <ProfileIdentity class="profile-manage__identity" :profileID="profile.id" />
              <div class="profile-manage__summary">
                <h3>{{ profile.name }}</h3>
                <dl>
                  <div>
                    <dt>Reading level</dt>
                    <dd>{{ readerEditionLabel(profile.readingLevel) }}</dd>
                  </div>
                  <div>
                    <dt>Entry</dt>
                    <dd>{{ profile.pinEnabled ? 'PIN protected' : 'No PIN' }}</dd>
                  </div>
                </dl>
              </div>
              <button class="profile-manage__button" type="button" @click="router.push('/profiles/' + encodeURIComponent(profile.id) + '/edit')">
                Edit {{ profile.name }}
              </button>
            </li>
          </ul>

        </section>

        <section class="profile-manage__account" aria-labelledby="account-heading">
          <div class="profile-manage__section-heading">
            <div>
              <h2 id="account-heading">Account</h2>
              <p>Parent tools stay separate from reader setup.</p>
            </div>
          </div>
          <div class="profile-manage__account-card">
            <p class="profile-manage__account-name">{{ accountName }}</p>
            <p class="profile-manage__account-context">Signed in as {{ principalName }} · {{ accountRoleLabel }}</p>
            <div class="profile-manage__account-actions">
              <button v-if="canSwitchAccount" class="profile-manage__button" type="button" @click="router.push('/account')">Switch account</button>
              <button v-if="canOpenStoryStudio" class="profile-manage__button" type="button" @click="router.push('/admin/stories')">Story Studio</button>
              <button class="profile-manage__button profile-manage__button--signout" type="button" :disabled="signingOut" @click="signOut">
                {{ signingOut ? 'Signing out…' : 'Sign out' }}
              </button>
            </div>
          </div>
        </section>
      </template>
    </div>
  </PandaAuthShell>
</template>

<style scoped>
.profile-manage__return { display: flex; }
.profile-manage { display: grid; gap: 2rem; }
.profile-manage__section,
.profile-manage__account { display: grid; gap: 1rem; }
.profile-manage__account { border-top: 1px solid var(--panda-line-strong); padding-top: 1.65rem; }
.profile-manage__section-heading { display: flex; align-items: end; justify-content: space-between; gap: 1rem; }
.profile-manage__section-heading h2 { margin: 0; font-family: var(--panda-serif); font-size: 1.35rem; font-weight: 680; letter-spacing: -0.02em; }
.profile-manage__section-heading p { margin: 0.3rem 0 0; color: var(--panda-muted); line-height: 1.45; }
.profile-manage__list { display: grid; gap: 0.75rem; margin: 0; padding: 0; list-style: none; }
.profile-manage__profile { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 0.9rem; border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-card); padding: 0.85rem; background: var(--panda-white); box-shadow: var(--panda-shadow-soft); }
.profile-manage__identity { --profile-identity-size: 3.8rem; }
.profile-manage__summary { min-width: 0; }
.profile-manage__summary h3 { margin: 0; overflow-wrap: anywhere; font-size: 1.05rem; font-weight: 850; }
.profile-manage__summary dl { display: flex; flex-wrap: wrap; gap: 0.25rem 1rem; margin: 0.35rem 0 0; }
.profile-manage__summary dl div { display: flex; gap: 0.3rem; }
.profile-manage__summary dt { color: var(--panda-muted); font-size: 0.79rem; }
.profile-manage__summary dd { margin: 0; color: var(--panda-soft-ink); font-size: 0.79rem; font-weight: 750; }
.profile-manage__button,
.profile-manage__back { min-height: 2.75rem; border: 1px solid var(--panda-ink); border-radius: var(--panda-radius-compact); padding: 0.58rem 0.9rem; background: var(--panda-paper-raised); color: var(--panda-ink); font: inherit; font-weight: 800; line-height: 1.15; }
.profile-manage__button--primary { background: var(--panda-ink); color: var(--panda-white); white-space: nowrap; }
.profile-manage__button--primary span { margin-right: 0.18rem; font-size: 1.15em; }
.profile-manage__back { display: inline-flex; width: fit-content; align-items: center; gap: 0.45rem; border-color: var(--panda-line-strong); background: var(--panda-white); text-decoration: none; box-shadow: var(--panda-shadow-soft); }
.profile-manage__empty,
.profile-manage__state { margin: 0; border: 1px dashed var(--panda-line-strong); border-radius: var(--panda-radius-compact); padding: 1rem; color: var(--panda-muted); line-height: 1.5; }
.profile-manage__state--error { border-style: solid; border-color: var(--panda-danger); background: var(--panda-danger-surface); color: var(--panda-danger); font-weight: 750; }
.profile-manage__account-card { border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-card); padding: 1rem; background: var(--panda-mist); }
.profile-manage__account-name,
.profile-manage__account-context { margin: 0; }
.profile-manage__account-name { font-weight: 850; }
.profile-manage__account-context { margin-top: 0.25rem; color: var(--panda-muted); font-size: 0.9rem; line-height: 1.45; }
.profile-manage__account-actions { display: flex; flex-wrap: wrap; gap: 0.65rem; margin-top: 1rem; }
.profile-manage__button--signout { border-color: var(--panda-line-strong); }
@media (max-width: 32rem) {
  .profile-manage__section-heading { align-items: stretch; flex-direction: column; }
  .profile-manage__section-heading .profile-manage__button { width: 100%; }
  .profile-manage__profile { grid-template-columns: auto minmax(0, 1fr); }
  .profile-manage__profile > .profile-manage__button { grid-column: 1 / -1; width: 100%; }
  .profile-manage__account-actions { display: grid; grid-template-columns: 1fr; }
  .profile-manage__account-actions .profile-manage__button { width: 100%; }
}

@media (forced-colors: active) {
  .profile-manage__profile, .profile-manage__account-card { border-color: CanvasText; background: Canvas; box-shadow: none; }
  .profile-manage__button, .profile-manage__back { border-color: CanvasText; background: Canvas; color: CanvasText; }
}
</style>
