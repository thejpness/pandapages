import { readFile } from 'node:fs/promises'
import { transformWithOxc } from 'vite'

async function compiledModuleURL(sourceURL) {
  const source = await readFile(sourceURL, 'utf8')
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
  if (!source.includes('./reader-locator-v2')) return source
  const dependencyURL = new URL('./reader-locator-v2.ts', sourceURL)
  const moduleURL = await compiledModuleURL(dependencyURL)
  return source.replaceAll('./reader-locator-v2', moduleURL)
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
