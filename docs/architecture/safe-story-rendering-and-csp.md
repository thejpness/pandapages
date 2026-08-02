# Safe story rendering and Content Security Policy

Panda Pages authors stories in Markdown. Markdown is canonical; rendered HTML
is a server-produced delivery representation, never an authoring format or a
general browser HTML API.

## Canonical rendering boundary

The Go `storyingest` package parses Markdown with Goldmark's safe default,
which omits raw HTML, then sanitises the generated output with one explicit
server-side allowlist before it is stored in `story_versions` or
`story_segments`. Raw HTML in Markdown is therefore not supported.

The current rendered-story allowlist is deliberately small:

- Elements: `p`, `h1`–`h6`, `em`, `strong`, `blockquote`, `ul`, `ol`, `li`,
  `hr`, `br`, `pre`, `code`, and `a`.
- Attributes: none, except Goldmark-generated heading `id` values matching the
  renderer's restricted identifier shape, and `href` on `a`.
- Links: relative URLs and `http`/`https` only. Fully qualified links receive
  `rel="nofollow noreferrer"`; Panda Pages does not add a new browsing target.

Scripts, styles, event attributes, forms, frames, objects, embedded media,
SVG, MathML, `srcdoc`, comments, arbitrary classes/IDs, `data:` URLs in story
links, and unsupported elements are removed. Images are not part of the
current story contract because no supported story content requires them.

`AdminPreview`, draft creation, and publication all use this canonical path.
Publication verifies that every persisted immutable field and segment matches a
fresh canonical result. Reader now repeats that verification for the selected
published version before returning any rendered segment. A historical or
out-of-band modified version is not rewritten or served: Reader treats it as
unavailable, while the protected Story Studio source endpoint returns its
existing finite `version_repair_required` error. One invalid story therefore
does not expose unsafe HTML or make the whole catalogue unavailable.

The only browser HTML sinks are the two Reader segment components and the Story
Studio preview. Their values carry the opaque `SafeRenderedStoryHTML` API
contract type. API parsers also validate the same restricted envelope before
constructing that type, so a malformed or hostile response is rejected before
it reaches `v-html`; this is defence in depth, not a second browser sanitiser.
Components do not accept arbitrary caller HTML. The chapter helper's
`DOMParser` use extracts text from an already canonical heading; it is not a
rendering path.

## Production browser policy

`apps/web/nginx.conf` owns production shell headers. Script, stylesheet,
font, worker, manifest, and application API resources remain same-origin.
`connect-src` adds exactly the configured HTTPS Supabase Auth origin for the
official browser session lifecycle. The policy blocks plugins, framing,
arbitrary base URLs, and form submission; contains neither `unsafe-inline` nor
`unsafe-eval` in `script-src`; and has no wildcard or other remote source.

The one limited exception is `style-src-attr 'unsafe-inline'`, separate from
`style-src 'self'`. Existing Vue presentation uses inline CSS variables and
measured widths for local Reader themes and progress surfaces. It does not
permit inline stylesheets or scripts. This exception remains explicit until
those presentation bindings can use an equally functional CSP-compatible
mechanism.

Vite development intentionally does not mirror the production policy: its HMR
style injection would require a weaker development exception. The production
Nginx policy is checked by `scripts/tests/web-security-headers-contract.sh` and
is exercised in the production image validation path.
The generated service worker precaches static build assets only; protected API
responses and rendered stories are not runtime-cached. A stale shell therefore
cannot bypass server-side version revalidation, and each build's changed asset
manifest continues through the existing prompt-based PWA update flow.


`X-Content-Type-Options`, `Referrer-Policy`, and `Permissions-Policy` are also
set by production Nginx. HSTS remains the HTTPS-terminating deployment
owner's decision. COOP and CORP are deferred because future OAuth popup and
redirect behaviour must be validated before constraining those boundaries.

The identity-foundation PR activates the official Supabase browser client only
for the isolated `/api/auth/*` flow. Production image construction replaces one
validated `__SUPABASE_AUTH_ORIGIN__` placeholder in each inherited CSP header
with the same exact project origin used by the client. Callback, refresh, and
logout browser tests intercept that origin; no live provider is required in CI.
No script, style, frame, object, base, wildcard, or broader connect exception
was added.
