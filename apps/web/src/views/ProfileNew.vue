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
<template><PandaAuthShell eyebrow="Profiles" title="Add profile" description="Create a reader without starting a reading session."><form class="form" @submit.prevent="createProfile"><ProfileFields v-model:name="name" v-model:reading-level="readingLevel" name-id="new-profile-name" level-id="new-profile-level" :disabled="busy" /><p v-if="errorMessage" class="error" role="alert">{{ errorMessage }}</p><div class="actions"><button type="button" :disabled="busy" @click="router.push(profileCreateReturnDestination(route.query.from))">Cancel</button><button type="submit" :disabled="busy">{{ busy ? "Creating…" : "Create profile" }}</button></div></form></PandaAuthShell></template>
<style scoped>.form{display:grid;gap:1rem}.actions{display:flex;flex-wrap:wrap;gap:.65rem}.error{margin:0;color:var(--panda-danger,#8b1e1e);font-weight:700}button{min-height:2.7rem;border:1px solid var(--panda-ink);border-radius:var(--panda-radius-compact);padding:.55rem .85rem;background:var(--panda-paper-raised);color:var(--panda-ink);font:inherit;font-weight:750}button[type="submit"]{background:var(--panda-ink);color:var(--panda-white)}</style>
