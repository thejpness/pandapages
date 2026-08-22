import DOMPurify from 'dompurify'
import { marked, Renderer } from 'marked'
import {
  parseSafeRenderedStoryHTML,
  type SafeRenderedStoryHTML,
} from './reader-locator-v2'

const allowedTags = [
  'p',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'em',
  'strong',
  'blockquote',
  'ul',
  'ol',
  'li',
  'hr',
  'br',
  'pre',
  'code',
  'a',
]

const renderer = new Renderer()

// Generated orchestration Markdown is model output. Raw HTML is deliberately
// discarded before sanitisation; it must never become an HTML rendering path.
renderer.html = () => ''

/**
 * Renders generated Markdown through three independent boundaries: raw HTML
 * is disabled by the parser, DOMPurify limits the resulting markup, then the
 * existing Panda Pages safe-story validator checks the final inert fragment.
 */
export function renderGeneratedMarkdown(markdown: string): SafeRenderedStoryHTML {
  if (typeof markdown !== 'string') {
    throw new Error('Generated Markdown is invalid')
  }

  const parsed = marked.parse(markdown, {
    async: false,
    gfm: true,
    breaks: false,
    renderer,
  })
  const sanitised = DOMPurify.sanitize(parsed, {
    ALLOWED_TAGS: allowedTags,
    ALLOWED_ATTR: ['href', 'rel', 'id'],
    ALLOW_UNKNOWN_PROTOCOLS: false,
  })

  return parseSafeRenderedStoryHTML(sanitised)
}
