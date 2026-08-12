# Benjamin Bunny benchmark source

This fixture is the single reviewed public-domain source used by story adaptation benchmark v1 end-to-end testing.

- Work: *The Tale of Benjamin Bunny* by Beatrix Potter
- Provider: Project Gutenberg
- External ID: `14407`
- Provider landing page: `https://www.gutenberg.org/ebooks/14407`
- Reviewed copyright-policy snapshot date: `2026-08-12`
- Panda Pages policy: `panda-pages-copyright-v3`

`source.md` is a benchmark projection of the story body. The Project Gutenberg license/boilerplate, publisher and copyright front matter, image placeholders, and display-only headings are not part of the canonical adaptation input. The manifest binds the exact committed projection by SHA-256 and supplies evidence that is re-evaluated by the same deterministic Panda Pages copyright policy used by the application.

This fixture does not create a general source-file bypass. Benchmark live generation may use only this committed, reviewed fixture until another source receives equivalent provenance and policy review.
