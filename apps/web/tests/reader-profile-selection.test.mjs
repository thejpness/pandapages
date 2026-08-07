import assert from 'node:assert/strict'
import test from 'node:test'

import { loadTypeScript } from './helpers/typescript-module.mjs'

const { module: selection } = await loadTypeScript(
  '../src/lib/reader-profile-selection.ts',
  import.meta.url,
)

const one = [{ id: '123e4567-e89b-42d3-a456-426614174300', name: 'Mina' }]
const two = [
  ...one,
  { id: '123e4567-e89b-42d3-a456-426614174301', name: 'Ted' },
]

function storage() {
  const values = new Map()
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  }
}

test('zero profiles stays unselected and creates no default', () => {
  assert.equal(selection.reconcileReaderProfileSelection(null, []), null)
})

test('exactly one server profile is selected as a frontend convenience', () => {
  assert.deepEqual(selection.reconcileReaderProfileSelection(null, one), { id: one[0].id })
})

test('multiple profiles require an explicit selection', () => {
  assert.equal(selection.reconcileReaderProfileSelection(null, two), null)
})

test('a persisted selection restores only when the account profile list confirms it', () => {
  assert.deepEqual(
    selection.reconcileReaderProfileSelection(two[1].id, two),
    { id: two[1].id },
  )
  assert.equal(
    selection.reconcileReaderProfileSelection('missing-profile', two),
    null,
  )
})

test('profile persistence stores only the selected ID and can be cleared after deletion or account change', () => {
  const localStorage = storage()
  selection.selectReaderProfile(one[0].id, localStorage)
  assert.equal(selection.selectedReaderProfileID(localStorage), one[0].id)
  selection.clearSelectedReaderProfile(localStorage)
  assert.equal(selection.selectedReaderProfileID(localStorage), null)
})
