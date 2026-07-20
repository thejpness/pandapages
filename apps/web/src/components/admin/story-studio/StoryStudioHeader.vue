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
        <span class="studio-brand__panda" aria-hidden="true">PP</span>
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
  border-bottom: 1px solid var(--studio-line);
  background: color-mix(in srgb, var(--studio-paper) 92%, transparent);
  backdrop-filter: blur(18px);
}

.studio-header__inner {
  display: flex;
  align-items: center;
  width: min(86rem, 100%);
  min-height: calc(4.5rem + env(safe-area-inset-top));
  margin-inline: auto;
  padding: calc(0.75rem + env(safe-area-inset-top)) max(1rem, env(safe-area-inset-right)) 0.75rem max(1rem, env(safe-area-inset-left));
  gap: 1.5rem;
}

.studio-brand {
  display: flex;
  align-items: center;
  min-height: 2.75rem;
  gap: 0.7rem;
  color: var(--studio-ink);
  text-align: left;
}

.studio-brand__panda {
  display: grid;
  place-items: center;
  width: 2.5rem;
  height: 2.5rem;
  border-radius: 0.8rem;
  background: var(--studio-green);
  color: white;
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0.08em;
}

.studio-brand strong,
.studio-brand small {
  display: block;
}

.studio-brand strong {
  font-family: 'Literata Variable', Georgia, serif;
  font-size: 1rem;
  line-height: 1.1;
}

.studio-brand small {
  margin-top: 0.18rem;
  color: var(--studio-muted);
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
  border-radius: 0.75rem;
  padding: 0.65rem 0.85rem;
  color: var(--studio-muted);
  font-size: 0.9rem;
  font-weight: 650;
}

.studio-nav button:hover,
.studio-nav button[aria-current='page'] {
  background: var(--studio-wash);
  color: var(--studio-ink);
}

.studio-nav .studio-nav__new {
  background: var(--studio-green);
  color: white;
}

.studio-nav__divider {
  width: 1px;
  height: 1.7rem;
  margin-inline: 0.35rem;
  background: var(--studio-line);
}

.studio-nav .studio-nav__secondary {
  font-weight: 550;
}

.studio-nav .studio-nav__lock {
  border: 1px solid var(--studio-line-strong);
  color: var(--studio-ink);
}

.studio-header__menu {
  display: none;
  margin-left: auto;
  border: 1px solid var(--studio-line-strong);
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
    right: max(1rem, env(safe-area-inset-right));
    left: max(1rem, env(safe-area-inset-left));
    flex-direction: column;
    align-items: stretch;
    border: 1px solid var(--studio-line);
    border-radius: 1rem;
    background: var(--studio-paper);
    box-shadow: var(--studio-shadow);
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
</style>
