import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

async function loadAPI() {
  const { module, source } = await loadTypeScript(
    '../src/lib/api.ts',
    import.meta.url,
    (value) => value.replaceAll('import.meta.env.VITE_API_BASE', "''"),
  )
  return { api: module, source }
}

function jsonResponse(body) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

test('admin list uses the fixed same-origin path and browser credentials', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })

  let request
  globalThis.fetch = async (url, init) => {
    request = { url, init }
    return jsonResponse({ items: [] })
  }

  const { api, source } = await loadAPI()
  assert.doesNotMatch(source, /VITE_ADMIN_KEY|X-PP-Admin-Key/)
  assert.deepEqual(await api.adminListStories(), { items: [] })
  assert.equal(request.url, '/api/v1/admin/stories')
  assert.equal(request.init.credentials, 'include')

  const headers = new Headers(request.init.headers)
  assert.equal(headers.has('Authorization'), false)
  assert.equal(headers.has('X-PP-Admin-Key'), false)
})

test('UTF-8 imported text is sent unchanged to the fixed draft path', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })

  let request
  globalThis.fetch = async (url, init) => {
    request = { url, init }
    return jsonResponse({
      storyId: 'story-id',
      storyVersionId: 'version-id',
      slug: 'cafe-panda',
      version: 1,
      segmentsCount: 2,
      renderedHtml: '<h1>Café Panda 🐼</h1>',
    })
  }

  const { api } = await loadAPI()
  const newline = String.fromCodePoint(10)
  const markdown = '# Café Panda 🐼' + newline + newline + '“Olá”, said the panda. 你好。' + newline
  await api.adminDraftUpsertStory({
    slug: 'cafe-panda',
    title: 'Café Panda 🐼',
    markdown,
  })

  assert.equal(request.url, '/api/v1/admin/stories/draft')
  assert.equal(request.init.method, 'POST')
  assert.equal(request.init.credentials, 'include')
  assert.deepEqual(JSON.parse(String(request.init.body)), {
    slug: 'cafe-panda',
    title: 'Café Panda 🐼',
    markdown,
  })
})

test('Compose keeps browser credentials out and proxy credentials server-side', async () => {
  const productionCompose = await readFile(
    new URL('../../../docker-compose.yml', import.meta.url),
    'utf8'
  )
  const developmentCompose = await readFile(
    new URL('../../../docker-compose.dev.yml', import.meta.url),
    'utf8'
  )
  const requiredAllowlist =
    'ipallowlist.sourcerange=' +
    '$' +
    '{PP_ADMIN_IPS:?PP_ADMIN_IPS must contain the authorised admin CIDR(s)}'

  assert.match(
    productionCompose,
    /pandapages-api-admin\.middlewares=pandapages-admin-ips@docker,pandapages-admin-key@docker/
  )
  assert.ok(productionCompose.includes(requiredAllowlist))
  assert.match(productionCompose, /PP_COOKIE_SECURE: "true"/)
  assert.doesNotMatch(productionCompose, /VITE_ADMIN_KEY/)

  assert.match(
    developmentCompose,
    /api-admin\.middlewares=pandapages-dev-admin-key@docker/
  )
  assert.match(
    developmentCompose,
    /pandapages-dev-admin-key\.headers\.customrequestheaders\.X-PP-Admin-Key=/
  )
  assert.doesNotMatch(developmentCompose, /VITE_ADMIN_KEY/)
})

test('PWA caches static assets only and protected routes are split', async () => {
  const viteConfig = await readFile(
    new URL('../vite.config.ts', import.meta.url),
    'utf8'
  )

  assert.match(viteConfig, /loadEnv\(mode, process\.cwd\(\), 'VITE_'\)/)
  assert.match(viteConfig, /cleanupOutdatedCaches: true/)
  assert.doesNotMatch(viteConfig, /runtimeCaching/)
  assert.doesNotMatch(viteConfig, /api-content/)
  assert.doesNotMatch(viteConfig, /\/api\/v1\/library/)

  const routerSource = await readFile(
    new URL('../src/router.ts', import.meta.url),
    'utf8'
  )
  assert.doesNotMatch(routerSource, /^import (Reader|Journey|Admin)/m)
  for (const modulePath of [
    './views/Reader.vue',
    './views/Journey.vue',
    './views/admin/AdminLayout.vue',
    './views/admin/AdminUpload.vue',
    './views/admin/AdminAI.vue',
  ]) {
    assert.ok(routerSource.includes(`import('${modulePath}')`))
  }
})
