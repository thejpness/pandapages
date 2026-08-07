import { readFile } from 'node:fs/promises'
import { transformWithOxc } from 'vite'

async function compiledModuleURL(sourceURL) {
  const originalSource = await readFile(sourceURL, 'utf8')
  const source = await resolveKnownImports(originalSource, sourceURL)
  const transformed = await transformWithOxc(source, sourceURL.pathname)
  return (
    'data:text/javascript;base64,' +
    Buffer.from(transformed.code).toString('base64') +
    '#' +
    Date.now() +
    Math.random()
  )
}

async function resolveKnownImports(source, sourceURL) {
  let resolved = source
  // API contract tests execute modules from data: URLs.  The browser account
  // context is deliberately replaced with a deterministic test boundary so
  // those tests exercise request construction without emulating Supabase.
  if (resolved.includes('./account-context')) {
    const accountContext =
      'data:text/javascript;base64,' +
      Buffer.from(`
        export async function currentAccountContext() {
          return globalThis.__pandapagesTestAccountContext ?? {
            accessToken: 'test-access-token',
            membership: { accountId: '11111111-1111-4111-8111-111111111111', role: 'adult' },
          };
        }
        export function clearSelectedAccount() {}
      `).toString('base64')
    resolved = resolved.replaceAll('./account-context', accountContext)
  }
  if (resolved.includes('./supabase-auth')) {
    const supabaseAuth =
      'data:text/javascript;base64,' +
      Buffer.from('export async function signOutSupabaseSession() {}').toString('base64')
    resolved = resolved.replaceAll('./supabase-auth', supabaseAuth)
  }
  for (const dependency of [
    './reader-locator-v2',
    './reader-themes',
    './reader-preferences-v2',
  ]) {
    if (!resolved.includes(dependency)) continue
    const dependencyURL = new URL(`${dependency}.ts`, sourceURL)
    const moduleURL = await compiledModuleURL(dependencyURL)
    resolved = resolved.replaceAll(dependency, moduleURL)
  }
  return resolved
}

export async function loadTypeScript(
  relativePath,
  parentURL,
  transform = (source) => source,
) {
  const sourceURL = new URL(relativePath, parentURL)
  const originalSource = await readFile(sourceURL, 'utf8')
  const source = await resolveKnownImports(transform(originalSource), sourceURL)
  const transformed = await transformWithOxc(source, sourceURL.pathname)
  const moduleURL =
    'data:text/javascript;base64,' +
    Buffer.from(transformed.code).toString('base64') +
    '#' +
    Date.now() +
    Math.random()

  return { module: await import(moduleURL), source: originalSource }
}
