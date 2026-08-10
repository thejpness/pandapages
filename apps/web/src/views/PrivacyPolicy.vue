<script setup lang="ts">
import PublicInfoLayout from '../components/app/PublicInfoLayout.vue'
</script>

<template>
  <PublicInfoLayout wide title="Privacy Policy" summary="How Panda Pages handles adult account, reader-profile, reading, and Story Studio information.">
    <div class="privacy-policy">
      <p class="privacy-policy__effective-date"><strong>Effective:</strong> 10 August 2026</p>
      <section class="privacy-policy__glance" aria-labelledby="privacy-at-a-glance">
        <h2 id="privacy-at-a-glance">At a glance</h2>
        <ul class="privacy-policy__glance-list">
          <li><strong>No behavioural advertising</strong><span>No behavioural ads or behavioural analytics.</span></li>
          <li><strong>Adult-managed</strong><span>Reader profiles exist inside adult-owned accounts.</span></li>
          <li><strong>Data minimised</strong><span>We do not require structured child DOB, email, school, photograph, or precise location.</span></li>
          <li><strong>Privacy contact</strong><a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a></li>
        </ul>
      </section>
      <div class="privacy-policy__document">
        <nav class="privacy-policy__toc" aria-label="On this page">
          <p>On this page</p>
          <ol>
            <li><a href="#privacy-chapter-overview">Overview</a></li>
            <li><a href="#privacy-chapter-readers">Accounts &amp; readers</a></li>
            <li><a href="#privacy-chapter-stories">Stories &amp; reading</a></li>
            <li><a href="#privacy-chapter-security">Security &amp; storage</a></li>
            <li><a href="#privacy-chapter-retention">Retention &amp; deletion</a></li>
            <li><a href="#privacy-chapter-rights">Your rights &amp; contact</a></li>
          </ol>
        </nav>
        <div class="privacy-policy__article">
          <div id="privacy-chapter-overview" class="privacy-policy__chapter">
            <p class="privacy-policy__chapter-label">Overview and adult accounts</p>
    <section aria-labelledby="privacy-overview"><h2 id="privacy-overview">Privacy at Panda Pages</h2><p>Panda Pages is a children’s reading PWA designed for adults and the readers in their care. We aim to use only the information needed to provide a calm, ad-free reading service, keep it secure, and improve it responsibly.</p></section>
    <section aria-labelledby="privacy-operator"><h2 id="privacy-operator">Who operates Panda Pages</h2><p>Panda Pages is operated in the United Kingdom by South Coast Apps Ltd. For privacy questions and data-protection requests, contact <a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a>.</p><p>Panda Pages is a South Coast Apps product. South Coast Apps is linked from this page and selected public and adult account surfaces.</p></section>
    <section aria-labelledby="privacy-service-for"><h2 id="privacy-service-for">Who the service is for</h2><p>A Panda Pages account is created and authenticated by an adult. Adults can create reader profiles within their account, including profiles for children. Reader profiles are not separate internet accounts.</p></section>
          </div>
          <div id="privacy-chapter-readers" class="privacy-policy__chapter">
            <p class="privacy-policy__chapter-label">Accounts and readers</p>
    <section aria-labelledby="privacy-information"><h2 id="privacy-information">Information we process</h2><p>Depending on how Panda Pages is used, we may process:</p><ul><li>adult account, principal, account-membership, and role information;</li><li>reader-profile name, reading level, optional PIN-protection state, and reading preferences;</li><li>reading progress, reading-location state, and per-story edition choices or overrides;</li><li>Story Studio text, Markdown, HTML, source text, source URLs, title, author, language, rights, edition, version, and release information supplied by authorised adults;</li><li>technical and security information needed to operate, protect, and diagnose the service.</li></ul></section>
    <section aria-labelledby="privacy-authentication"><h2 id="privacy-authentication">Adult accounts and social login</h2><p>Adult sign-in uses Supabase Auth, currently with Google or Facebook as sign-in options. Panda Pages receives a Supabase-authenticated bearer identity and resolves it to a Panda Pages principal, account membership, and adult role.</p><p>Panda Pages does not copy Google or Facebook profile information into its application database or use it to personalise the reading service. The Panda Pages database records a provider-neutral Supabase external identity, including its issuer and subject, together with Panda Pages account and membership information. Your browser’s Supabase session may still contain information provided through the chosen sign-in provider.</p></section>
    <section aria-labelledby="privacy-reader-profiles"><h2 id="privacy-reader-profiles">Reader profiles and children’s information</h2><p>A reader profile may represent an identifiable child, so this information can be personal information. Panda Pages may process the reader-profile name, reading level, optional PIN security state (including information needed to verify attempts and apply temporary protection), reading progress, and story or edition choices.</p><p>Panda Pages deliberately does not require structured reader-profile fields for date of birth, age, email address, phone number, postal address, school, gender, photograph or avatar, precise location, advertising identifiers, analytics identifiers, or social-media identity.</p></section>
        <aside class="privacy-policy__minimisation" aria-labelledby="privacy-minimisation-title">
          <h3 id="privacy-minimisation-title">Designed to ask for less</h3>
          <p>Reader profiles need a name, reading level, and only the settings needed to support reading. Panda Pages does not require structured identity, contact, school, photograph, or precise-location fields for readers.</p>
        </aside>
          </div>
          <div id="privacy-chapter-stories" class="privacy-policy__chapter">
            <p class="privacy-policy__chapter-label">Stories and reading</p>
    <section aria-labelledby="privacy-reading"><h2 id="privacy-reading">Reading activity and preferences</h2><p>We use reading progress and reader preferences to return a reader to an appropriate place, apply the reading level selected for that profile, and honour any per-story edition choice that remains eligible for the current release. We do not use reading activity for behavioural advertising or behavioural analytics.</p></section>
    <section aria-labelledby="privacy-story-studio"><h2 id="privacy-story-studio">Story Studio and supplied or imported content</h2><p>Story Studio is an adult capability. It can store content and metadata that an authorised adult writes, imports, or provides. HTML and document conversion for Story Studio takes place inside Panda Pages; Panda Pages does not send imported material to a third-party AI or document-conversion service for that conversion.</p></section>
          </div>
          <div id="privacy-chapter-security" class="privacy-policy__chapter">
            <p class="privacy-policy__chapter-label">How information is handled</p>
    <section aria-labelledby="privacy-technical"><h2 id="privacy-technical">Technical information</h2><p>Like other online services, Panda Pages and its hosting infrastructure may process limited technical information needed to deliver and secure requests, investigate faults or abuse, and maintain the service. Panda Pages API request logging deliberately records limited operational information such as request ID, HTTP method, request path, response status, request duration, and response size; it does not deliberately add request bodies, bearer tokens, account IDs, reader-profile IDs, or PIN values. Hosting or web-server layers may process standard connection information such as IP address, browser or user-agent information, timestamps, and referrer information where applicable. Operational and security logs may be retained for up to 90 days.</p><p>Panda Pages does not currently use advertising technology, Google Analytics, Meta Pixel, behavioural tracking SDKs, or an external browser-telemetry or error-reporting integration.</p></section>
    <section aria-labelledby="privacy-use"><h2 id="privacy-use">How we use information</h2><ul><li>to authenticate adult account holders and provide account access;</li><li>to provide reader profiles, reading progress, reading-level access, and edition choices;</li><li>to provide Story Studio functionality requested by authorised adults;</li><li>to secure the service, prevent abuse, investigate faults, and maintain reliability;</li><li>to meet legal and regulatory obligations where applicable.</li></ul></section>
    <section aria-labelledby="privacy-bases"><h2 id="privacy-bases">Legal bases</h2><p>We generally rely on contract where necessary to provide an adult account or Story Studio functionality requested by an adult. We generally rely on legitimate interests for reader-profile information and reading progress, while taking particular account of children’s rights, interests, and appropriate safeguards. We generally rely on legitimate interests for security, abuse prevention, and diagnostics, and on legal obligation where a statutory response is required.</p></section>
    <section aria-labelledby="privacy-providers"><h2 id="privacy-providers">Service providers and third parties</h2><p>Supabase provides adult authentication. Google or Facebook processes sign-in information when an adult chooses that provider. Panda Pages does not sell personal information or share it for behavioural advertising.</p><p>Panda Pages application infrastructure is hosted with Hetzner in Finland. Supabase authentication services used by Panda Pages are hosted in an AWS EU-West region.</p></section>
    <section aria-labelledby="privacy-transfers"><h2 id="privacy-transfers">International transfers</h2><p>Some service providers may process information outside the United Kingdom. Where that happens, we will use transfer safeguards required by applicable data-protection law. Panda Pages application infrastructure and backup storage are in Finland, and Supabase authentication services are hosted in an AWS EU-West region.</p></section>
          </div>
          <div id="privacy-chapter-retention" class="privacy-policy__chapter">
            <p class="privacy-policy__chapter-label">Keeping information safe</p>
    <section aria-labelledby="privacy-retention"><h2 id="privacy-retention">Retention</h2><p>We keep information for as long as needed to provide the service, maintain security, meet legal obligations, or resolve disputes. Our retention policy is to remove inactive accounts after 24 months of inactivity, measured from the last authenticated adult account activity. We may delete an account and its associated Panda Pages data when that period is reached; this policy is not based on a child’s reading session, reader progress alone, background PWA activity, service-worker activity, or anonymous page visits.</p><p>Panda Pages maintains daily backups for resilience and disaster recovery. Backups are stored in Hetzner Object Storage in Finland and are retained for up to 30 days. Information deleted from the live service may remain in restricted backup copies until those backups expire through our normal backup cycle, which is currently up to 30 days. Backup copies are maintained for resilience and disaster recovery rather than ordinary application use.</p></section>
        <aside class="privacy-policy__retention-facts" aria-labelledby="privacy-retention-facts-title">
          <h3 id="privacy-retention-facts-title">Retention at a glance</h3>
          <dl>
            <div><dt>Backups</dt><dd>Daily in Hetzner Object Storage, Finland, retained for up to 30 days.</dd></div>
            <div><dt>Operational and security logs</dt><dd>May be retained for up to 90 days.</dd></div>
            <div><dt>Inactive accounts</dt><dd>Our retention policy is 24 months from the last authenticated adult account activity.</dd></div>
          </dl>
        </aside>
    <section aria-labelledby="privacy-security"><h2 id="privacy-security">Security</h2><p>Panda Pages uses account, membership, profile, and role boundaries to limit access to information. Reader mode is distinct from parent controls, and child-mode or PIN-verified state is intentionally memory-only rather than a durable browser setting. No online service can promise absolute security, but we use safeguards appropriate to the service and its risks.</p></section>
    <section aria-labelledby="privacy-children"><h2 id="privacy-children">Children’s privacy</h2><p>Panda Pages is intended to be managed by an adult account holder. We recognise that reader-profile information may relate to a child and aim to minimise the information required for that profile. We do not provide separate child internet accounts, advertising, behavioural profiling, or social-media features for readers.</p></section>
    <section aria-labelledby="privacy-storage"><h2 id="privacy-storage">Cookies, local storage, and PWA storage</h2><p>Panda Pages uses Supabase browser-session persistence for adult authentication. It also stores the selected Panda Pages account UUID and selected reader-profile UUID in browser local storage, and may store reading-display and Library sorting preferences on the device. Its PWA service worker caches application resources. Panda Pages does not use its own cookie-based application login, and child-mode and PIN-verified state are memory-only.</p><p>Because Panda Pages does not currently use advertising or behavioural analytics, this service does not currently present an analytics or advertising cookie-consent control.</p></section>
          </div>
          <div id="privacy-chapter-rights" class="privacy-policy__chapter">
            <p class="privacy-policy__chapter-label">Rights and control</p>
    <section aria-labelledby="privacy-rights"><h2 id="privacy-rights">Data-protection rights</h2><p>Depending on the circumstances, adults may have rights to request access, correction, deletion, restriction, objection, or portability of personal information, and to raise concerns about our processing. An adult account holder can exercise rights concerning reader profiles in their account where appropriate. Contact <a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a> for privacy and individual-rights requests.</p></section>
    <section aria-labelledby="privacy-deletion"><h2 id="privacy-deletion">Deletion</h2><p>Adults can currently delete individual reader profiles through Panda Pages. Deleting a reader profile also removes its associated reading progress and per-story edition overrides. If you want to request deletion of the full Panda Pages account, contact <a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a>. A dedicated account-deletion process is being finalised; Panda Pages does not currently offer a self-service full-account deletion workflow.</p><p>When a Panda Pages account is deleted, we intend to remove the account’s reader information and account-owned Story Studio content, including customer-created content where applicable, subject to limited information we may need to retain for legal or security reasons and normal backup expiry. Signing out or disconnecting a social-login provider does not by itself delete a Panda Pages account.</p></section>
    <section aria-labelledby="privacy-changes"><h2 id="privacy-changes">Changes to this policy</h2><p>We may update this policy as Panda Pages changes. Material changes will be reflected on this page with an updated effective date.</p></section>
    <section aria-labelledby="privacy-contact"><h2 id="privacy-contact">Contact</h2><p>For privacy questions, data-protection requests, or full-account deletion enquiries, contact <a href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a>. This is a privacy contact, not a general product-support mailbox.</p></section>
        <aside class="privacy-policy__contact-callout" aria-labelledby="privacy-contact-callout-title">
          <h3 id="privacy-contact-callout-title">Questions about your privacy?</h3>
          <a class="privacy-policy__contact-email" href="mailto:privacy@southcoastapps.co.uk">privacy@southcoastapps.co.uk</a>
          <p>South Coast Apps Ltd</p>
          <a class="privacy-policy__contact-action" href="mailto:privacy@southcoastapps.co.uk">Email the privacy team</a>
        </aside>
    <section aria-labelledby="privacy-ico"><h2 id="privacy-ico">ICO complaints</h2><p>If you are in the United Kingdom and are unhappy with how we handle personal information, you can raise a concern with the Information Commissioner’s Office (ICO). Details are available on the <a href="https://ico.org.uk/make-a-complaint/" target="_blank" rel="noreferrer">ICO website</a>.</p></section>
          </div>
        </div>
      </div>
    </div>
  </PublicInfoLayout>
</template>

<style scoped>
.privacy-policy__effective-date,.privacy-policy section>:first-child{margin-top:0}.privacy-policy__effective-date{margin-bottom:2.5rem;border-inline-start:.25rem solid var(--panda-line-strong);padding-inline-start:.9rem;color:var(--panda-muted);font-size:.93rem;line-height:1.55}.privacy-policy section{margin-top:2.5rem;scroll-margin-top:1rem}.privacy-policy h2{margin-bottom:.75rem;font-family:var(--panda-serif);font-size:clamp(1.35rem,4vw,1.8rem);font-weight:680;letter-spacing:-.025em;line-height:1.18}.privacy-policy p,.privacy-policy li{color:var(--panda-soft-ink);font-size:1rem;line-height:1.7}.privacy-policy p+p{margin-top:1rem}.privacy-policy ul{display:grid;gap:.55rem;margin:1rem 0 0;padding-inline-start:1.25rem}.privacy-policy a{color:var(--panda-ink);font-weight:750;text-decoration-thickness:.08em;text-underline-offset:.16em}.privacy-policy a:focus-visible{outline:3px solid var(--panda-focus);outline-offset:3px}@media (forced-colors:active){.privacy-policy p,.privacy-policy li{color:CanvasText}.privacy-policy a{color:LinkText}}

.privacy-policy{display:grid;gap:clamp(2rem,5vw,3.5rem)}
.privacy-policy__effective-date{margin:0}
.privacy-policy__glance{margin:0;padding:clamp(1.25rem,3vw,1.75rem);border:1px solid var(--panda-line-strong);border-radius:var(--panda-radius-card);background:var(--panda-paper-raised)}
.privacy-policy__glance h2{font-size:clamp(1.25rem,3vw,1.55rem)}
.privacy-policy__glance-list{grid-template-columns:repeat(2,minmax(0,1fr));gap:1rem;margin:1.1rem 0 0;padding:0;list-style:none}
.privacy-policy__glance-list li{display:grid;gap:.25rem;padding-top:.8rem;border-top:1px solid var(--panda-line-strong)}
.privacy-policy__glance-list strong{color:var(--panda-ink);font-size:.92rem}.privacy-policy__glance-list span,.privacy-policy__glance-list a{font-size:.9rem;line-height:1.55}
.privacy-policy__document{display:grid;gap:clamp(2rem,5vw,4rem)}
.privacy-policy__toc{align-self:start;padding:1rem 1.1rem;border-top:2px solid var(--panda-ink);background:var(--panda-paper-raised)}
.privacy-policy__toc p,.privacy-policy__chapter-label{margin:0;color:var(--panda-soft-ink);font-size:.72rem;font-weight:850;letter-spacing:.1em;text-transform:uppercase}
.privacy-policy__toc ol{display:grid;gap:.2rem;margin:.65rem 0 0;padding:0;list-style:none}.privacy-policy__toc a{display:inline-flex;min-height:2.25rem;align-items:center;font-size:.9rem;font-weight:750}
.privacy-policy__article{min-width:0}.privacy-policy__chapter{scroll-margin-top:1rem;padding-top:clamp(2rem,5vw,3.5rem);border-top:1px solid var(--panda-line-strong)}
.privacy-policy__chapter:first-child{padding-top:0;border-top:0}.privacy-policy__chapter-label{margin-bottom:1rem}.privacy-policy__chapter>section:first-of-type{margin-top:0}
.privacy-policy__minimisation,.privacy-policy__retention-facts,.privacy-policy__contact-callout{margin-top:1.5rem;padding:clamp(1.1rem,3vw,1.5rem);border-inline-start:.28rem solid var(--panda-ink);background:var(--panda-paper-raised)}
.privacy-policy h3{margin:0 0 .5rem;font-family:var(--panda-serif);font-size:clamp(1.1rem,3vw,1.35rem);letter-spacing:-.02em;line-height:1.2}.privacy-policy__minimisation p,.privacy-policy__contact-callout p{margin:0}
.privacy-policy__retention-facts dl{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1rem;margin:1rem 0 0}.privacy-policy__retention-facts dt{color:var(--panda-ink);font-size:.88rem;font-weight:850}.privacy-policy__retention-facts dd{margin:.35rem 0 0;color:var(--panda-soft-ink);font-size:.92rem;line-height:1.55}
.privacy-policy__contact-email{display:inline-flex;min-height:2.75rem;align-items:center;font-size:1.05rem}.privacy-policy__contact-action{display:inline-flex;min-height:2.75rem;align-items:center;margin-top:.75rem;padding-inline:.9rem;border:1px solid var(--panda-ink);border-radius:var(--panda-radius-compact);text-decoration:none}
@media (min-width:70rem){.privacy-policy__document{grid-template-columns:11rem minmax(0,44rem);justify-content:center}.privacy-policy__toc{position:sticky;top:1.5rem}}
@media (max-width:40rem){.privacy-policy__glance-list,.privacy-policy__retention-facts dl{grid-template-columns:1fr}.privacy-policy__glance-list{gap:.75rem}.privacy-policy__document{gap:1.5rem}.privacy-policy__toc{padding:1rem}.privacy-policy__chapter{padding-top:2.25rem}}
@media (forced-colors:active){.privacy-policy__glance,.privacy-policy__toc,.privacy-policy__minimisation,.privacy-policy__retention-facts,.privacy-policy__contact-callout{border:1px solid CanvasText;background:Canvas}.privacy-policy__glance-list li,.privacy-policy__chapter{border-color:CanvasText}.privacy-policy__glance-list strong,.privacy-policy__retention-facts dt{color:CanvasText}}
</style>
