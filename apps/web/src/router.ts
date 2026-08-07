import { createRouter, createWebHistory } from "vue-router";

import {
  AccountContextError,
  currentAccountContext,
} from "./lib/account-context";
import {
  ProfileContextError,
  currentReaderProfileContext,
} from "./lib/profile-context";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      component: () => import("./views/LandingPage.vue"),
      props: {
        storiesHref: "/library",
        storyBaseHref: "/read",
      },
    },

    {
      path: "/account/login",
      component: () => import("./views/SupabaseLogin.vue"),
    },
    {
      path: "/auth/callback",
      component: () => import("./views/SupabaseCallback.vue"),
    },
    {
      path: "/account",
      component: () => import("./views/SupabaseIdentity.vue"),
    },
    {
      path: "/profiles",
      component: () => import("./views/Profiles.vue"),
      meta: { requiresAccount: true },
    },

    {
      path: "/library",
      component: () => import("./views/Library.vue"),
      meta: { requiresAccount: true, requiresProfile: true },
    },
    {
      path: "/read/:slug",
      component: () => import("./views/Reader.vue"),
      props: true,
      meta: { requiresAccount: true, requiresProfile: true },
    },
    {
      path: "/journey",
      component: () => import("./views/Journey.vue"),
      meta: { requiresAccount: true },
    },

    {
      path: "/admin",
      component: () => import("./views/admin/AdminLayout.vue"),
      meta: { requiresAccount: true },
      children: [
        { path: "", redirect: { name: "admin-stories" } },
        { path: "upload", redirect: { name: "admin-story-new" } },
        {
          path: "stories",
          name: "admin-stories",
          component: () => import("./views/admin/StoryStudioList.vue"),
        },
        {
          path: "stories/new",
          name: "admin-story-new",
          component: () => import("./views/admin/StoryStudioEditor.vue"),
        },
        {
          path: "stories/:slug/edit",
          name: "admin-story-edit",
          component: () => import("./views/admin/StoryStudioEditor.vue"),
        },
        {
          path: "stories/:slug",
          name: "admin-story-detail",
          component: () => import("./views/admin/StoryStudioDetail.vue"),
        },
        { path: "ai", component: () => import("./views/admin/AdminAI.vue") },
      ],
    },
  ],
});

router.beforeEach(async (to) => {
  if (!to.matched.some((route) => route.meta.requiresAccount)) return true;
  try {
    await currentAccountContext();
  } catch (error) {
    if (
      error instanceof AccountContextError &&
      error.kind === "account_selection_required"
    )
      return "/account";
    return { path: "/account/login", query: { next: to.fullPath } };
  }

  if (!to.matched.some((route) => route.meta.requiresProfile)) return true;
  try {
    await currentReaderProfileContext();
    return true;
  } catch (error) {
    if (error instanceof ProfileContextError) {
      return {
        path: "/profiles",
        query: {
          next: to.fullPath,
          ...(error.kind === "unavailable" ? { unavailable: "1" } : {}),
        },
      };
    }
    return { path: "/account/login", query: { next: to.fullPath } };
  }
});
