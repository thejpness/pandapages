<script setup lang="ts">
import { nextTick, watch } from 'vue'
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'

const props = withDefaults(defineProps<{
  open: boolean
  contentId: string
  title: string
  description: string
  wide?: boolean
  showClose?: boolean
}>(), {
  wide: false,
  showClose: true,
})

const emit = defineEmits<{ 'update:open': [open: boolean] }>()
let returnFocus: HTMLElement | null = null

watch(
  () => props.open,
  async (open, previous) => {
    if (open && !previous) {
      returnFocus =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null
      return
    }
    if (!open && previous) {
      await nextTick()
      returnFocus?.focus({ preventScroll: true })
      returnFocus = null
    }
  },
)
</script>

<template>
  <DialogRoot :open="open" @update:open="emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="reader-dialog-overlay" />
      <DialogContent
        :id="contentId"
        class="reader-dialog-content"
        :class="{ 'reader-dialog-content--wide': wide }"
      >
        <div class="reader-dialog-heading">
          <div>
            <DialogTitle class="reader-dialog-title">{{ title }}</DialogTitle>
            <DialogDescription class="reader-dialog-description">
              {{ description }}
            </DialogDescription>
          </div>
          <DialogClose v-if="showClose" as-child>
            <button class="reader-button reader-button--quiet" type="button">
              Close
            </button>
          </DialogClose>
        </div>
        <slot />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
