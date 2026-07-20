<script setup lang="ts">
defineProps<{
  eyebrow?: string
  title: string
  description?: string
}>()
</script>

<template>
  <div class="panda-auth-shell panda-print-surface">
    <a class="panda-auth-shell__skip" href="#panda-auth-main">Skip to main content</a>

    <div class="panda-auth-shell__frame">
      <header class="panda-auth-shell__brand" aria-label="Panda Pages">
        <img src="/logo.png" alt="" width="48" height="48" decoding="async" />
        <span>
          <strong>Panda Pages</strong>
          <small>Stories for curious readers</small>
        </span>
      </header>

      <main id="panda-auth-main" class="panda-auth-shell__main" tabindex="-1">
        <section
          class="panda-auth-shell__panel"
          aria-labelledby="panda-auth-title"
          :aria-describedby="description ? 'panda-auth-description' : undefined"
        >
          <p v-if="eyebrow" class="panda-auth-shell__eyebrow">{{ eyebrow }}</p>
          <h1 id="panda-auth-title">{{ title }}</h1>
          <p v-if="description" id="panda-auth-description" class="panda-auth-shell__description">
            {{ description }}
          </p>

          <div class="panda-auth-shell__content">
            <slot />
          </div>
        </section>
      </main>

      <footer v-if="$slots.footer" class="panda-auth-shell__footer">
        <slot name="footer" />
      </footer>
    </div>
  </div>
</template>

<style scoped>
.panda-auth-shell,
.panda-auth-shell *,
.panda-auth-shell *::before,
.panda-auth-shell *::after {
  box-sizing: border-box;
}

.panda-auth-shell {
  min-height: 100dvh;
  overflow-x: clip;
  background: var(--panda-paper);
  color: var(--panda-ink);
  color-scheme: light;
  font-family: var(--panda-sans);
  -webkit-font-smoothing: antialiased;
}

.panda-auth-shell :where(a, button, input):focus-visible {
  outline-color: var(--panda-focus);
}

.panda-auth-shell__skip {
  position: fixed;
  z-index: 100;
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

.panda-auth-shell__skip:focus {
  transform: none;
}

.panda-auth-shell__frame {
  position: relative;
  z-index: 1;
  display: flex;
  width: min(100%, 36rem);
  min-height: 100dvh;
  margin-inline: auto;
  flex-direction: column;
  justify-content: center;
  padding:
    max(clamp(1.25rem, 4vw, 2.5rem), env(safe-area-inset-top))
    max(clamp(0.85rem, 4vw, 1.5rem), env(safe-area-inset-right))
    max(clamp(1.25rem, 4vw, 2.5rem), env(safe-area-inset-bottom))
    max(clamp(0.85rem, 4vw, 1.5rem), env(safe-area-inset-left));
}

.panda-auth-shell__brand {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.panda-auth-shell__brand img {
  width: 3rem;
  height: 3rem;
  flex: 0 0 auto;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-white);
  object-fit: contain;
}

.panda-auth-shell__brand span {
  display: grid;
  min-width: 0;
}

.panda-auth-shell__brand strong {
  font-size: 1rem;
  font-weight: 850;
  letter-spacing: -0.025em;
}

.panda-auth-shell__brand small {
  color: var(--panda-muted);
  font-size: 0.76rem;
  font-weight: 650;
}

.panda-auth-shell__main {
  min-width: 0;
}

.panda-auth-shell__main:focus {
  outline: none;
}

.panda-auth-shell__main:focus-visible {
  outline: 3px solid var(--panda-focus);
  outline-offset: 0.4rem;
}

.panda-auth-shell__panel {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--panda-line-strong);
  border-radius: var(--panda-radius-card);
  padding: clamp(1rem, 5vw, 1.75rem);
  background: var(--panda-paper-raised);
  box-shadow: var(--panda-shadow);
}

.panda-auth-shell__panel::before {
  content: "";
  position: absolute;
  top: 0;
  right: 0;
  left: 0;
  height: 0.28rem;
  background: var(--panda-ink);
}

.panda-auth-shell__eyebrow {
  margin: 0 0 0.4rem;
  color: var(--panda-soft-ink);
  font-size: 0.75rem;
  font-weight: 850;
  letter-spacing: 0.11em;
  text-transform: uppercase;
}

.panda-auth-shell h1 {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--panda-ink);
  font-family: var(--panda-serif);
  font-size: clamp(1.75rem, 7vw, 2.45rem);
  font-weight: 680;
  letter-spacing: -0.035em;
  line-height: 1.12;
  text-wrap: balance;
}

.panda-auth-shell__description {
  margin: 0.65rem 0 0;
  color: var(--panda-muted);
  line-height: 1.55;
}

.panda-auth-shell__content {
  margin-top: 1.35rem;
}

.panda-auth-shell__footer {
  margin-top: 1rem;
  color: var(--panda-muted);
  font-size: 0.76rem;
  line-height: 1.5;
  text-align: center;
}

@media (max-height: 42rem) {
  .panda-auth-shell__frame {
    justify-content: flex-start;
  }
}

@media (forced-colors: active) {
  .panda-auth-shell {
    background: Canvas;
    color: CanvasText;
  }

  .panda-auth-shell__panel::before {
    display: none;
  }

  .panda-auth-shell__panel,
  .panda-auth-shell__brand img {
    border-color: CanvasText;
    background: Canvas;
    box-shadow: none;
  }
}
</style>
