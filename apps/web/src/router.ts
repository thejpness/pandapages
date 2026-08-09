import { createRouter, createWebHistory } from "vue-router";

import {
  AccountContextError,
  currentAccountContext,
} from "./lib/account-context";
import {
  ProfileContextError,
  currentReaderProfileContext,
} from "./lib/profile-context";
import { resolveReaderDestination } from "./lib/profile-destination";
import { enterChildMode, isChildMode, isChildModeFor } from "./lib/reader-mode";

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
      meta: { parentOnly: true },
    },
    {
      path: "/profiles",
      component: () => import("./views/Profiles.vue"),
      meta: { requiresAccount: true, parentOnly: true },
    },

    {
      path: "/library",
      component: () => import("./views/Library.vue"),
      meta: { requiresAccount: true, requiresProfile: true, requiresChildMode: true },
    },
    {
      path: "/read/:slug",
      component: () => import("./views/Reader.vue"),
      props: true,
      meta: { requiresAccount: true, requiresProfile: true, requiresChildMode: true },
    },
    {
      path: "/admin",
      component: () => import("./views/admin/AdminLayout.vue"),
      meta: { requiresAccount: true, parentOnly: true },
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
          path: "stories/ingest",
          name: "admin-story-ingest",
          component: () => import("./views/admin/StoryStudioEditionIngest.vue"),
        },
        {
          path: "stories/:slug/edit",
          name: "admin-story-edit",
          component: () => import("./views/admin/StoryStudioEditor.vue"),
        },
        {
          path: "stories/:slug/source",
          name: "admin-story-source",
          component: () => import("./views/admin/StoryStudioSourceEditor.vue"),
        },
        {
          path: "stories/:slug/ingest",
          name: "admin-story-ingest-existing",
          component: () => import("./views/admin/StoryStudioEditionIngest.vue"),
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
  const requiresAccount = to.matched.some((route) => route.meta.requiresAccount);
  const parentOnly = to.matched.some((route) => route.meta.parentOnly);

  if (!requiresAccount) {
    return parentOnly && isChildMode() ? "/library" : true;
  }

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

  if (parentOnly && isChildMode()) {
    return "/library";
  }

  if (!to.matched.some((route) => route.meta.requiresProfile)) return true;
  try {
    const context = await currentReaderProfileContext();
    if (
      to.matched.some((route) => route.meta.requiresChildMode) &&
      !isChildModeFor(context.profile.id)
    ) {
      if (!context.profile.pinEnabled) {
        enterChildMode(context.profile.id);
        return true;
      }
      return { path: "/profiles", query: { next: to.fullPath } };
    }
    return true;
  } catch (error) {
    if (error instanceof ProfileContextError) {
      return {
        path: "/profiles",
        query: {
          next: resolveReaderDestination(to.fullPath),
          ...(error.kind === "unavailable" ? { unavailable: "1" } : {}),
        },
      };
    }
    return { path: "/account/login", query: { next: to.fullPath } };
  }
});
