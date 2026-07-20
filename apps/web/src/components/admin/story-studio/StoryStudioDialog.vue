<script setup lang="ts">
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'

withDefaults(
  defineProps<{
    open: boolean
    title: string
    description: string
    confirmLabel: string
    cancelLabel?: string
    busy?: boolean
    danger?: boolean
  }>(),
  {
    cancelLabel: 'Cancel',
    busy: false,
    danger: false,
  },
)

const emit = defineEmits<{
  confirm: []
  cancel: []
}>()
</script>

<template>
  <DialogRoot :open="open" @update:open="!$event && emit('cancel')">
    <DialogPortal>
      <DialogOverlay class="studio-dialog__overlay" />
      <DialogContent class="studio-dialog__content" @escape-key-down="busy && $event.preventDefault()">
        <div class="studio-dialog__mark" aria-hidden="true">PP</div>
        <DialogTitle class="studio-dialog__title">{{ title }}</DialogTitle>
        <DialogDescription class="studio-dialog__description">
          {{ description }}
        </DialogDescription>
        <div v-if="$slots.default" class="studio-dialog__body">
          <slot />
        </div>
        <div class="studio-dialog__actions">
          <button
            type="button"
            class="studio-button studio-button--quiet"
            :disabled="busy"
            @click="emit('cancel')"
          >
            {{ cancelLabel }}
          </button>
          <button
            type="button"
            class="studio-button"
            :class="danger ? 'studio-button--danger' : 'studio-button--primary'"
            :disabled="busy"
            @click="emit('confirm')"
          >
            {{ busy ? 'Working…' : confirmLabel }}
          </button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style>
.studio-dialog__overlay {
  position: fixed;
  z-index: 90;
  inset: 0;
  background: rgb(9 24 29 / 72%);
  backdrop-filter: blur(4px);
}

.studio-dialog__content {
  position: fixed;
  z-index: 91;
  top: 50%;
  left: 50%;
  width: min(31rem, calc(100vw - 2rem));
  max-height: min(38rem, calc(100dvh - 2rem));
  overflow: auto;
  transform: translate(-50%, -50%);
  border: 1px solid #d8ded8;
  border-radius: 1.4rem;
  background: #fffef9;
  color: #173338;
  box-shadow: 0 28px 80px rgb(9 24 29 / 28%);
  padding: 1.5rem;
  padding-bottom: max(1.5rem, env(safe-area-inset-bottom));
}

.studio-dialog__mark {
  display: grid;
  place-items: center;
  width: 2.25rem;
  height: 2.25rem;
  margin-bottom: 1rem;
  border-radius: 0.75rem;
  background: #dcefe8;
  color: #1d6258;
  font-size: 0.72rem;
  font-weight: 800;
  letter-spacing: 0.08em;
}

.studio-dialog__title {
  font-family: 'Literata Variable', Georgia, serif;
  font-size: clamp(1.25rem, 4vw, 1.6rem);
  font-weight: 650;
  line-height: 1.25;
}

.studio-dialog__description {
  margin-top: 0.55rem;
  color: #52686b;
  line-height: 1.55;
}

.studio-dialog__body {
  margin-top: 1rem;
  padding: 0.9rem 1rem;
  border-radius: 0.9rem;
  background: #f3f4ed;
  color: #435c5f;
  font-size: 0.92rem;
}

.studio-dialog__actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 0.65rem;
  margin-top: 1.4rem;
}

@media (max-height: 430px) {
  .studio-dialog__content {
    top: 1rem;
    transform: translateX(-50%);
  }
}

@media (prefers-reduced-motion: no-preference) {
  .studio-dialog__overlay,
  .studio-dialog__content {
    animation: studio-dialog-in 150ms ease-out;
  }
}

@keyframes studio-dialog-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>
