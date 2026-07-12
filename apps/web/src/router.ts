import { createRouter, createWebHistory } from 'vue-router'

import Unlock from './views/Unlock.vue'
import Library from './views/Library.vue'

import { authStatus } from './lib/api'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/library' },

    { path: '/unlock', component: Unlock },

    { path: '/library', component: Library, meta: { requiresUnlock: true } },
    { path: '/read/:slug', component: () => import('./views/Reader.vue'), props: true, meta: { requiresUnlock: true } },
    { path: '/journey', component: () => import('./views/Journey.vue'), meta: { requiresUnlock: true } },

    {
      path: '/admin',
      component: () => import('./views/admin/AdminLayout.vue'),
      meta: { requiresUnlock: true },
      children: [
        { path: '', redirect: { path: 'upload' } },
        { path: 'upload', component: () => import('./views/admin/AdminUpload.vue') },
        { path: 'ai', component: () => import('./views/admin/AdminAI.vue') },
      ],
    },
  ],
})

/* --------------------------- Unlock guard --------------------------- */

// tiny cache to avoid hammering /auth/status on every route change
let cachedAuth: { unlocked: boolean; at: number } | null = null
const AUTH_TTL_MS = 5000

async function getUnlocked(): Promise<boolean> {
  const now = Date.now()
  if (cachedAuth && now - cachedAuth.at < AUTH_TTL_MS) return cachedAuth.unlocked

  const { unlocked } = await authStatus()
  cachedAuth = { unlocked, at: now }
  return unlocked
}

router.beforeEach(async (to) => {
  const requires = to.matched.some((r) => r.meta.requiresUnlock)

  // If you're already unlocked, no need to sit on /unlock
  if (to.path === '/unlock') {
    const unlocked = await getUnlocked()
    if (unlocked) {
      const next = typeof to.query.next === 'string' ? to.query.next : '/library'
      return { path: next }
    }
    return true
  }

  if (!requires) return true

  const unlocked = await getUnlocked()
  if (!unlocked) {
    return { path: '/unlock', query: { next: to.fullPath } }
  }

  return true
})
