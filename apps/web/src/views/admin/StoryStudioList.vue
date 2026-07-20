<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import StoryCatalogue from '@/components/admin/story-studio/StoryCatalogue.vue'
import StoryStudioState from '@/components/admin/story-studio/StoryStudioState.vue'
import {
  adminListStories,
  type AdminStoryListItem,
  type AdminStoryStatus,
} from '@/lib/api'
import { authState } from '@/lib/session'
import {
  filterStoryCatalogue,
  projectStoryStudioError,
  storyStatusLabel,
  storyStatusOrder,
  type StoryStudioError,
} from '@/lib/story-studio-navigation'

const router = useRouter()
const stories = ref<AdminStoryListItem[]>([])
const loading = ref(true)
const error = ref<StoryStudioError | null>(null)
const query = ref('')
const status = ref<AdminStoryStatus | 'all'>('all')
let loadGeneration = 0
let controller: AbortController | null = null

const visibleStories = computed(() =>
  filterStoryCatalogue(stories.value, query.value, status.value),
)

async function handleSessionEnded() {
  authState.confirmLocked()
  await router.replace({
    path: '/unlock',
    query: { next: '/admin/stories' },
  })
}

async function loadStories() {
  controller?.abort()
  controller = new AbortController()
  const generation = ++loadGeneration
  loading.value = stories.value.length === 0
  error.value = null
  try {
    const response = await adminListStories(controller.signal)
    if (generation !== loadGeneration) return
    stories.value = response.items
  } catch (caught) {
    if (controller.signal.aborted || generation !== loadGeneration) return
    const projected = projectStoryStudioError(caught)
    error.value = projected
    if (projected.kind === 'session') await handleSessionEnded()
  } finally {
    if (generation === loadGeneration) loading.value = false
  }
}

function openStory(slug: string) {
  void router.push({ name: 'admin-story-detail', params: { slug } })
}

onMounted(loadStories)
onBeforeUnmount(() => {
  loadGeneration += 1
  controller?.abort()
})
</script>

<template>
  <div>
    <header class="studio-page-heading">
      <div>
        <p class="studio-page-heading__eyebrow">Story Studio</p>
        <h1>Stories</h1>
        <p class="studio-page-heading__summary">
          Create, review and publish the stories in your Panda Pages catalogue.
        </p>
      </div>
      <button type="button" class="studio-button studio-button--primary" @click="router.push('/admin/stories/new')">
        New story
      </button>
    </header>

    <section class="catalogue-tools studio-panel" aria-labelledby="catalogue-tools-title">
      <h2 id="catalogue-tools-title" class="studio-visually-hidden">Find stories</h2>
      <div class="studio-field catalogue-tools__search">
        <label for="studio-story-search">Search stories</label>
        <input
          id="studio-story-search"
          v-model="query"
          type="search"
          autocomplete="off"
          placeholder="Title, author or slug"
        />
      </div>
      <div class="studio-field">
        <label for="studio-status-filter">Status</label>
        <select id="studio-status-filter" v-model="status">
          <option value="all">All statuses</option>
          <option v-for="value in storyStatusOrder" :key="value" :value="value">
            {{ storyStatusLabel(value) }}
          </option>
        </select>
      </div>
      <p class="catalogue-tools__count">
        {{ visibleStories.length }} {{ visibleStories.length === 1 ? 'story' : 'stories' }}
      </p>
    </section>

    <StoryStudioState
      v-if="loading"
      kind="loading"
      title="Loading stories"
      message="Story Studio is opening your catalogue."
    />
    <StoryStudioState
      v-else-if="error && stories.length === 0"
      :kind="error.kind === 'forbidden' ? 'forbidden' : error.kind === 'session' ? 'session' : 'error'"
      :title="error.title"
      :message="error.message"
      :action-label="error.retryable ? 'Try again' : undefined"
      @action="loadStories"
    />
    <template v-else>
      <div v-if="error" class="catalogue-error" role="alert">
        <div><strong>{{ error.title }}</strong><p>{{ error.message }}</p></div>
        <button v-if="error.retryable" type="button" class="studio-button studio-button--quiet" @click="loadStories">Try again</button>
      </div>

      <StoryStudioState
        v-if="stories.length === 0"
        kind="empty"
        title="Create your first story"
        message="Start with Markdown or import a text, Markdown or HTML file. Nothing is published until you choose Publish."
        action-label="Create story"
        @action="router.push('/admin/stories/new')"
      />
      <StoryStudioState
        v-else-if="visibleStories.length === 0"
        kind="empty"
        title="No stories match"
        message="Try another search or status filter."
        action-label="Clear filters"
        @action="query = ''; status = 'all'"
      />
      <StoryCatalogue v-else :stories="visibleStories" @open="openStory" />
    </template>
  </div>
</template>

<style scoped>
.catalogue-tools {
  display: grid;
  grid-template-columns: minmax(12rem, 1fr) minmax(11rem, 15rem) auto;
  align-items: end;
  gap: 1rem;
  margin-bottom: 1.25rem;
}

.catalogue-tools__count {
  min-width: 5rem;
  padding-bottom: 0.75rem;
  color: var(--studio-muted);
  font-size: 0.85rem;
  text-align: right;
}

.catalogue-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
  border: 1px solid var(--panda-danger);
  border-radius: var(--panda-radius-compact);
  background: var(--panda-danger-surface);
  padding: 0.9rem 1rem;
  color: var(--panda-danger);
}

.catalogue-error p { margin-top: 0.2rem; font-size: 0.88rem; }

@media (max-width: 680px) {
  .catalogue-tools { grid-template-columns: 1fr; }
  .catalogue-tools__count { padding: 0; text-align: left; }
  .catalogue-error { align-items: stretch; flex-direction: column; }
}
</style>
