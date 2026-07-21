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
