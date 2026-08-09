import assert from 'node:assert/strict'
import test from 'node:test'

import { loadTypeScript } from './helpers/typescript-module.mjs'

const testStateKey = '__pandapagesProfileSessionTestState'
const selectionMock =
  'data:text/javascript;base64,' +
  Buffer.from(`
    const state = globalThis.${testStateKey} ?? (globalThis.${testStateKey} = { selected: null, active: null })
    export const selectReaderProfile = (id) => { if (!id || id.trim() !== id) return false; state.selected = id; return true }
    export const selectedReaderProfileID = () => state.selected
    export const clearSelectedReaderProfile = () => { state.selected = null }
  `).toString('base64')
const modeMock =
  'data:text/javascript;base64,' +
  Buffer.from(`
    const state = globalThis.${testStateKey} ?? (globalThis.${testStateKey} = { selected: null, active: null })
    export const enterChildMode = (id) => { if (!id || id.trim() !== id) return false; state.active = id; return true }
    export const isChildModeFor = (id) => state.active === id
    export const leaveChildMode = () => { state.active = null }
  `).toString('base64')

const { module: session } = await loadTypeScript(
  '../src/lib/profile-session.ts',
  import.meta.url,
  (source) => source
    .replaceAll('"./reader-profile-selection"', JSON.stringify(selectionMock))
    .replaceAll('"./reader-mode"', JSON.stringify(modeMock)),
)

function state() {
  return globalThis[testStateKey]
}

test('successful entry establishes matching persisted and child state', () => {
  state().selected = null
  state().active = null
  assert.equal(session.enterReaderProfileSession('reader-a'), true)
  assert.deepEqual(state(), { selected: 'reader-a', active: 'reader-a' })
})

test('profile invalidation only clears a matching session', () => {
  state().selected = 'reader-a'
  state().active = 'reader-a'
  session.invalidateReaderProfileSession('reader-b')
  assert.deepEqual(state(), { selected: 'reader-a', active: 'reader-a' })
  session.invalidateReaderProfileSession('reader-a')
  assert.deepEqual(state(), { selected: null, active: null })
})

test('only established profile error codes invalidate a session', () => {
  for (const code of ['profile_required', 'invalid_profile', 'profile_forbidden']) {
    const error = Object.assign(new Error(code), { code })
    assert.equal(session.isProfileSessionInvalidError(error), true)
  }
  for (const code of ['forbidden', 'story_not_found', 'invalid_content']) {
    const error = Object.assign(new Error(code), { code })
    assert.equal(session.isProfileSessionInvalidError(error), false)
  }
})
