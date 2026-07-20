<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { logout as logoutSession } from '@/lib/api'
import { authState } from '@/lib/session'
import { runLockTransition } from '@/lib/session-transitions'
import StoryStudioDialog from '@/components/admin/story-studio/StoryStudioDialog.vue'
import StoryStudioHeader from '@/components/admin/story-studio/StoryStudioHeader.vue'

const route = useRoute()
const router = useRouter()

const editorDirty = ref(false)
const leaveDialogOpen = ref(false)
const pendingPath = ref<string | null>(null)
const pendingLock = ref(false)
const locking = ref(false)
const lockError = ref('')
let bypassNextGuard = false
let removeGuard: (() => void) | null = null

function setEditorDirty(value: boolean) {
  editorDirty.value = value
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (!editorDirty.value) return
  event.preventDefault()
  event.returnValue = ''
}

function askToLeave(path: string) {
  pendingPath.value = path
  pendingLock.value = false
  leaveDialogOpen.value = true
}

function navigate(path: string) {
  if (path === route.fullPath) return
  void router.push(path)
}

function cancelLeave() {
  if (locking.value) return
  leaveDialogOpen.value = false
  pendingPath.value = null
  pendingLock.value = false
}

async function runLock() {
  if (locking.value) return
  locking.value = true
  lockError.value = ''
  try {
    const result = await runLockTransition({
      requestLogout: logoutSession,
      clearAccountState: () => {
        editorDirty.value = false
        bypassNextGuard = true
      },
      markLocked: authState.confirmLocked,
      navigateToUnlock: () => router.replace('/unlock'),
    })
    if (result === 'navigation-failed') {
      lockError.value =
        'Panda Pages is locked, but the Unlock screen could not be opened. Reload to continue.'
    }
  } catch {
    lockError.value =
      'Could not lock Panda Pages. Story Studio is still open; try again.'
  } finally {
    locking.value = false
    bypassNextGuard = false
  }
}

function requestLock() {
  if (editorDirty.value) {
    pendingLock.value = true
    pendingPath.value = null
    leaveDialogOpen.value = true
    return
  }
  void runLock()
}

async function confirmLeave() {
  const lock = pendingLock.value
  const path = pendingPath.value
  leaveDialogOpen.value = false
  pendingLock.value = false
  pendingPath.value = null

  if (lock) {
    await runLock()
    return
  }
  if (!path) return

  editorDirty.value = false
  bypassNextGuard = true
  await nextTick()
  try {
    await router.push(path)
  } finally {
    bypassNextGuard = false
  }
}

onMounted(() => {
  window.addEventListener('beforeunload', beforeUnload)
  removeGuard = router.beforeEach((to, from) => {
    if (
      bypassNextGuard ||
      !editorDirty.value ||
      to.fullPath === from.fullPath
    ) {
      return true
    }
    askToLeave(to.fullPath)
    return false
  })
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', beforeUnload)
  removeGuard?.()
  removeGuard = null
})
</script>

<template>
  <div class="story-studio-shell">
    <a class="studio-skip-link" href="#studio-main">Skip to main content</a>
    <StoryStudioHeader
      :current-path="route.path"
      :locking="locking"
      @navigate="navigate"
      @lock="requestLock"
    />

    <p v-if="lockError" class="studio-shell-alert" role="alert">
      <strong>Lock not completed.</strong> {{ lockError }}
    </p>

    <main id="studio-main" class="studio-main" tabindex="-1">
      <router-view v-slot="{ Component }">
        <component :is="Component" @studio-dirty="setEditorDirty" />
      </router-view>
    </main>

    <StoryStudioDialog
      :open="leaveDialogOpen"
      title="Leave with unsaved changes?"
      description="Your latest edits have not been saved as an immutable draft."
      :confirm-label="pendingLock ? 'Discard changes and lock' : 'Discard changes and leave'"
      danger
      @confirm="confirmLeave"
      @cancel="cancelLeave"
    >
      <p>Choose Cancel to return to the editor and save your work.</p>
    </StoryStudioDialog>
  </div>
</template>

<style>
.story-studio-shell {
  --studio-paper: #f5f4ec;
  --studio-card: #fffef9;
  --studio-wash: #e7efe9;
  --studio-ink: #173338;
  --studio-muted: #5c7072;
  --studio-green: #247064;
  --studio-green-dark: #15564d;
  --studio-line: #d9dfd9;
  --studio-line-strong: #bdc8c2;
  --studio-shadow: 0 20px 55px rgb(20 47 50 / 14%);
  --studio-shadow-soft: 0 8px 24px rgb(20 47 50 / 7%);
  min-height: 100dvh;
  background:
    radial-gradient(circle at 8% 8%, rgb(220 239 232 / 65%), transparent 28rem),
    var(--studio-paper);
  color: var(--studio-ink);
}

.studio-main {
  width: min(86rem, 100%);
  margin-inline: auto;
  padding: clamp(1.25rem, 3vw, 2.5rem) max(1rem, env(safe-area-inset-right)) max(2rem, calc(2rem + env(safe-area-inset-bottom))) max(1rem, env(safe-area-inset-left));
}

.studio-skip-link {
  position: fixed;
  z-index: 100;
  top: 0.5rem;
  left: 0.5rem;
  transform: translateY(-150%);
  border-radius: 0.6rem;
  background: var(--studio-ink);
  color: white;
  padding: 0.7rem 1rem;
}

.studio-skip-link:focus { transform: translateY(0); }

.studio-shell-alert {
  width: min(82rem, calc(100% - 2rem));
  margin: 1rem auto 0;
  border: 1px solid #dfa187;
  border-radius: 0.8rem;
  background: #fff1e9;
  padding: 0.8rem 1rem;
  color: #763923;
}

.studio-page-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1.25rem;
  margin-bottom: 1.5rem;
}

.studio-page-heading__eyebrow {
  color: var(--studio-green);
  font-size: 0.75rem;
  font-weight: 780;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.studio-page-heading h1 {
  overflow-wrap: anywhere;
  margin-top: 0.25rem;
  color: var(--studio-ink);
  font-family: 'Literata Variable', Georgia, serif;
  font-size: clamp(1.8rem, 4vw, 2.75rem);
  font-weight: 650;
  letter-spacing: -0.025em;
  line-height: 1.15;
}

.studio-page-heading__summary {
  max-width: 46rem;
  margin-top: 0.55rem;
  color: var(--studio-muted);
  line-height: 1.6;
}

.studio-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 2.75rem;
  border: 1px solid transparent;
  border-radius: 0.75rem;
  padding: 0.65rem 1rem;
  font-size: 0.9rem;
  font-weight: 720;
  line-height: 1.2;
  text-align: center;
}

.studio-button--primary { background: var(--studio-green); color: white; }
.studio-button--primary:hover { background: var(--studio-green-dark); }
.studio-button--quiet { border-color: var(--studio-line-strong); background: var(--studio-card); color: var(--studio-ink); }
.studio-button--quiet:hover { background: var(--studio-wash); }
.studio-button--danger { background: #a84b36; color: white; }
.studio-button--danger:hover { background: #843827; }
.studio-button:disabled { cursor: not-allowed; opacity: 0.52; }

.studio-button:focus-visible,
.story-studio-shell button:focus-visible,
.story-studio-shell a:focus-visible,
.story-studio-shell input:focus-visible,
.story-studio-shell select:focus-visible,
.story-studio-shell textarea:focus-visible {
  outline: 3px solid #e3a848;
  outline-offset: 3px;
}

.studio-visually-hidden {
  position: absolute !important;
  width: 1px !important;
  height: 1px !important;
  overflow: hidden !important;
  clip: rect(0, 0, 0, 0) !important;
  white-space: nowrap !important;
  clip-path: inset(50%) !important;
}

.studio-field label {
  display: flex;
  justify-content: space-between;
  gap: 0.6rem;
  color: var(--studio-ink);
  font-size: 0.84rem;
  font-weight: 720;
}

.studio-field label span { color: var(--studio-muted); font-weight: 500; }

.studio-field input,
.studio-field select {
  display: block;
  width: 100%;
  min-height: 2.85rem;
  margin-top: 0.4rem;
  border: 1px solid var(--studio-line-strong);
  border-radius: 0.75rem;
  background: #fbfcf7;
  color: var(--studio-ink);
  padding: 0.65rem 0.75rem;
  font-size: max(1rem, 16px);
}

.studio-field input[readonly] { background: #edf0ea; color: #607174; }
.studio-field input[aria-invalid='true'] { border-color: #b65b3d; }
.studio-field__hint { margin-top: 0.3rem; color: var(--studio-muted); font-size: 0.76rem; line-height: 1.45; }
.studio-field__error { margin-top: 0.4rem; color: #9b4028; font-size: 0.82rem; font-weight: 650; }

.studio-panel {
  border: 1px solid var(--studio-line);
  border-radius: 1.15rem;
  background: var(--studio-card);
  padding: clamp(1rem, 3vw, 1.5rem);
  box-shadow: var(--studio-shadow-soft);
}

.studio-rendered-story {
  color: #243b3f;
  font-family: 'Literata Variable', Georgia, serif;
  font-size: 1rem;
  line-height: 1.75;
}

.studio-rendered-story h1,
.studio-rendered-story h2,
.studio-rendered-story h3 {
  margin-block: 1.3em 0.5em;
  color: #173338;
  font-weight: 680;
  line-height: 1.25;
}

.studio-rendered-story h1 { margin-top: 0; font-size: 1.7rem; }
.studio-rendered-story h2 { font-size: 1.3rem; }
.studio-rendered-story p + p { margin-top: 0.9rem; }

@media (max-width: 640px) {
  .studio-page-heading { align-items: stretch; flex-direction: column; }
  .studio-page-heading > .studio-button { width: 100%; }
}

@media (prefers-reduced-motion: reduce) {
  .story-studio-shell *,
  .story-studio-shell *::before,
  .story-studio-shell *::after {
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
  }
}
</style>
