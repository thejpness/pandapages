<script setup lang="ts">
import PublicInfoLayout from "../components/app/PublicInfoLayout.vue"
</script>

<template>
  <PublicInfoLayout wide title="Content & Copyright" summary="How Panda Pages approaches reading editions, source information, and content concerns.">
    <div class="content-copyright">
      <section class="content-copyright__glance" aria-labelledby="content-at-a-glance">
        <h2 id="content-at-a-glance">At a glance</h2>
        <ul class="content-copyright__glance-list">
          <li><strong>Source works</strong><span>Some Panda Pages reading material may be derived from public-domain literature.</span></li>
          <li><strong>Adapted editions</strong><span>Reading editions may be shortened, rewritten, restructured, or adjusted for reading level.</span></li>
          <li><strong>Story Studio</strong><span>Adults can create or import material they are entitled to use.</span></li>
          <li><strong>Provenance</strong><span>Source, author, rights, URL, and version information may be recorded where available.</span></li>
        </ul>
      </section>

      <div class="content-copyright__document">
        <nav class="content-copyright__toc" aria-label="On this page">
          <p>On this page</p>
          <ol>
            <li><a href="#content-chapter-sources">Sources &amp; rights</a></li>
            <li><a href="#content-adaptations">Adapted editions</a></li>
            <li><a href="#content-chapter-studio">Story Studio &amp; provenance</a></li>
            <li><a href="#content-chapter-concerns">Concerns &amp; privacy</a></li>
          </ol>
        </nav>

        <article class="content-copyright__article">
          <div id="content-chapter-sources" class="content-copyright__chapter">
            <p class="content-copyright__chapter-label">Sources and adaptations</p>
            <section aria-labelledby="content-overview"><h2 id="content-overview">Content on Panda Pages</h2><p>Panda Pages can provide reading editions and Story Studio content. Material can have different origins and rights positions, so it should not be assumed that every story has the same copyright status.</p></section>
            <section aria-labelledby="content-public-domain"><h2 id="content-public-domain">Public-domain source works</h2><p>Some reading material may derive from public-domain literature. Whether a source work is public domain can depend on the original work, a particular edition, translation, adaptation, illustration, and the jurisdiction in question. An older work is not necessarily unrestricted everywhere.</p></section>
            <section aria-labelledby="content-adaptations"><h2 id="content-adaptations">Panda Pages adaptations</h2><p>Panda Pages may prepare reading editions by shortening, rewriting, restructuring, adjusting vocabulary, changing narrative scope, or adapting material for a reading level. A public-domain source work does not necessarily mean that the displayed Panda Pages adaptation is a verbatim public-domain text. Panda Pages does not claim ownership of source works simply because it presents an adaptation.</p></section>
            <aside class="content-copyright__distinction" aria-labelledby="content-distinction-title">
              <h3 id="content-distinction-title">Public-domain source ≠ verbatim edition</h3>
              <p>Panda Pages may adapt source material for readers. Providing an adaptation does not mean Panda Pages claims ownership of the source work.</p>
            </aside>
          </div>

          <div id="content-chapter-studio" class="content-copyright__chapter">
            <p class="content-copyright__chapter-label">Story Studio and provenance</p>
            <section aria-labelledby="content-story-studio"><h2 id="content-story-studio">Story Studio supplied material</h2><p>Authenticated adults can use Story Studio to create or import material. Supported information can include text, Markdown, HTML, title, author, language, source URL, source text, rights metadata, editions, versions, and releases. Account holders should provide only material they are entitled to use. Formal upload licences, warranties, and service terms will be addressed separately.</p></section>
            <section aria-labelledby="content-provenance"><h2 id="content-provenance">Source and provenance information</h2><p>Panda Pages may record source title, author, language, rights information, source URL, and source versions where available. This helps maintain provenance, but rights metadata is information rather than a legal guarantee. Not every story necessarily displays every provenance field publicly, and Panda Pages does not claim to have independently legally verified every rights entry.</p></section>
            <aside class="content-copyright__provenance-callout" aria-labelledby="content-provenance-callout-title">
              <h3 id="content-provenance-callout-title">Recorded provenance helps describe where content came from</h3>
              <p>Source title, author, language, rights information, source URL, and source versions may be recorded where available. Rights metadata is information, not a legal guarantee.</p>
            </aside>
          </div>

          <div id="content-chapter-concerns" class="content-copyright__chapter">
            <p class="content-copyright__chapter-label">Concerns and contact</p>
            <section aria-labelledby="content-copyright-concerns"><h2 id="content-copyright-concerns">Copyright and attribution concerns</h2><p>Panda Pages is UK-operated. Concerns may include suspected copyright infringement, incorrect author attribution, incorrect source information, or other rights and provenance issues. Contact details for general content and copyright reports will be provided through Panda Pages support.</p></section>
            <section aria-labelledby="content-other-concerns"><h2 id="content-other-concerns">Other content concerns</h2><p>Future Panda Pages support contact details will also cover unsuitable content, factual or editorial issues, formatting or import problems, and accessibility concerns. We do not currently provide a separate reporting backend or a US-specific DMCA process.</p></section>
            <section aria-labelledby="content-privacy"><h2 id="content-privacy">Privacy</h2><p>For privacy, individual-rights, or account-deletion enquiries, contact <a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a>. Read the <a href="/privacy">Panda Pages Privacy Policy</a> for information about how Panda Pages handles personal information.</p></section>
            <aside class="content-copyright__privacy-callout" aria-labelledby="content-privacy-callout-title">
              <h3 id="content-privacy-callout-title">Have a privacy concern?</h3>
              <a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a>
              <p>Read the <a href="/privacy">Panda Pages Privacy Policy</a> for information about how Panda Pages handles personal information.</p>
            </aside>
          </div>
        </article>
      </div>
    </div>
  </PublicInfoLayout>
</template>

<style scoped>
section{margin-top:2.5rem;scroll-margin-top:1rem}section>:first-child{margin-top:0}h2{margin-bottom:.75rem;font-family:var(--panda-serif);font-size:clamp(1.35rem,4vw,1.8rem);font-weight:680;letter-spacing:-.025em;line-height:1.18}p{color:var(--panda-soft-ink);font-size:1rem;line-height:1.7}a{color:var(--panda-ink);font-weight:750;text-decoration-thickness:.08em;text-underline-offset:.16em}a:focus-visible{outline:3px solid var(--panda-focus);outline-offset:3px}@media(forced-colors:active){p{color:CanvasText}a{color:LinkText}}

.content-copyright{display:grid;gap:clamp(2rem,5vw,3.5rem)}
.content-copyright__glance{margin:0;padding:clamp(1.25rem,3vw,1.75rem);border:1px solid var(--panda-line-strong);border-radius:var(--panda-radius-card);background:var(--panda-paper-raised)}
.content-copyright__glance h2{margin-bottom:.75rem;font-size:clamp(1.25rem,3vw,1.55rem)}
.content-copyright__glance-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1rem;margin:0;padding:0;list-style:none}.content-copyright__glance-list li{display:grid;gap:.25rem;padding-top:.8rem;border-top:1px solid var(--panda-line-strong)}
.content-copyright__glance-list strong{color:var(--panda-ink);font-size:.92rem}.content-copyright__glance-list span{font-size:.9rem;line-height:1.55}
.content-copyright__document{display:grid;gap:1.5rem;max-width:44rem;margin-inline:auto}.content-copyright__toc{padding:1rem 1.1rem;border-top:2px solid var(--panda-ink);background:var(--panda-paper-raised)}
.content-copyright__toc p,.content-copyright__chapter-label{margin:0;color:var(--panda-soft-ink);font-size:.72rem;font-weight:850;letter-spacing:.1em;text-transform:uppercase}.content-copyright__toc ol{display:flex;flex-wrap:wrap;gap:.15rem 1rem;margin:.65rem 0 0;padding:0;list-style:none}.content-copyright__toc a{display:inline-flex;min-height:2.25rem;align-items:center;font-size:.9rem;font-weight:750}
.content-copyright__chapter{scroll-margin-top:1rem;padding-top:clamp(2rem,5vw,3.25rem);border-top:1px solid var(--panda-line-strong)}.content-copyright__chapter:first-child{padding-top:0;border-top:0}.content-copyright__chapter-label{margin-bottom:1rem}.content-copyright__chapter>section:first-of-type{margin-top:0}
.content-copyright__distinction,.content-copyright__provenance-callout,.content-copyright__privacy-callout{margin-top:1.5rem;padding:clamp(1.1rem,3vw,1.5rem);border-inline-start:.28rem solid var(--panda-ink);background:var(--panda-paper-raised)}
.content-copyright h3{margin:0 0 .5rem;font-family:var(--panda-serif);font-size:clamp(1.1rem,3vw,1.35rem);letter-spacing:-.02em;line-height:1.2}.content-copyright__distinction p,.content-copyright__provenance-callout p,.content-copyright__privacy-callout p{margin:0}.content-copyright__privacy-callout>a{display:inline-flex;min-height:2.75rem;align-items:center;font-size:1.02rem}
@media (max-width:40rem){.content-copyright__glance-list{grid-template-columns:1fr}.content-copyright__toc ol{display:grid;gap:.2rem}.content-copyright__chapter{padding-top:2.25rem}}
@media (forced-colors:active){.content-copyright__glance,.content-copyright__toc,.content-copyright__distinction,.content-copyright__provenance-callout,.content-copyright__privacy-callout{border:1px solid CanvasText;background:Canvas}.content-copyright__glance-list li,.content-copyright__chapter{border-color:CanvasText}.content-copyright__glance-list strong{color:CanvasText}}
</style>
