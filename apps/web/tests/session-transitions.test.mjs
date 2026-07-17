import assert from 'node:assert/strict'
import { readFile, readdir } from 'node:fs/promises'
import test from 'node:test'
import { loadTypeScript as loadModule } from './helpers/typescript-module.mjs'

const loadTypeScript = (relativePath, transform) =>
  loadModule(relativePath, import.meta.url, transform)

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

test('confirmed unlock replaces a cached signed-out result', async () => {
  const { module } = await loadTypeScript('../src/lib/auth-state.ts')
  let checks = 0
  const state = module.createAuthStateCache(async () => {
    checks += 1
    return false
  })

  assert.equal(await state.verify(), 'locked')
  state.confirmUnlocked()
  assert.equal(state.current().state, 'unlocked')
  assert.equal(await state.verify(), 'unlocked')
  assert.equal(checks, 1)
})

test('an older in-flight signed-out check cannot undo confirmed unlock', async () => {
  const { module } = await loadTypeScript('../src/lib/auth-state.ts')
  let checks = 0
  let resolveCheck
  const check = new Promise((resolve) => {
    resolveCheck = resolve
  })
  const state = module.createAuthStateCache(async () => {
    checks += 1
    return check
  })

  const pending = state.verify()
  state.confirmUnlocked()
  resolveCheck(false)

  assert.equal(await pending, 'unlocked')
  assert.equal(await state.verify(), 'unlocked')
  assert.equal(checks, 1)
})

test('confirmed logout replaces cached authenticated state synchronously', async () => {
  const { module } = await loadTypeScript('../src/lib/auth-state.ts')
  let checks = 0
  const state = module.createAuthStateCache(async () => {
    checks += 1
    return true
  })

  assert.equal(await state.verify(), 'unlocked')
  state.confirmLocked()
  assert.equal(state.current().state, 'locked')
  assert.equal(await state.verify(), 'locked')
  assert.equal(checks, 1)
})

test('an older in-flight authenticated check cannot undo confirmed logout', async () => {
  const { module } = await loadTypeScript('../src/lib/auth-state.ts')
  let resolveCheck
  const check = new Promise((resolve) => {
    resolveCheck = resolve
  })
  const state = module.createAuthStateCache(async () => check)

  const pending = state.verify()
  state.confirmLocked()
  resolveCheck(true)

  assert.equal(await pending, 'locked')
  assert.equal(state.current().state, 'locked')
})

test('verification failures remain unavailable and retry can recover', async () => {
  const { module } = await loadTypeScript('../src/lib/auth-state.ts')
  let available = false
  let checks = 0
  const state = module.createAuthStateCache(async () => {
    checks += 1
    if (!available) throw new Error('unavailable')
    return true
  })

  assert.equal(await state.verify(), 'unavailable')
  assert.equal(state.current().state, 'unavailable')
  assert.equal(await state.verify(), 'unavailable')
  assert.equal(checks, 1)

  available = true
  assert.equal(await state.retry(), 'unlocked')
  assert.equal(checks, 2)
})

test('auth status preserves transport and server failures instead of returning signed out', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })

  const { module: api } = await loadTypeScript(
    '../src/lib/api.ts',
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''")
  )

  globalThis.fetch = async () => {
    throw new TypeError('network unavailable')
  }
  await assert.rejects(api.authStatus, /network unavailable/)

  globalThis.fetch = async () =>
    jsonResponse(
      { error: { code: 'session_unavailable', message: 'session validation unavailable' } },
      503
    )
  await assert.rejects(api.authStatus, (error) => error.status === 503)

  globalThis.fetch = async () =>
    jsonResponse({ error: { code: 'unauthorized', message: 'unlock required' } }, 401)
  const unauthorized = await api.authStatus().catch((error) => error)
  assert.equal(api.getAPIErrorStatus(unauthorized), 401)

  globalThis.fetch = async () => jsonResponse({ unlocked: false })
  assert.deepEqual(await api.authStatus(), { unlocked: false })

  globalThis.fetch = async () => jsonResponse({ ok: true })
  await assert.rejects(api.authStatus, /Invalid authentication status response/)
})

test('logout wrapper is repeatable and does not swallow failures', async (t) => {
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })

  const { module: api } = await loadTypeScript(
    '../src/lib/api.ts',
    (source) => source.replaceAll('import.meta.env.VITE_API_BASE', "''")
  )
  const requests = []
  globalThis.fetch = async (url, init) => {
    requests.push({ url, init })
    return jsonResponse({ ok: true })
  }

  await api.logout()
  await api.logout()
  assert.deepEqual(
    requests.map((request) => [request.url, request.init.method, request.init.credentials]),
    [
      ['/api/v1/auth/logout', 'POST', 'include'],
      ['/api/v1/auth/logout', 'POST', 'include'],
    ]
  )

  globalThis.fetch = async () => jsonResponse({ error: { message: 'unavailable' } }, 503)
  await assert.rejects(api.logout, (error) => error.status === 503)

  globalThis.fetch = async () => jsonResponse({ ok: false })
  await assert.rejects(api.logout, /Invalid logout response/)
})

test('Lock waits for server confirmation and a failure preserves local state and route', async () => {
  const { module } = await loadTypeScript('../src/lib/session-transitions.ts')
  let confirmLogout
  const logoutPending = new Promise((resolve) => {
    confirmLogout = resolve
  })
  const calls = []

  const transition = module.runLockTransition({
    requestLogout: async () => {
      calls.push('logout')
      await logoutPending
    },
    clearAccountState: () => calls.push('clear'),
    markLocked: () => calls.push('locked'),
    navigateToUnlock: async () => {
      calls.push('navigate')
    },
  })

  await Promise.resolve()
  assert.deepEqual(calls, ['logout'])
  confirmLogout()
  assert.equal(await transition, 'navigated')
  assert.deepEqual(calls, ['logout', 'clear', 'locked', 'navigate'])

  const failedCalls = []
  await assert.rejects(
    module.runLockTransition({
      requestLogout: async () => {
        failedCalls.push('logout')
        throw new Error('offline')
      },
      clearAccountState: () => failedCalls.push('clear'),
      markLocked: () => failedCalls.push('locked'),
      navigateToUnlock: async () => {
        failedCalls.push('navigate')
      },
    }),
    /offline/
  )
  assert.deepEqual(failedCalls, ['logout'])
})

test('a navigation failure occurs only after the browser session is confirmed locked', async () => {
  const { module } = await loadTypeScript('../src/lib/session-transitions.ts')
  const calls = []
  const result = await module.runLockTransition({
    requestLogout: async () => calls.push('logout'),
    clearAccountState: () => calls.push('clear'),
    markLocked: () => calls.push('locked'),
    navigateToUnlock: async () => {
      calls.push('navigate')
      throw new Error('router failure')
    },
  })

  assert.equal(result, 'navigation-failed')
  assert.deepEqual(calls, ['logout', 'clear', 'locked', 'navigate'])
})

test('a Vue Router-style resolved navigation failure is not reported as navigated', async () => {
  const { module } = await loadTypeScript('../src/lib/session-transitions.ts')
  const result = await module.runLockTransition({
    requestLogout: async () => {},
    clearAccountState: () => {},
    markLocked: () => {},
    navigateToUnlock: async () => ({ type: 4 }),
  })

  assert.equal(result, 'navigation-failed')
  assert.equal(module.navigationDidFail(undefined), false)
  assert.equal(module.navigationDidFail({ type: 4 }), true)
})

test('safe next accepts only known internal application destinations', async () => {
  const { module } = await loadTypeScript('../src/lib/session-navigation.ts')

  for (const destination of [
    '/library',
    '/journey',
    '/admin',
    '/admin/upload',
    '/admin/ai',
    '/read/the-gruffalo',
    '/read/story-2',
  ]) {
    assert.equal(module.safeNextPath(destination), destination)
  }

  for (const destination of [
    '',
    ' /library',
    '/library ',
    'https://example.test/library',
    '//example.test/library',
    'javascript:alert(1)',
    'data:text/html,hello',
    '\\example.test\\library',
    '/\\example.test/library',
    '/unlock',
    '/unknown',
    '/library?q=panda',
    '/library#stories',
    '/read/',
    '/read/a/b',
    '/read/%2Fadmin',
    '/read/%5Cadmin',
    '/read/bad%2',
    '/read/Uppercase',
    '/read/the%2Dgruffalo',
    undefined,
    ['/library'],
  ]) {
    assert.equal(module.safeNextPath(destination), '/library')
  }
})

test('router decisions distinguish signed out from unavailable status', async () => {
  const { module } = await loadTypeScript('../src/lib/session-navigation.ts')

  assert.equal(module.protectedRouteDecision('unlocked', '/journey'), true)
  assert.deepEqual(module.protectedRouteDecision('locked', '/journey'), {
    path: '/unlock',
    query: { next: '/journey' },
  })
  for (const state of ['unavailable', 'unknown']) {
    assert.deepEqual(module.protectedRouteDecision(state, '/journey'), {
      path: '/session-unavailable',
      query: { next: '/journey' },
    })
  }

  assert.deepEqual(module.unlockRouteDecision('unlocked', '/admin/upload'), {
    path: '/admin/upload',
  })
  assert.equal(module.unlockRouteDecision('locked', '/admin/upload'), true)
  assert.deepEqual(module.unlockRouteDecision('unavailable', '/admin/upload'), {
    path: '/session-unavailable',
    query: { next: '/admin/upload' },
  })
})

test('unlock confirms auth state before navigating and Lock prevents duplicate submission', async () => {
  const unlockSource = await readFile(new URL('../src/views/Unlock.vue', import.meta.url), 'utf8')
  const librarySource = await readFile(new URL('../src/views/Library.vue', import.meta.url), 'utf8')

  const unlockRequest = unlockSource.indexOf('await unlock(code.value)')
  const confirmUnlocked = unlockSource.indexOf('authState.confirmUnlocked()')
  const navigate = unlockSource.indexOf('await router.replace(safeNextPath(route.query.next))')
  assert.ok(unlockRequest >= 0 && unlockRequest < confirmUnlocked)
  assert.ok(confirmUnlocked < navigate)

  assert.match(unlockSource, /getAPIErrorStatus\(error\) === 401/)
  assert.match(unlockSource, /Could not unlock Panda Pages/)
  assert.match(unlockSource, /navigationDidFail\(result\)/)

  const logoutBlock = librarySource.slice(
    librarySource.indexOf('async function logout()'),
    librarySource.indexOf('/* ---------------- "Top" button visibility')
  )
  assert.match(logoutBlock, /if \(locking\.value\) return/)
  assert.match(logoutBlock, /requestLogout: logoutSession/)
  assert.match(logoutBlock, /markLocked: authState\.confirmLocked/)
  assert.match(librarySource, /getAPIErrorStatus\(error\) === 401/)
  assert.match(librarySource, /authState\.confirmLocked\(\)/)
})

test('frontend authentication code no longer references legacy cookies', async () => {
  const sourceRoot = new URL('../src/', import.meta.url)
  const entries = await readdir(sourceRoot, { recursive: true })
  const sources = []
  for (const entry of entries) {
    if (!entry.endsWith('.ts') && !entry.endsWith('.vue')) continue
    sources.push(await readFile(new URL(entry, sourceRoot), 'utf8'))
  }

  const source = sources.join('\n')
  assert.doesNotMatch(source, /pp_unlocked|pp_aid/)
})
