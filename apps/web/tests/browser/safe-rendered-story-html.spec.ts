import { expect, test } from '@playwright/test'

const hostileFragments = [
  { name: 'script', html: '<script src="https://story-xss.invalid/script.js"></script>' },
  { name: 'style', html: '<style>body { display: none }</style>' },
  { name: 'stylesheet link', html: '<link rel="stylesheet" href="https://story-xss.invalid/story.css">' },
  { name: 'meta', html: '<meta http-equiv="refresh" content="0;url=https://story-xss.invalid/">' },
  { name: 'base', html: '<base href="https://story-xss.invalid/">' },
  { name: 'title', html: '<title>Hostile story title</title>' },
  { name: 'leading comment', html: '<!-- hidden payload --><p>Story</p>' },
  { name: 'malformed document head', html: '</head><script>window.__storyXSS = true</script><body><p>Story</p>' },
  { name: 'SVG namespace', html: '<svg><a onload="window.__storyXSS = true"></a></svg>' },
  { name: 'MathML namespace', html: '<math><annotation-xml encoding="text/html"><script>window.__storyXSS = true</script></annotation-xml></math>' },
  { name: 'iframe', html: '<iframe srcdoc="<script>window.__storyXSS = true</script>"></iframe>' },
  { name: 'event handler', html: '<p onclick="window.__storyXSS = true">Story</p>' },
  { name: 'style attribute', html: '<p style="background:url(https://story-xss.invalid/pixel)">Story</p>' },
  { name: 'executable link', html: '<a href="javascript:window.__storyXSS = true">Story</a>' },
  { name: 'arbitrary attribute', html: '<hr id="Not-a-heading">' },
] as const

const validFragment = [
  '<h1 id="Story-title">Panda Pages 世界</h1>',
  '<h2>Chapter</h2><h3>Section</h3><h4>Part</h4><h5>Note</h5><h6>Aside</h6>',
  '<p>Text with <em>emphasis</em>, <strong>strength</strong>, and <a href="https://example.invalid/story" rel="nofollow noreferrer">a link</a>.</p>',
  '<blockquote><p>A quotation.</p></blockquote>',
  '<ul><li>One</li></ul><ol><li>Two</li></ol>',
  '<hr><br><pre><code>&lt;safe&gt;</code></pre>',
].join('')

test('the browser validates the complete inert story fragment', async ({ page }) => {
  const unexpectedRequests: string[] = []
  const dialogs: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).hostname === 'story-xss.invalid') {
      unexpectedRequests.push(request.url())
    }
  })
  page.on('dialog', async (dialog) => {
    dialogs.push(dialog.message())
    await dialog.dismiss()
  })

  await page.goto('/unlock')
  const results = await page.evaluate(
    async ({ hostile, valid }) => {
      const modulePath = '/src/lib/reader-locator-v2.ts'
      const { parseSafeRenderedStoryHTML } = (await import(
        /* @vite-ignore */ modulePath
      )) as {
        parseSafeRenderedStoryHTML: (value: unknown) => string
      }
      const rejected = hostile.map(({ name, html }) => {
        try {
          parseSafeRenderedStoryHTML(html)
          return { name, rejected: false }
        } catch {
          return { name, rejected: true }
        }
      })
      let accepted: string | null
      try {
        accepted = parseSafeRenderedStoryHTML(valid)
      } catch {
        accepted = null
      }
      return { rejected, accepted }
    },
    { hostile: hostileFragments, valid: validFragment },
  )

  expect(results.rejected).toEqual(
    hostileFragments.map(({ name }) => ({ name, rejected: true })),
  )
  expect(results.accepted).toBe(validFragment)
  expect(dialogs).toEqual([])
  expect(unexpectedRequests).toEqual([])
  expect(
    await page.evaluate(
      () => (window as Window & { __storyXSS?: boolean }).__storyXSS,
    ),
  ).toBeUndefined()
})
