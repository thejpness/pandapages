import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import {
  createDevelopmentProxy,
  createNavigationFallbackDenylist,
} from './vite-route-policy'

export default defineConfig(({ mode }) => {
  // Load env for dev proxy only (build-time vars are still handled by Vite normally)
  const env = loadEnv(mode, process.cwd(), 'VITE_')

  // In dev you can set VITE_API_BASE=http://pandapages.localhost (or http://localhost:8081)
  // In prod you should prefer same-origin (BASE = ''), so the app calls /api/... on panda-pages.com
  const apiBase = (env.VITE_API_BASE || '').trim()

  const devProxyTarget = apiBase || 'http://localhost:8080'

  return {
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },

    // DEV ONLY: proxy so your frontend can just call "/api/..." and still hit the API
    // Keeps cookies sane and avoids CORS headaches.
    server: {
      proxy: createDevelopmentProxy(devProxyTarget),
    },

    plugins: [
      vue(),
      VitePWA({
        registerType: 'prompt',
        includeAssets: ['logo.png'],
        manifest: {
          id: '/',
          name: 'Panda Pages',
          short_name: 'PandaPages',
          start_url: '/',
          scope: '/',
          display: 'standalone',
          display_override: ['standalone', 'minimal-ui', 'browser'],
          background_color: '#0B1724',
          theme_color: '#0B1724',
          icons: [
            { src: '/logo.png', sizes: '192x192', type: 'image/png' },
            { src: '/logo.png', sizes: '512x512', type: 'image/png' },
          ],
        },

        // Helps offline navigation for history-mode routes (/admin/upload, /read/:slug)
        // (nginx fallback still required for normal online refreshes)
        workbox: {
          cleanupOutdatedCaches: true,
          navigateFallback: '/index.html',
          navigateFallbackDenylist: createNavigationFallbackDenylist(),
        },
      }),
      tailwindcss(),
    ],
  }
})
