<script setup lang="ts">
import type { StoryStudioForm } from '@/lib/story-studio-form'

const props = defineProps<{
  modelValue: StoryStudioForm
  fixedSlug: boolean
  issuesByField: Readonly<Record<string, string[]>>
}>()

const emit = defineEmits<{
  'update:modelValue': [value: StoryStudioForm]
  'slug-input': []
}>()

function update<K extends keyof StoryStudioForm>(key: K, value: StoryStudioForm[K]) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
</script>

<template>
  <fieldset class="metadata-form">
    <legend>Story details</legend>
    <p class="metadata-form__intro">Metadata appears in Story Studio and, when published, in the reader.</p>

    <div class="metadata-form__grid">
      <div class="studio-field metadata-form__wide">
        <label for="story-title">Title <span aria-hidden="true">*</span></label>
        <input
          id="story-title"
          :value="modelValue.title"
          autocomplete="off"
          :aria-invalid="Boolean(issuesByField.title?.length)"
          :aria-describedby="issuesByField.title?.length ? 'story-title-error' : 'story-title-hint'"
          @input="update('title', ($event.target as HTMLInputElement).value)"
        />
        <p id="story-title-hint" class="studio-field__hint">The title readers will see.</p>
        <p v-if="issuesByField.title?.length" id="story-title-error" class="studio-field__error">{{ issuesByField.title[0] }}</p>
      </div>

      <div class="studio-field">
        <label for="story-author">Author <span>Optional</span></label>
        <input
          id="story-author"
          :value="modelValue.author"
          autocomplete="off"
          :aria-invalid="Boolean(issuesByField.author?.length)"
          @input="update('author', ($event.target as HTMLInputElement).value)"
        />
        <p v-if="issuesByField.author?.length" class="studio-field__error">{{ issuesByField.author[0] }}</p>
      </div>

      <div class="studio-field">
        <label for="story-slug">Slug</label>
        <input
          id="story-slug"
          :value="modelValue.slug"
          autocomplete="off"
          spellcheck="false"
          :readonly="fixedSlug"
          :aria-describedby="fixedSlug ? 'story-slug-fixed' : 'story-slug-hint'"
          :aria-invalid="Boolean(issuesByField.slug?.length)"
          @input="emit('slug-input'); update('slug', ($event.target as HTMLInputElement).value)"
        />
        <p :id="fixedSlug ? 'story-slug-fixed' : 'story-slug-hint'" class="studio-field__hint">
          {{ fixedSlug ? 'The canonical slug stays fixed for this story.' : 'Follows the title until you edit it.' }}
        </p>
        <p v-if="issuesByField.slug?.length" class="studio-field__error">{{ issuesByField.slug[0] }}</p>
      </div>

      <div class="studio-field">
        <label for="story-language">Language</label>
        <input
          id="story-language"
          :value="modelValue.language"
          autocomplete="off"
          spellcheck="false"
          :aria-invalid="Boolean(issuesByField.language?.length)"
          @input="update('language', ($event.target as HTMLInputElement).value)"
        />
        <p class="studio-field__hint">For example, en-GB or cy.</p>
        <p v-if="issuesByField.language?.length" class="studio-field__error">{{ issuesByField.language[0] }}</p>
      </div>

      <div class="studio-field">
        <label for="story-rights">Rights</label>
        <input
          id="story-rights"
          :value="modelValue.rightsLabel"
          autocomplete="off"
          placeholder="Public domain"
          :aria-invalid="Boolean(issuesByField.rights?.length)"
          @input="update('rightsLabel', ($event.target as HTMLInputElement).value)"
        />
        <p class="studio-field__hint">Existing additional rights metadata is retained.</p>
        <p v-if="issuesByField.rights?.length" class="studio-field__error">{{ issuesByField.rights[0] }}</p>
      </div>

      <div class="studio-field metadata-form__wide">
        <label for="story-source-url">Source URL <span>Optional</span></label>
        <input
          id="story-source-url"
          type="url"
          :value="modelValue.sourceUrl"
          autocomplete="url"
          placeholder="https://example.org/source"
          :aria-invalid="Boolean(issuesByField.sourceUrl?.length)"
          @input="update('sourceUrl', ($event.target as HTMLInputElement).value)"
        />
        <p v-if="issuesByField.sourceUrl?.length" class="studio-field__error">{{ issuesByField.sourceUrl[0] }}</p>
      </div>
    </div>
  </fieldset>
</template>

<style scoped>
.metadata-form {
  border: 0;
  padding: 0;
}

.metadata-form legend {
  color: var(--studio-ink);
  font-family: var(--panda-serif);
  font-size: 1.15rem;
  font-weight: 650;
}

.metadata-form__intro {
  margin-top: 0.25rem;
  color: var(--studio-muted);
  font-size: 0.88rem;
}

.metadata-form__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
  margin-top: 1.2rem;
}

.metadata-form__wide { grid-column: 1 / -1; }

@media (max-width: 620px) {
  .metadata-form__grid { grid-template-columns: 1fr; }
  .metadata-form__wide { grid-column: auto; }
}
</style>
