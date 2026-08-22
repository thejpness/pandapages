import { expect, test } from './support/auth'

test('generated Markdown drops raw HTML and remains limited to the Panda Pages safe story fragment', async ({ page }) => {
  const unexpectedRequests: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).hostname === 'generated-xss.invalid') unexpectedRequests.push(request.url())
  })

  await page.goto('/account/login')
  const rendered = await page.evaluate(async () => {
    const modulePath = '/src/lib/generated-markdown.ts'
    const { renderGeneratedMarkdown } = await import(
      /* @vite-ignore */ modulePath
    ) as {
      renderGeneratedMarkdown: (markdown: string) => string
    }
    return renderGeneratedMarkdown([
      '# A safe heading',
      '',
      '**Strong** and *emphasis* remain readable.',
      '',
      '- One',
      '- Two',
      '',
      '[Safe link](https://example.invalid/story)',
      '[Bad link](javascript:window.__generatedXSS = true)',
      '',
      '<script src="https://generated-xss.invalid/script.js">window.__generatedXSS = true</script>',
      '<p onclick="window.__generatedXSS = true">Raw HTML attempt</p>',
      '<img src=x onerror="window.__generatedXSS = true">',
      '[Malformed](<javascript:window.__generatedXSS = true>)',
    ].join('\n') )
  })

  expect(rendered).toContain('<h1>A safe heading</h1>')
  expect(rendered).toContain('<strong>Strong</strong>')
  expect(rendered).toContain('<em>emphasis</em>')
  expect(rendered).toContain('<ul>')
  expect(rendered).toContain('https://example.invalid/story')
  expect(rendered).not.toMatch(/<script|onclick=|onerror=|href="javascript:|<img/i)
  expect(unexpectedRequests).toEqual([])
  expect(await page.evaluate(() => (window as Window & { __generatedXSS?: boolean }).__generatedXSS)).toBeUndefined()
})
