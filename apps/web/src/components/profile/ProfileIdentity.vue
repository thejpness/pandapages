<script setup lang="ts">
import { computed } from 'vue'
import { profilePresentationV1 } from '../../lib/profile-presentation'

const props = defineProps<{ profileID: string }>()

const presentation = computed(() => profilePresentationV1(props.profileID))
</script>

<template>
  <span
    class="profile-identity"
    :class="`profile-identity--${presentation.pattern}`"
    :style="{
      '--profile-identity-surface': presentation.surface,
      '--profile-identity-accent': presentation.accent,
      '--profile-identity-ink': presentation.ink,
    }"
    aria-hidden="true"
  >
    <i class="profile-identity__pattern"></i>
    <i class="profile-identity__face">
      <b></b>
      <b></b>
      <em></em>
    </i>
  </span>
</template>

<style scoped>
.profile-identity {
  position: relative;
  display: inline-grid;
  width: var(--profile-identity-size, 5.25rem);
  aspect-ratio: 1;
  place-items: center;
  overflow: hidden;
  flex: 0 0 auto;
  border: 1px solid color-mix(in srgb, var(--profile-identity-ink) 42%, transparent);
  border-radius: 1.35rem;
  background: var(--profile-identity-surface);
  color: var(--profile-identity-ink);
  box-shadow: 0.3rem 0.35rem 0 color-mix(in srgb, var(--profile-identity-ink) 13%, transparent);
}

.profile-identity__pattern {
  position: absolute;
  inset: 0;
  opacity: 0.68;
}

.profile-identity--dots .profile-identity__pattern {
  background: radial-gradient(circle, var(--profile-identity-accent) 0 0.16rem, transparent 0.18rem) 0 0 / 1rem 1rem;
}

.profile-identity--arches .profile-identity__pattern {
  background: radial-gradient(circle at 50% 100%, transparent 0 28%, var(--profile-identity-accent) 29% 34%, transparent 35%) 0 0 / 2rem 2rem;
}

.profile-identity--rays .profile-identity__pattern {
  inset: -35%;
  background: repeating-conic-gradient(from 10deg, transparent 0 13deg, var(--profile-identity-accent) 14deg 25deg);
}

.profile-identity--checks .profile-identity__pattern {
  background: linear-gradient(45deg, var(--profile-identity-accent) 25%, transparent 25% 75%, var(--profile-identity-accent) 75%) 0 0 / 1.35rem 1.35rem, linear-gradient(45deg, var(--profile-identity-accent) 25%, transparent 25% 75%, var(--profile-identity-accent) 75%) 0.675rem 0.675rem / 1.35rem 1.35rem;
}

.profile-identity--rings .profile-identity__pattern {
  background: repeating-radial-gradient(circle at 50% 50%, transparent 0 0.4rem, var(--profile-identity-accent) 0.43rem 0.52rem, transparent 0.55rem 0.85rem);
}

.profile-identity--steps .profile-identity__pattern {
  background: linear-gradient(135deg, transparent 0 38%, var(--profile-identity-accent) 39% 50%, transparent 51%) 0 0 / 1.4rem 1.4rem;
}

.profile-identity--leaves .profile-identity__pattern {
  background: radial-gradient(ellipse 28% 48% at 20% 20%, var(--profile-identity-accent) 0 48%, transparent 51%) 0 0 / 1.45rem 1.45rem;
}

.profile-identity--waves .profile-identity__pattern {
  background: repeating-radial-gradient(ellipse at 50% 120%, transparent 0 35%, var(--profile-identity-accent) 36% 41%, transparent 42% 60%) 0 0 / 1.7rem 1.05rem;
}

.profile-identity__face {
  position: relative;
  z-index: 1;
  display: block;
  width: 54%;
  aspect-ratio: 1.08;
  border: 2px solid currentColor;
  border-radius: 46% 46% 43% 43%;
  background: color-mix(in srgb, var(--profile-identity-surface) 78%, white);
}

.profile-identity__face::before,
.profile-identity__face::after {
  position: absolute;
  top: -18%;
  width: 34%;
  aspect-ratio: 1;
  border: 2px solid currentColor;
  border-radius: 45% 50% 43% 50%;
  background: var(--profile-identity-ink);
  content: '';
}

.profile-identity__face::before { left: -13%; }
.profile-identity__face::after { right: -13%; }

.profile-identity__face b {
  position: absolute;
  top: 39%;
  width: 18%;
  aspect-ratio: 1;
  border-radius: 50%;
  background: currentColor;
}

.profile-identity__face b:first-child { left: 23%; }
.profile-identity__face b:nth-child(2) { right: 23%; }

.profile-identity__face em {
  position: absolute;
  bottom: 21%;
  left: 50%;
  width: 17%;
  aspect-ratio: 1.35;
  border-radius: 50% 50% 58% 58%;
  background: currentColor;
  transform: translateX(-50%);
}

@media (forced-colors: active) {
  .profile-identity {
    border-color: CanvasText;
    background: Canvas;
    color: CanvasText;
    box-shadow: none;
  }

  .profile-identity__pattern { display: none; }
  .profile-identity__face { background: Canvas; }
  .profile-identity__face::before,
  .profile-identity__face::after,
  .profile-identity__face b,
  .profile-identity__face em { background: CanvasText; }
}
</style>
