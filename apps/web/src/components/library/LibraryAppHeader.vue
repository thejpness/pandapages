<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  PopoverContent,
  PopoverPortal,
  PopoverRoot,
  PopoverTrigger,
} from 'reka-ui'
import type { LibrarySort } from '../../lib/library-sorting'

const props = defineProps<{
  q: string
  sort: LibrarySort
  resultLabel: string
  locking: boolean
  surpriseDisabled: boolean
}>()

const emit = defineEmits<{
  'update:q': [value: string]
  'update:sort': [value: LibrarySort]
  clear: []
  surprise: []
  journey: []
  admin: []
  lock: []
  'sticky-offset': [height: number]
}>()

const searchInput = ref<HTMLInputElement | null>(null)
const headerRoot = ref<HTMLElement | null>(null)
const parentMenuOpen = ref(false)
const parentFirstAction = ref<HTMLButtonElement | null>(null)
const parentLastAction = ref<HTMLButtonElement | null>(null)
const lockButton = ref<HTMLButtonElement | null>(null)
const stickyHeader = ref(true)

let headerObserver: ResizeObserver | null = null
let onwardFocus: HTMLElement | null = null

const qModel = computed({
  get: () => props.q,
  set: (value: string) => emit('update:q', value),
})

function updateSort(event: Event) {
  emit('update:sort', (event.target as HTMLSelectElement).value as LibrarySort)
}

async function clearSearch() {
  emit('clear')
  await nextTick()
  searchInput.value?.focus()
}

function focusSearch() {
  searchInput.value?.focus()
}

function chooseParentAction(action: 'journey' | 'admin') {
  parentMenuOpen.value = false
  if (action === 'journey') emit('journey')
  else emit('admin')
}

function handleParentTriggerKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab' || event.shiftKey || !parentMenuOpen.value) return
  event.preventDefault()
  parentFirstAction.value?.focus()
}

function handleParentMenuKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab') return
  const active = document.activeElement

  if (event.shiftKey && active === parentFirstAction.value) {
    event.preventDefault()
    parentMenuOpen.value = false
    return
  }

  if (!event.shiftKey && active === parentLastAction.value) {
    event.preventDefault()
    onwardFocus = lockButton.value
    parentMenuOpen.value = false
  }
}

function handleParentCloseAutoFocus(event: Event) {
  if (onwardFocus === null) return
  event.preventDefault()
  const target = onwardFocus
  onwardFocus = null
  void nextTick(() => target.focus({ preventScroll: true }))
}

function updateStickyHeader() {
  if (headerRoot.value === null) return
  const height = headerRoot.value.getBoundingClientRect().height
  const shouldStick =
    window.innerHeight > 480 && height <= window.innerHeight * 0.42
  stickyHeader.value = shouldStick
  emit('sticky-offset', shouldStick ? height : 0)
}

onMounted(() => {
  headerObserver = new ResizeObserver(updateStickyHeader)
  if (headerRoot.value !== null) headerObserver.observe(headerRoot.value)
  window.addEventListener('resize', updateStickyHeader)
  updateStickyHeader()
})

onBeforeUnmount(() => {
  headerObserver?.disconnect()
  headerObserver = null
  window.removeEventListener('resize', updateStickyHeader)
})

defineExpose({ focusSearch })
</script>

<template>
  <header
    ref="headerRoot"
    class="library-header"
    :class="{ 'library-header--static': !stickyHeader }"
  >
    <div class="library-header__inner">
      <div class="library-header__topline">
        <RouterLink class="library-brand" to="/" aria-label="Panda Pages home">
          <img src="/logo.png" alt="" aria-hidden="true" />
          <span>
            <strong>Panda Pages</strong>
            <small>Story library</small>
          </span>
        </RouterLink>

        <div class="library-header__actions">
          <PopoverRoot v-model:open="parentMenuOpen">
            <div class="parent-options">
              <PopoverTrigger as-child>
                <button
                  class="header-button header-button--quiet"
                  type="button"
                  @keydown="handleParentTriggerKeydown"
                >
                  <span aria-hidden="true">•••</span>
                  <span class="header-button__label">Parent options</span>
                </button>
              </PopoverTrigger>
              <PopoverPortal>
                <PopoverContent
                  class="parent-menu"
                  align="end"
                  side="bottom"
                  :side-offset="8"
                  :collision-padding="16"
                  :prioritize-position="true"
                  position-strategy="fixed"
                  sticky="always"
                  style="position: relative; z-index: 80; max-height: 60dvh; overflow: auto"
                  @open-auto-focus="$event.preventDefault()"
                  @close-auto-focus="handleParentCloseAutoFocus"
                  @keydown.capture="handleParentMenuKeydown"
                >
                  <button
                    ref="parentFirstAction"
                    class="parent-menu__item"
                    type="button"
                    @click="chooseParentAction('journey')"
                  >
                    <span aria-hidden="true">◌</span>
                    Reading profile
                  </button>
                  <button
                    ref="parentLastAction"
                    class="parent-menu__item"
                    type="button"
                    @click="chooseParentAction('admin')"
                  >
                    <span aria-hidden="true">⚙</span>
                    Admin
                  </button>
                </PopoverContent>
              </PopoverPortal>
            </div>
          </PopoverRoot>

          <button
            ref="lockButton"
            class="header-button header-button--ink"
            type="button"
            :disabled="locking"
            aria-label="Lock Panda Pages"
            @click="emit('lock')"
          >
            <span aria-hidden="true">▣</span>
            {{ locking ? 'Locking…' : 'Lock' }}
          </button>
        </div>
      </div>

      <div class="library-header__tools">
        <div class="library-search">
          <label class="library-sr-only" for="library-search">Search the library</label>
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="10.5" cy="10.5" r="6.5" />
            <path d="m15.5 15.5 4.5 4.5" />
          </svg>
          <input
            id="library-search"
            ref="searchInput"
            v-model="qModel"
            type="search"
            placeholder="Search stories or authors"
            autocomplete="off"
            enterkeyhint="search"
          />
          <button
            v-if="qModel"
            type="button"
            aria-label="Clear search"
            @click="clearSearch"
          >
            Clear
          </button>
        </div>

        <div class="library-sort">
          <label for="library-sort">Sort</label>
          <select id="library-sort" :value="sort" @change="updateSort">
            <option value="recent">Recently read</option>
            <option value="title">Title A–Z</option>
            <option value="shortest">Shortest first</option>
            <option value="longest">Longest first</option>
          </select>
        </div>

        <button
          class="surprise-button"
          type="button"
          :disabled="surpriseDisabled"
          @click="emit('surprise')"
        >
          <span aria-hidden="true">✦</span>
          Surprise me
        </button>
      </div>

      <p class="library-result-label" aria-live="polite">{{ resultLabel }}</p>
    </div>
  </header>
</template>

<style scoped>
.library-header {
  position: sticky;
  z-index: 30;
  top: 0;
  border-bottom: 1px solid var(--library-line);
  background: color-mix(in srgb, var(--library-paper) 91%, transparent);
  color: var(--library-ink);
  backdrop-filter: blur(18px);
}

.library-header--static {
  position: relative;
}

.library-header__inner {
  width: min(80rem, 100%);
  margin-inline: auto;
  padding: calc(0.65rem + env(safe-area-inset-top)) max(1rem, env(safe-area-inset-right)) 0.75rem max(1rem, env(safe-area-inset-left));
}

.library-header__topline,
.library-header__actions,
.library-header__tools,
.library-brand,
.header-button,
.surprise-button {
  display: flex;
  align-items: center;
}

.library-header__topline {
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 1rem;
}

.library-brand {
  flex: 1 1 10rem;
  min-width: 0;
  gap: 0.65rem;
  color: inherit;
  text-decoration: none;
}

.library-brand img {
  width: 2.7rem;
  height: 2.7rem;
  flex: 0 0 auto;
  object-fit: contain;
}

.library-brand span {
  display: grid;
  min-width: 0;
}

.library-brand strong {
  font-size: 1rem;
  font-weight: 850;
  letter-spacing: -0.035em;
  overflow-wrap: anywhere;
}

.library-brand small {
  color: var(--library-muted);
  font-size: 0.72rem;
  font-weight: 700;
}

.library-header__actions {
  min-width: 0;
  flex: 0 1 auto;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.45rem;
  margin-inline-start: auto;
}

.header-button,
.surprise-button {
  min-height: 2.75rem;
  justify-content: center;
  gap: 0.45rem;
  border: 1px solid var(--library-ink);
  border-radius: 999px;
  padding: 0.6rem 0.9rem;
  background: var(--library-white);
  color: var(--library-ink);
  font-size: 0.82rem;
  font-weight: 800;
  cursor: pointer;
}

.header-button--ink {
  background: var(--library-ink);
  color: var(--library-white);
}

.header-button:disabled,
.surprise-button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.library-header__tools {
  flex-wrap: wrap;
  gap: 0.65rem;
  margin-top: 0.7rem;
}

.library-search {
  position: relative;
  display: flex;
  min-width: min(100%, 10rem);
  flex: 1 1 15rem;
  align-items: center;
}

.library-search > svg {
  position: absolute;
  left: 0.9rem;
  width: 1.1rem;
  height: 1.1rem;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.8;
  pointer-events: none;
}

.library-search input,
.library-sort select {
  width: 100%;
  min-height: 2.8rem;
  border: 1px solid var(--library-line-strong);
  border-radius: 999px;
  background: var(--library-white);
  color: var(--library-ink);
  font-size: 0.9rem;
}

.library-search input {
  padding: 0.65rem 4rem 0.65rem 2.65rem;
}

.library-search button {
  position: absolute;
  right: 0.45rem;
  min-height: 2.75rem;
  border: 0;
  border-radius: 999px;
  padding-inline: 0.65rem;
  background: var(--library-mist);
  color: inherit;
  font-size: 0.72rem;
  font-weight: 800;
  cursor: pointer;
}

.library-sort {
  display: grid;
  min-width: min(100%, 8rem);
  flex: 0 1 10.5rem;
}

.library-sort label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}

.library-sort select {
  appearance: auto;
  padding: 0.6rem 0.8rem;
  font-weight: 750;
}

.surprise-button {
  flex: 0 0 auto;
}

.library-result-label {
  min-height: 1rem;
  margin: 0.4rem 0 0;
  color: var(--library-muted);
  font-size: 0.72rem;
  text-align: right;
}

.library-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}

@media (max-width: 42rem) {
  .header-button__label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .header-button--quiet {
    width: 2.75rem;
    padding-inline: 0;
  }

  .library-search {
    min-width: 0;
  }

  .library-sort {
    min-width: 0;
  }

}

@media (max-width: 23rem) {
  .library-header__inner {
    padding-right: max(0.65rem, env(safe-area-inset-right));
    padding-left: max(0.65rem, env(safe-area-inset-left));
  }

  .library-brand small {
    display: none;
  }

  .library-result-label {
    text-align: left;
  }
}

@media (max-height: 30rem) {
  .library-header {
    position: relative;
  }
}
</style>

<style scoped>
.parent-options {
  position: relative;
}

.parent-menu {
  position: relative;
  z-index: 80;
  box-sizing: border-box;
  width: min(
    13rem,
    calc(100vw - 2rem - env(safe-area-inset-left) - env(safe-area-inset-right))
  );
  max-width: var(--reka-popover-content-available-width);
  max-height: 60dvh;
  overflow: auto;
  border: 1px solid rgba(17, 17, 15, 0.2);
  border-radius: 1rem;
  padding: 0.35rem;
  background: #fffefa;
  color: #11110f;
  box-shadow: 0 1.1rem 3rem rgba(17, 17, 15, 0.18);
  font-family: "Atkinson Hyperlegible Next Variable", ui-sans-serif, sans-serif;
}

.parent-menu__item {
  display: flex;
  width: 100%;
  min-height: 2.75rem;
  align-items: center;
  gap: 0.65rem;
  border-radius: 0.7rem;
  padding: 0.55rem 0.7rem;
  font-size: 0.88rem;
  font-weight: 760;
  outline: none;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  user-select: none;
}

.parent-menu__item:hover,
.parent-menu__item:focus-visible {
  background: #e7e3d9;
}
</style>
