import { createRouter, createWebHistory } from 'vue-router'

import Unlock from './views/Unlock.vue'
import Library from './views/Library.vue'

import { authState } from './lib/session'
import {
  protectedRouteDecision,
  unlockRouteDecision,
} from './lib/session-navigation'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      component: () => import('./views/LandingPage.vue'),
      props: {
        storiesHref: '/library',
        storyBaseHref: '/read',
      },
    },

    { path: '/unlock', component: Unlock },
    {
      path: '/session-unavailable',
      component: () => import('./views/SessionUnavailable.vue'),
    },

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

router.beforeEach(async (to) => {
  if (to.path === '/session-unavailable') return true

  const requires = to.matched.some((r) => r.meta.requiresUnlock)

  // If you're already unlocked, no need to sit on /unlock
  if (to.path === '/unlock') {
    const state = await authState.verify()
    return unlockRouteDecision(state, to.query.next)
  }

  if (!requires) return true

  const state = await authState.verify()
  return protectedRouteDecision(state, to.path)
})
