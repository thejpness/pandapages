<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import ProfileIdentity from '../components/profile/ProfileIdentity.vue';
import ProfilePINDialog from '../components/profile/ProfilePINDialog.vue';
import { getAPIErrorStatus, listReaderProfiles, type ReaderProfile, verifyReaderProfilePIN } from "../lib/api";
import { currentAccountContext } from "../lib/account-context";
import { resolveReaderDestination } from "../lib/profile-destination";
import { enterReaderProfileSession } from "../lib/profile-session";
import { readerEditionLabel } from '../lib/reader-editions';
import { clearSelectedReaderProfile, reconcileReaderProfileSelection, selectedReaderProfileID } from "../lib/reader-profile-selection";
import { isChildMode, isChildModeFor } from "../lib/reader-mode";
const route=useRoute();const router=useRouter();const profiles=ref<readonly ReaderProfile[]>([]);const selectedID=ref<string|null>(null);const loading=ref(true);const busy=ref(false);const errorMessage=ref("");const pinTarget=ref<ReaderProfile|null>(null);const pinValue=ref("");const pinError=ref("");const pinTrigger=ref<HTMLElement|null>(null);const unavailable=computed(()=>route.query.unavailable==='1');
const destination=()=>resolveReaderDestination(route.query.next);const message=(error:unknown)=>error instanceof Error&&error.message?error.message:"Profiles could not be updated. Please try again.";
function readerStatus(profileID:string){if(selectedID.value!==profileID)return null;return isChildModeFor(profileID)?'Current reader':'Selected reader';}
function restore(){const value=reconcileReaderProfileSelection(selectedReaderProfileID(),profiles.value);if(!value){clearSelectedReaderProfile();selectedID.value=null;return;}selectedID.value=value.id;}
async function refresh(){profiles.value=await listReaderProfiles(await currentAccountContext());restore();}
async function choose(profile:ReaderProfile,trigger?:EventTarget|null){if(isChildModeFor(profile.id) && selectedReaderProfileID() === profile.id){await router.replace(destination());return;}if(profile.pinEnabled){pinTrigger.value=trigger instanceof HTMLElement?trigger:null;pinTarget.value=profile;pinValue.value="";pinError.value="";return;}if(!enterReaderProfileSession(profile.id)){errorMessage.value="This reader could not be started. Please try again.";return;}selectedID.value=profile.id;await router.replace(destination());}
function closePIN(restoreFocus=true){pinTarget.value=null;pinValue.value="";pinError.value="";if(restoreFocus){const trigger=pinTrigger.value;void nextTick(()=>trigger?.focus({preventScroll:true}));}pinTrigger.value=null;}
async function submitPIN(){const profile=pinTarget.value;if(!profile||busy.value)return;busy.value=true;pinError.value="";try{await verifyReaderProfilePIN(profile.id,pinValue.value);if(!enterReaderProfileSession(profile.id)){pinError.value="This reader could not be started. Please try again.";return;}selectedID.value=profile.id;closePIN(false);await router.replace(destination());}catch(error){pinError.value=getAPIErrorStatus(error)===429?"Too many tries. Please wait before trying again.":"That PIN is not right.";}finally{busy.value=false;}}
onMounted(async()=>{try{await refresh();}catch(error){errorMessage.value=message(error);}finally{loading.value=false;}});
</script>
<template>
  <div class="profile-chooser panda-print-surface">
    <a class="profile-chooser__skip" href="#profile-chooser-main">Skip to reader profiles</a>

    <header class="profile-chooser__brand" aria-label="Panda Pages">
      <img src="/logo.png" alt="" width="48" height="48" decoding="async" />
      <span><strong>Panda Pages</strong><small>Stories for curious readers</small></span>
    </header>

    <main id="profile-chooser-main" class="profile-chooser__main" tabindex="-1">
      <section class="profile-chooser__intro" aria-labelledby="profile-chooser-title">
        <p class="profile-chooser__eyebrow">Reader profiles</p>
        <h1 id="profile-chooser-title">Who’s reading?</h1>
        <p>Choose a reader to start a story.</p>
      </section>

      <section v-if="loading" class="profile-state" role="status" aria-live="polite">
        <span class="profile-state__mark" aria-hidden="true">…</span>
        <h2>Loading readers</h2>
        <p>Getting your Panda Pages readers ready.</p>
      </section>
      <section v-else-if="errorMessage" class="profile-state profile-state--error" role="alert">
        <span class="profile-state__mark" aria-hidden="true">!</span>
        <h2>Profiles are unavailable</h2>
        <p>{{ errorMessage }}</p>
      </section>
      <template v-else>
        <p v-if="unavailable" class="profile-notice" role="status">Readers could not be checked just now. Choose one when the connection is available.</p>
        <section v-if="profiles.length === 0" class="profile-state profile-state--empty">
          <span class="profile-state__mark" aria-hidden="true">+</span>
          <h2>Add the first reader</h2>
          <p>Every story starts with choosing who is reading.</p>
        </section>
        <ul v-else class="profile-grid" aria-label="Readers">
          <li v-for="profile in profiles" :key="profile.id">
            <button class="profile-choice" type="button" :aria-label="`Start reading as ${profile.name}`" @click="choose(profile, $event.currentTarget)">
              <ProfileIdentity class="profile-choice__identity" :profileID="profile.id" />
              <span class="profile-choice__copy">
                <span class="profile-choice__name">{{ profile.name }}</span>
                <span class="profile-choice__level">{{ readerEditionLabel(profile.readingLevel) }}</span>
              </span>
              <span class="profile-choice__states">
                <span v-if="profile.pinEnabled" class="profile-choice__pin">PIN protected</span>
                <span v-if="readerStatus(profile.id)" class="profile-choice__current">{{ readerStatus(profile.id) }}</span>
              </span>
            </button>
          </li>
        </ul>
      </template>

      <nav v-if="!isChildMode()" class="profile-chooser__parent-actions" aria-label="Profile management">
        <button class="profile-chooser__add" type="button" @click="router.push({ path: &quot;/profiles/new&quot;, query: { from: &quot;chooser&quot; } })"><span aria-hidden="true">+</span>Add profile</button>
        <button class="profile-chooser__manage" type="button" @click="router.push(&quot;/profiles/manage&quot;)">Manage profiles</button>
      </nav>
    </main>

    <ProfilePINDialog
      v-if="pinTarget"
      :open="pinTarget !== null"
      :profileID="pinTarget.id"
      :profile-name="pinTarget.name"
      :pin-value="pinValue"
      :busy="busy"
      :error="pinError"
      @update:pin-value="pinValue = $event"
      @submit="submitPIN"
      @cancel="closePIN"
    />
  </div>
</template>

<style scoped>
.profile-chooser {
  min-height: 100dvh;
  overflow-x: clip;
  padding: max(1rem, env(safe-area-inset-top)) max(1rem, env(safe-area-inset-right)) max(1.5rem, env(safe-area-inset-bottom)) max(1rem, env(safe-area-inset-left));
  background: var(--panda-paper);
  color: var(--panda-ink);
  font-family: var(--panda-sans);
}

.profile-chooser :where(button, input):focus-visible { outline: 3px solid var(--panda-focus); outline-offset: 4px; }
.profile-chooser__skip {
  position: fixed;
  z-index: 10;
  top: max(0.65rem, env(safe-area-inset-top));
  left: max(0.65rem, env(safe-area-inset-left));
  transform: translateY(-180%);
  border: 2px solid var(--panda-ink);
  border-radius: var(--panda-radius-compact);
  padding: 0.65rem 0.9rem;
  background: var(--panda-white);
  color: var(--panda-ink);
  font-weight: 800;
  text-decoration: none;
}
.profile-chooser__skip:focus { transform: none; }

.profile-chooser__brand {
  display: flex;
  width: min(100%, 76rem);
  align-items: center;
  gap: 0.75rem;
  margin: 0 auto clamp(2.5rem, 8vh, 6rem);
}
.profile-chooser__brand img {
  width: 3rem;
  height: 3rem;
  flex: 0 0 auto;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-white);
  object-fit: contain;
}
.profile-chooser__brand span { display: grid; min-width: 0; }
.profile-chooser__brand strong { font-size: 1rem; font-weight: 850; letter-spacing: -0.025em; }
.profile-chooser__brand small { color: var(--panda-muted); font-size: 0.76rem; font-weight: 650; }

.profile-chooser__main { width: min(100%, 76rem); margin-inline: auto; outline: none; }
.profile-chooser__main:focus-visible { outline: 3px solid var(--panda-focus); outline-offset: 0.45rem; }
.profile-chooser__intro { max-width: 42rem; margin: 0 auto clamp(2rem, 6vh, 4rem); text-align: center; }
.profile-chooser__eyebrow { margin: 0 0 0.45rem; color: var(--panda-soft-ink); font-size: 0.75rem; font-weight: 850; letter-spacing: 0.11em; text-transform: uppercase; }
.profile-chooser h1, .profile-chooser h2, .profile-chooser p { margin: 0; }
.profile-chooser h1 { color: var(--panda-ink); font-family: var(--panda-serif); font-size: clamp(2.35rem, 7vw, 4.6rem); font-weight: 680; letter-spacing: -0.055em; line-height: 1.04; text-wrap: balance; }
.profile-chooser__intro > p:last-child { margin-top: 0.8rem; color: var(--panda-muted); font-size: clamp(1rem, 2vw, 1.15rem); line-height: 1.55; }

.profile-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 12rem), 15rem)); justify-content: center; gap: clamp(0.9rem, 2.4vw, 1.5rem); margin: 0; padding: 0; list-style: none; }
.profile-choice {
  display: grid;
  width: 100%;
  min-height: 17rem;
  align-content: start;
  justify-items: center;
  gap: 0.95rem;
  border: 1px solid var(--panda-line-strong);
  border-radius: 1.35rem;
  padding: clamp(1rem, 4vw, 1.45rem);
  background: var(--panda-paper-raised);
  color: var(--panda-ink);
  box-shadow: var(--panda-shadow-soft);
  cursor: pointer;
  font: inherit;
  text-align: center;
}
.profile-choice:hover { border-color: var(--panda-ink); }
.profile-choice:active { transform: translateY(1px); }
.profile-choice__identity { --profile-identity-size: clamp(5.75rem, 18vw, 7rem); }
.profile-choice__copy, .profile-choice__states { display: grid; gap: 0.32rem; }
.profile-choice__name { overflow-wrap: anywhere; font-family: var(--panda-serif); font-size: clamp(1.35rem, 4vw, 1.75rem); font-weight: 680; letter-spacing: -0.035em; line-height: 1.1; }
.profile-choice__level { color: var(--panda-muted); font-size: 0.9rem; font-weight: 700; }
.profile-choice__states { min-height: 2.4rem; align-content: start; margin-top: auto; font-size: 0.76rem; font-weight: 800; letter-spacing: 0.025em; }
.profile-choice__pin { color: var(--panda-soft-ink); }
.profile-choice__current { width: fit-content; justify-self: center; border: 1px solid var(--panda-success); border-radius: var(--panda-radius-pill); padding: 0.2rem 0.55rem; color: var(--panda-success); }

.profile-chooser__parent-actions { display: flex; flex-wrap: wrap; justify-content: center; gap: 0.7rem; margin-top: clamp(2rem, 6vh, 4rem); }
.profile-chooser__parent-actions button { min-height: 2.75rem; border: 1px solid var(--panda-ink); border-radius: var(--panda-radius-compact); padding: 0.6rem 0.9rem; background: var(--panda-paper-raised); color: var(--panda-ink); font: inherit; font-weight: 800; }
.profile-chooser__add { background: var(--panda-ink) !important; color: var(--panda-white) !important; }
.profile-chooser__add span { margin-right: 0.25rem; font-size: 1.1em; }
.profile-chooser__manage { border-color: var(--panda-line-strong) !important; }

.profile-state { display: grid; max-width: 30rem; justify-items: center; gap: 0.65rem; margin: 0 auto; border: 1px solid var(--panda-line-strong); border-radius: var(--panda-radius-card); padding: clamp(1.4rem, 6vw, 2.5rem); background: var(--panda-paper-raised); box-shadow: var(--panda-shadow-soft); text-align: center; }
.profile-state__mark { display: grid; width: 2.75rem; aspect-ratio: 1; place-items: center; border: 1px solid currentColor; border-radius: 50%; color: var(--panda-soft-ink); font-size: 1.2rem; font-weight: 850; }
.profile-state h2 { font-family: var(--panda-serif); font-size: 1.5rem; }
.profile-state p { color: var(--panda-muted); line-height: 1.5; }
.profile-state--error { border-color: var(--panda-danger); }
.profile-state--error .profile-state__mark { color: var(--panda-danger); }
.profile-notice { width: min(100%, 44rem); margin: 0 auto 1.25rem; border: 1px solid var(--panda-warning); border-radius: var(--panda-radius-compact); padding: 0.75rem 0.9rem; background: var(--panda-warning-surface); color: var(--panda-warning); font-weight: 700; line-height: 1.45; text-align: center; }

@media (max-width: 29rem) {
  .profile-chooser { padding-inline: max(0.8rem, env(safe-area-inset-right)) max(0.8rem, env(safe-area-inset-left)); }
  .profile-chooser__brand { margin-bottom: 2.25rem; }
  .profile-grid { grid-template-columns: 1fr; }
  .profile-choice { min-height: 0; grid-template-columns: auto minmax(0, 1fr); justify-items: start; text-align: left; }
  .profile-choice__identity { --profile-identity-size: 5.5rem; grid-row: span 2; }
  .profile-choice__states { min-height: 0; margin-top: 0; }
  .profile-choice__current { justify-self: start; }
}

@media (forced-colors: active) {
  .profile-chooser { background: Canvas; color: CanvasText; }
  .profile-chooser::before { display: none; }
  .profile-chooser__brand img, .profile-choice, .profile-state { border-color: CanvasText; background: Canvas; box-shadow: none; }
  .profile-choice__current { border-color: Highlight; color: Highlight; }
  .profile-chooser__add { border-color: CanvasText; background: Canvas !important; color: CanvasText !important; }
}
</style>
