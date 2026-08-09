import assert from 'node:assert/strict'
import test from 'node:test'

import { loadTypeScript } from './helpers/typescript-module.mjs'

const { module: destination } = await loadTypeScript(
  '../src/lib/profile-destination.ts',
  import.meta.url,
)

test('reader destination accepts Library and Reader local continuations', () => {
  assert.equal(destination.resolveReaderDestination('/library?q=moon#shelf'), '/library?q=moon#shelf')
  assert.equal(destination.resolveReaderDestination('/read/the-three-little-pigs?mode=read#chapter-1'), '/read/the-three-little-pigs?mode=read#chapter-1')
})

test('reader destination rejects non-reader and external continuations', () => {
  for (const value of [
    'https://example.com',
    '//example.com',
    '/\\example.com',
    'javascript:alert(1)',
    '/account',
    '/admin/stories',
    '/profiles/manage',
    '/read/not/a/slug',
    '/read/%2F',
  ]) {
    assert.equal(destination.resolveReaderDestination(value), '/library')
  }
})

test('profile creation has a finite safe return origin', () => {
  assert.equal(destination.profileCreateReturnDestination('chooser'), '/profiles')
  assert.equal(destination.profileCreateReturnDestination('manage'), '/profiles/manage')
  assert.equal(destination.profileCreateReturnDestination('anything-else'), '/profiles')
})
