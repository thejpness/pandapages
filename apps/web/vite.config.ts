import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { VitePWA } from 'vite-plugin-pwa'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig(({ mode }) => {
  // Load env for dev proxy only (build-time vars are still handled by Vite normally)
  const env = loadEnv(mode, process.cwd(), '')

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
      proxy: {
        '/api': { target: devProxyTarget, changeOrigin: true },
        '/assets': { target: devProxyTarget, changeOrigin: true },
        '/healthz': { target: devProxyTarget, changeOrigin: true },
      },
    },

    plugins: [
      vue(),
      VitePWA({
        registerType: 'prompt',
        includeAssets: ['logo.png', 'apple-touch-icon.png'],
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
          navigateFallback: '/index.html',
          navigateFallbackDenylist: [
            /^\/api\//,
            /^\/assets\//,
            /^\/healthz$/,
          ],

          runtimeCaching: [
            // Cache story/library endpoints only (avoid caching auth/admin/progress/settings etc.)
            {
              urlPattern: ({ url, request }) => {
                if (request.method !== 'GET') return false
                if (!url.pathname.startsWith('/api/v1/')) return false

                // NEVER cache auth/admin/private state
                if (url.pathname.startsWith('/api/v1/auth/')) return false
                if (url.pathname.startsWith('/api/v1/admin/')) return false
                if (url.pathname.startsWith('/api/v1/progress/')) return false
                if (url.pathname.startsWith('/api/v1/settings')) return false
                if (url.pathname.startsWith('/api/v1/continue')) return false

                // Cache “content-ish” endpoints
                if (url.pathname === '/api/v1/library') return true
                if (url.pathname.startsWith('/api/v1/story/')) return true

                return false
              },
              handler: 'NetworkFirst',
              options: {
                cacheName: 'api-content',
                networkTimeoutSeconds: 3,
                expiration: { maxEntries: 200, maxAgeSeconds: 60 * 60 * 24 * 7 }, // 7 days
              },
            },

            // Cache assets (cover images / rendered assets)
            {
              urlPattern: ({ url, request }) =>
                request.method === 'GET' && url.pathname.startsWith('/assets/'),
              handler: 'CacheFirst',
              options: {
                cacheName: 'assets',
                expiration: { maxEntries: 500, maxAgeSeconds: 60 * 60 * 24 * 30 }, // 30 days
              },
            },
          ],
        },
      }),
      tailwindcss(),
    ],
  }
})
