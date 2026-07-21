import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { loadTypeScript } from './helpers/typescript-module.mjs'

test('installed-shell notice does not promise offline library, stories or progress', async () => {
  const source = await readFile(new URL('../src/App.vue', import.meta.url), 'utf8')

  assert.match(source, /Installed and ready to open\./)
  assert.match(
    source,
    /Your library, stories and reading progress need an internet connection\./,
  )
  assert.doesNotMatch(source, /ready to use offline/i)
  assert.doesNotMatch(source, /offline reading/i)
})

async function routePolicy() {
  const { module } = await loadTypeScript(
    '../vite-route-policy.ts',
    import.meta.url,
  )
  return module
}

test('standalone Vite proxies readiness through the same API target as liveness', async () => {
  const policy = await routePolicy()
  const proxy = policy.createDevelopmentProxy('http://api.internal:8080')
  const config = await readFile(new URL('../vite.config.ts', import.meta.url), 'utf8')

  assert.match(config, /proxy: createDevelopmentProxy\(devProxyTarget\)/)
  assert.deepEqual(proxy['/readyz'], proxy['/healthz'])
  assert.deepEqual(proxy['/readyz'], {
    target: 'http://api.internal:8080',
    changeOrigin: true,
  })
})

test('service-worker navigation fallback excludes readiness with or without a query', async () => {
  const policy = await routePolicy()
  const denylist = policy.createNavigationFallbackDenylist()
  const isDenied = (path) => denylist.some((pattern) => pattern.test(path))
  const config = await readFile(new URL('../vite.config.ts', import.meta.url), 'utf8')

  assert.match(
    config,
    /navigateFallbackDenylist: createNavigationFallbackDenylist\(\)/,
  )
  assert.equal(isDenied('/healthz'), true)
  assert.equal(isDenied('/readyz'), true)
  assert.equal(isDenied('/readyz?probe=manual'), true)
  assert.equal(isDenied('/readyz/spa-route'), false)
})
