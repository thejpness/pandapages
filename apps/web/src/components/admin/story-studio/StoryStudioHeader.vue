<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  currentPath: string
  locking: boolean
}>()

const emit = defineEmits<{
  navigate: [path: string]
  lock: []
}>()

const menuOpen = ref(false)

const storiesActive = computed(
  () => props.currentPath === '/admin/stories' || props.currentPath.startsWith('/admin/stories/'),
)

function navigate(path: string) {
  menuOpen.value = false
  emit('navigate', path)
}
</script>

<template>
  <header class="studio-header">
    <div class="studio-header__inner">
      <button
        type="button"
        class="studio-brand"
        aria-label="Story Studio stories"
        @click="navigate('/admin/stories')"
      >
        <img class="studio-brand__panda" src="/logo.png" alt="" aria-hidden="true" />
        <span>
          <strong>Panda Pages</strong>
          <small>Story Studio</small>
        </span>
      </button>

      <button
        type="button"
        class="studio-header__menu"
        :aria-expanded="menuOpen"
        aria-controls="studio-navigation"
        @click="menuOpen = !menuOpen"
      >
        Menu
      </button>

      <nav id="studio-navigation" class="studio-nav" :class="{ 'studio-nav--open': menuOpen }" aria-label="Story Studio">
        <button
          type="button"
          :aria-current="storiesActive ? 'page' : undefined"
          @click="navigate('/admin/stories')"
        >
          Stories
        </button>
        <button type="button" class="studio-nav__new" @click="navigate('/admin/stories/new')">
          <span aria-hidden="true">+</span> New story
        </button>
        <span class="studio-nav__divider" aria-hidden="true" />
        <button
          type="button"
          class="studio-nav__secondary"
          :aria-current="currentPath === '/admin/ai' ? 'page' : undefined"
          @click="navigate('/admin/ai')"
        >
          AI create
        </button>
        <button type="button" class="studio-nav__secondary" @click="navigate('/library')">
          Library
        </button>
        <button
          type="button"
          class="studio-nav__lock"
          :disabled="locking"
          aria-label="Lock Panda Pages"
          @click="emit('lock')"
        >
          {{ locking ? 'Locking…' : 'Lock' }}
        </button>
      </nav>
    </div>
  </header>
</template>

<style scoped>
.studio-header {
  position: sticky;
  z-index: 40;
  top: 0;
  border-bottom: 1px solid var(--panda-line-strong);
  background: color-mix(in srgb, var(--panda-paper) 94%, transparent);
  backdrop-filter: blur(14px);
}

.studio-header__inner {
  display: flex;
  align-items: center;
  width: min(var(--panda-content-width), 100%);
  min-height: calc(4.5rem + env(safe-area-inset-top));
  margin-inline: auto;
  padding: var(--panda-safe-top) var(--panda-safe-right) 0.75rem var(--panda-safe-left);
  gap: 1.5rem;
}

.studio-brand {
  display: flex;
  align-items: center;
  min-height: 2.75rem;
  gap: 0.7rem;
  color: var(--panda-ink);
  text-align: left;
}

.studio-brand__panda {
  width: 2.5rem;
  height: 2.5rem;
  flex: 0 0 auto;
  object-fit: contain;
}

.studio-brand strong,
.studio-brand small {
  display: block;
}

.studio-brand strong {
  font-family: var(--panda-serif);
  font-size: 1rem;
  line-height: 1.1;
}

.studio-brand small {
  margin-top: 0.18rem;
  color: var(--panda-muted);
  font-size: 0.75rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.studio-nav {
  display: flex;
  align-items: center;
  margin-left: auto;
  gap: 0.3rem;
}

.studio-nav button,
.studio-header__menu {
  min-height: 2.75rem;
  border-radius: var(--panda-radius-compact);
  padding: 0.65rem 0.85rem;
  color: var(--panda-muted);
  font-size: 0.9rem;
  font-weight: 650;
}

.studio-nav button:hover,
.studio-nav button[aria-current='page'] {
  background: var(--panda-mist);
  color: var(--panda-ink);
}

.studio-nav .studio-nav__new {
  border: 1px solid var(--panda-ink);
  background: var(--panda-ink);
  color: var(--panda-white);
}

.studio-nav .studio-nav__new:hover {
  background: var(--panda-soft-ink);
  color: var(--panda-white);
}

.studio-nav__divider {
  width: 1px;
  height: 1.7rem;
  margin-inline: 0.35rem;
  background: var(--panda-line-strong);
}

.studio-nav .studio-nav__secondary {
  font-weight: 550;
}

.studio-nav .studio-nav__lock {
  border: 1px solid var(--panda-line-strong);
  color: var(--panda-ink);
}

.studio-header__menu {
  display: none;
  margin-left: auto;
  border: 1px solid var(--panda-line-strong);
}

@media (max-width: 760px) {
  .studio-header__inner {
    position: relative;
  }

  .studio-header__menu {
    display: block;
  }

  .studio-nav {
    display: none;
    position: absolute;
    top: calc(100% - 0.2rem);
    right: var(--panda-safe-right);
    left: var(--panda-safe-left);
    flex-direction: column;
    align-items: stretch;
    border: 1px solid var(--panda-line-strong);
    border-radius: var(--panda-radius-card);
    background: var(--panda-paper-raised);
    box-shadow: var(--panda-shadow);
    padding: 0.6rem;
  }

  .studio-nav--open {
    display: flex;
  }

  .studio-nav__divider {
    width: 100%;
    height: 1px;
    margin: 0.25rem 0;
  }
}

@media (max-height: 30rem) {
  .studio-header {
    position: relative;
  }
}
</style>
