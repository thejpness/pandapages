<template>
  <div class="panda-page">
    <a class="skip-link" href="#main-content">Skip to main content</a>

    <header class="site-header">
      <a class="brand" href="/" aria-label="Panda Pages home">
        <img
          class="brand-logo brand-logo--header"
          src="/logo.png"
          alt=""
          aria-hidden="true"
        />
        <span class="brand-name">Panda Pages</span>
      </a>

      <nav class="header-actions" aria-label="Primary navigation">
        <a class="text-link text-link--nav" :href="storiesHref">Browse stories</a>
        <button class="button button--small button--ink" type="button" @click="handleInstall">
          <svg class="button-icon" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 3v11m0 0 4-4m-4 4-4-4M5 17v2a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2" />
          </svg>
          {{ installButtonLabel }}
        </button>
      </nav>
    </header>

    <main id="main-content" tabindex="-1">
      <section class="hero section-shell" aria-labelledby="hero-heading">
        <div class="hero-copy">
          <p class="eyebrow"><span aria-hidden="true">●</span> An ad-free children’s library</p>
          <h1 id="hero-heading">Stories that<br />never grow old.</h1>
          <p class="hero-intro">
            A quiet place to discover and read the classic stories loved by children for generations.
          </p>

          <div class="hero-actions">
            <a class="button button--large button--ink" :href="storiesHref">
              Start reading
              <svg class="button-icon button-icon--arrow" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M5 12h14m-5-5 5 5-5 5" />
              </svg>
            </a>
            <button class="button button--large button--paper" type="button" @click="handleInstall">
              <svg class="button-icon" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M12 3v11m0 0 4-4m-4 4-4-4M5 17v2a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2" />
              </svg>
              {{ installButtonLabel }}
            </button>
          </div>

          <ul class="trust-list" aria-label="Panda Pages benefits">
            <li>No adverts</li>
            <li>No subscription</li>
            <li>Made for reading together</li>
          </ul>
        </div>

        <div
          class="hero-visual"
          role="img"
          aria-label="An open storybook featuring a friendly panda"
        >
          <div class="orbit orbit--one" aria-hidden="true"></div>
          <div class="orbit orbit--two" aria-hidden="true"></div>

          <div class="floating-note floating-note--top" aria-hidden="true">
            <span>Bedtime favourite</span>
            <strong>One more story?</strong>
          </div>

          <div class="storybook" aria-hidden="true">
            <div class="book-shadow"></div>
            <div class="book-cover book-cover--left"></div>
            <div class="book-cover book-cover--right"></div>

            <div class="book-page book-page--left">
              <span class="page-kicker">Panda Pages</span>
              <p class="once-upon">Once upon<br />a time…</p>
              <span class="page-line page-line--short"></span>
              <span class="page-line"></span>
              <span class="page-line page-line--medium"></span>
              <span class="page-number">12</span>
            </div>

            <div class="book-page book-page--right">
              <div class="panda-portrait">
                <span class="panda-ear panda-ear--left"></span>
                <span class="panda-ear panda-ear--right"></span>
                <span class="panda-face">
                  <span class="panda-patch panda-patch--left"><i></i></span>
                  <span class="panda-patch panda-patch--right"><i></i></span>
                  <span class="panda-muzzle"><i></i></span>
                </span>
                <span class="panda-body"></span>
              </div>
              <span class="spark spark--one">✦</span>
              <span class="spark spark--two">✦</span>
              <span class="spark spark--three">·</span>
              <span class="page-number">13</span>
            </div>
            <div class="book-spine"></div>
          </div>

          <div class="floating-note floating-note--bottom" aria-hidden="true">
            <span class="mini-panda">●</span>
            <span><strong>Calm by design</strong><small>No noise. Just stories.</small></span>
          </div>
        </div>
      </section>

      <section class="story-strip" aria-label="Featured classic stories">
        <div class="story-strip__inner section-shell">
          <p class="story-strip__label">Tonight’s shelf</p>
          <div class="story-strip__list">
            <a
              v-for="(story, index) in stories"
              :key="story.slug"
              class="story-chip"
              :href="storyHref(story.slug)"
            >
              <span class="story-chip__number">0{{ index + 1 }}</span>
              <span>
                <strong>{{ story.title }}</strong>
                <small>{{ story.detail }}</small>
              </span>
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M5 12h14m-5-5 5 5-5 5" />
              </svg>
            </a>
          </div>
        </div>
      </section>

      <section class="library section-shell" aria-labelledby="library-heading">
        <div class="section-heading">
          <p class="eyebrow">A little library of big adventures</p>
          <h2 id="library-heading">Familiar stories.<br />Freshly presented.</h2>
          <p>
            Climb the beanstalk with Jack, follow the yellow brick road, and visit three very determined little pigs.
          </p>
        </div>

        <div class="story-grid">
          <a
            v-for="(story, index) in stories"
            :key="`${story.slug}-card`"
            class="story-card"
            :class="`story-card--${index + 1}`"
            :href="storyHref(story.slug)"
          >
            <div class="story-card__art" aria-hidden="true">
              <div v-if="index === 0" class="pigs-art">
                <span class="house house--straw"></span>
                <span class="house house--wood"></span>
                <span class="house house--brick"></span>
                <span class="wind wind--one"></span>
                <span class="wind wind--two"></span>
              </div>
              <div v-else-if="index === 1" class="beanstalk-art">
                <span class="stalk"></span>
                <span class="leaf leaf--one"></span>
                <span class="leaf leaf--two"></span>
                <span class="leaf leaf--three"></span>
                <span class="cloud cloud--one"></span>
                <span class="cloud cloud--two"></span>
                <span class="castle">♜</span>
              </div>
              <div v-else class="oz-art">
                <span class="road"></span>
                <span class="city city--one"></span>
                <span class="city city--two"></span>
                <span class="city city--three"></span>
                <span class="star star--one">✦</span>
                <span class="star star--two">✦</span>
              </div>
            </div>
            <div class="story-card__content">
              <span class="story-card__meta">Classic story · {{ story.readTime }}</span>
              <h3>{{ story.title }}</h3>
              <p>{{ story.description }}</p>
              <span class="story-card__link">
                Read this story
                <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14m-5-5 5 5-5 5" /></svg>
              </span>
            </div>
          </a>
        </div>

        <div class="center-action">
          <a class="text-link" :href="storiesHref">Explore the complete library <span aria-hidden="true">→</span></a>
        </div>
      </section>

      <section id="why" class="why-section">
        <div class="why-section__inner section-shell">
          <div class="why-copy">
            <p class="eyebrow eyebrow--light">Made for stories, not screen time</p>
            <h2>Nothing competing for their attention.</h2>
            <p>
              Panda Pages keeps the words, the pictures and the person reading them at the centre of every page.
            </p>

            <div class="reading-quote">
              <span class="reading-quote__mark" aria-hidden="true">“</span>
              <blockquote>The best stories become part of childhood.</blockquote>
            </div>
          </div>

          <div class="benefit-grid">
            <article v-for="benefit in benefits" :key="benefit.title" class="benefit-card">
              <span class="benefit-card__icon" aria-hidden="true">
                <svg v-if="benefit.icon === 'ad-free'" viewBox="0 0 24 24">
                  <path d="M4 4l16 16M7.5 7.5A7 7 0 0 0 17 17M9.2 4.6A7 7 0 0 1 19.4 14.8" />
                  <path d="M6 10h2l1.5-3h5L16 10h2l1 7h-3M8 17H5l1-7" />
                </svg>
                <svg v-else-if="benefit.icon === 'calm'" viewBox="0 0 24 24">
                  <path d="M4 6.5A2.5 2.5 0 0 1 6.5 4H11v15H6.5A2.5 2.5 0 0 0 4 21.5v-15ZM20 6.5A2.5 2.5 0 0 0 17.5 4H13v15h4.5a2.5 2.5 0 0 1 2.5 2.5v-15Z" />
                </svg>
                <svg v-else-if="benefit.icon === 'share'" viewBox="0 0 24 24">
                  <path d="M8 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM16.5 10a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5ZM3 20v-2a4 4 0 0 1 4-4h2a4 4 0 0 1 4 4v2M14 14h2a4 4 0 0 1 4 4v2" />
                </svg>
                <svg v-else viewBox="0 0 24 24">
                  <rect x="6" y="2.5" width="12" height="19" rx="2" />
                  <path d="M10 18.5h4" />
                </svg>
              </span>
              <h3>{{ benefit.title }}</h3>
              <p>{{ benefit.text }}</p>
            </article>
          </div>
        </div>
      </section>

      <section class="install-section section-shell" aria-labelledby="install-heading">
        <div class="phone-frame" aria-hidden="true">
          <div class="phone-speaker"></div>
          <div class="phone-screen">
            <div class="phone-topbar">
              <img
                class="phone-brand-logo"
                src="/logo.png"
                alt=""
                aria-hidden="true"
              />
              <strong>Panda Pages</strong>
              <span>•••</span>
            </div>
            <div class="phone-story-art">
              <span class="moon"></span>
              <span class="hill hill--back"></span>
              <span class="hill hill--front"></span>
              <span class="tiny-panda"><i></i></span>
            </div>
            <span class="phone-kicker">Tonight’s story</span>
            <h3>The Three Little Pigs</h3>
            <p>Once upon a time, there were three little pigs who were ready to build homes of their own…</p>
            <span class="phone-button">Continue reading</span>
          </div>
        </div>

        <div class="install-copy">
          <p class="eyebrow">Your storybook shelf, one tap away</p>
          <h2 id="install-heading">Install it like an app.<br />Keep reading like a book.</h2>
          <p>
            Add Panda Pages to your phone or tablet for quick access at bedtime, on journeys, or whenever somebody asks for one more story.
          </p>
          <ul class="check-list">
            <li>Quick access from your home screen</li>
            <li>No app store required</li>
            <li>Free to use and always ad-free</li>
          </ul>
          <button class="button button--large button--ink" type="button" @click="handleInstall">
            <svg class="button-icon" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M12 3v11m0 0 4-4m-4 4-4-4M5 17v2a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2" />
            </svg>
            {{ installButtonLabel }}
          </button>
          <p class="install-note">Works in your browser too. Nothing to download before you begin.</p>
        </div>
      </section>

      <section class="closing-section">
        <div class="closing-section__inner section-shell">
          <span class="closing-panda" aria-hidden="true">
            <i class="closing-panda__ear closing-panda__ear--left"></i>
            <i class="closing-panda__ear closing-panda__ear--right"></i>
            <i class="closing-panda__eye closing-panda__eye--left"></i>
            <i class="closing-panda__eye closing-panda__eye--right"></i>
          </span>
          <p class="eyebrow">Ready when you are</p>
          <h2>Choose tonight’s adventure.</h2>
          <p>Open a story, get comfortable and begin.</p>
          <a class="button button--large button--paper" :href="storiesHref">
            Browse all stories
            <svg class="button-icon button-icon--arrow" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M5 12h14m-5-5 5 5-5 5" />
            </svg>
          </a>
        </div>
      </section>
    </main>

    <footer class="site-footer section-shell">
      <a class="brand brand--footer" href="/" aria-label="Panda Pages home">
        <img
          class="brand-logo brand-logo--footer"
          src="/logo.png"
          alt=""
          aria-hidden="true"
        />
        <span class="brand-name">Panda Pages</span>
      </a>
      <p>Classic stories. Quiet reading. Happy imaginations.</p>
      <nav aria-label="Footer links">
        <a :href="storiesHref">Stories</a>
        <a href="#why">Why Panda Pages</a>
        <button type="button" @click="handleInstall">Install app</button>
      </nav>
    </footer>

    <DialogRoot v-model:open="showInstallHelp">
      <DialogPortal>
        <DialogOverlay class="dialog-backdrop" />
        <DialogContent class="install-dialog">
          <DialogClose as-child>
            <button class="dialog-close" type="button" aria-label="Close install instructions">×</button>
          </DialogClose>
          <img
            class="dialog-logo"
            src="/logo.png"
            alt=""
            aria-hidden="true"
          />
          <p class="eyebrow">Keep Panda Pages close</p>
          <DialogTitle as="h2">{{ installHelpTitle }}</DialogTitle>
          <DialogDescription as="p">{{ installHelpText }}</DialogDescription>
          <ol class="install-steps">
            <li v-for="step in installSteps" :key="step">{{ step }}</li>
          </ol>
          <DialogClose as-child>
            <button class="button button--large button--ink button--full" type="button">Got it</button>
          </DialogClose>
        </DialogContent>
      </DialogPortal>
    </DialogRoot>

    <p class="sr-only" aria-live="polite">{{ installStatus }}</p>
  </div>
</template>

<script setup lang="ts">
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

type BenefitIcon = 'ad-free' | 'calm' | 'share' | 'anywhere'
type InstallPlatform = 'ios' | 'android' | 'other'

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

const props = withDefaults(defineProps<{
  storiesHref?: string
  storyBaseHref?: string
}>(), {
  storiesHref: '/library',
  storyBaseHref: '/read',
})

const stories = [
  {
    title: 'The Three Little Pigs',
    slug: 'the-three-little-pigs',
    detail: 'Houses, huffing and courage',
    readTime: '6 min read',
    description: 'Three determined little pigs discover why care, patience and a sturdy home can make all the difference.',
  },
  {
    title: 'Jack and the Beanstalk',
    slug: 'jack-and-the-beanstalk',
    detail: 'A climb above the clouds',
    readTime: '8 min read',
    description: 'A handful of beans leads Jack to an enormous adventure, a castle in the clouds and one very loud giant.',
  },
  {
    title: 'The Wonderful Wizard of Oz',
    slug: 'the-wonderful-wizard-of-oz',
    detail: 'Follow the yellow brick road',
    readTime: '10 min read',
    description: 'Dorothy and her new friends set off through the Land of Oz in search of courage, kindness and a way home.',
  },
]

const benefits = [
  {
    title: 'Completely ad-free',
    text: 'No sponsored recommendations, interruptions or attention-grabbing clutter.',
    icon: 'ad-free',
  },
  {
    title: 'Calm by design',
    text: 'Comfortable type, simple controls and pages that allow the story to breathe.',
    icon: 'calm',
  },
  {
    title: 'Made to share',
    text: 'Built for a child and grown-up to sit together, slow down and read aloud.',
    icon: 'share',
  },
  {
    title: 'Ready anywhere',
    text: 'Read in a browser or add Panda Pages to a home screen for quick access.',
    icon: 'anywhere',
  },
] satisfies Array<{ title: string; text: string; icon: BenefitIcon }>

const deferredPrompt = ref<BeforeInstallPromptEvent | null>(null)
const isInstalled = ref(false)
const platform = ref<InstallPlatform>('other')
const showInstallHelp = ref(false)
const installStatus = ref('')
let installReturnFocus: HTMLElement | null = null

const installButtonLabel = computed(() => {
  if (isInstalled.value) return 'Open Panda Pages'
  if (deferredPrompt.value) return 'Install Panda Pages'
  return 'Add Panda Pages'
})

const installHelpTitle = computed(() => {
  if (platform.value === 'ios') return 'Add to your Home Screen'
  if (platform.value === 'android') return 'Install from your browser menu'
  return 'Add Panda Pages to this device'
})

const installHelpText = computed(() => {
  if (platform.value === 'ios') return 'Safari handles installation directly from the Share menu.'
  if (platform.value === 'android') return 'Your browser can add Panda Pages to your apps in a few taps.'
  return 'Your browser may offer an install or create-shortcut option in its main menu.'
})

const installSteps = computed(() => {
  if (platform.value === 'ios') {
    return ['Open Panda Pages in Safari.', 'Tap the Share button.', 'Choose “Add to Home Screen”, then tap Add.']
  }

  if (platform.value === 'android') {
    return ['Open your browser menu.', 'Choose “Install app” or “Add to Home screen”.', 'Confirm the installation.']
  }

  return ['Open your browser menu.', 'Look for “Install Panda Pages” or “Create shortcut”.', 'Confirm to add it to your device.']
})

function storyHref(slug: string) {
  return `${props.storyBaseHref.replace(/\/$/, '')}/${slug}`
}

function detectPlatform() {
  const userAgent = window.navigator.userAgent.toLowerCase()
  const isAppleMobile = /iphone|ipad|ipod/.test(userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
  const isAndroid = /android/.test(userAgent)

  platform.value = isAppleMobile ? 'ios' : isAndroid ? 'android' : 'other'
  const standaloneNavigator = window.navigator as Navigator & { standalone?: boolean }
  isInstalled.value = window.matchMedia('(display-mode: standalone)').matches || standaloneNavigator.standalone === true
}

function captureInstallPrompt(event: Event) {
  event.preventDefault()
  deferredPrompt.value = event as BeforeInstallPromptEvent
}

function markInstalled() {
  deferredPrompt.value = null
  isInstalled.value = true
  installStatus.value = 'Panda Pages has been installed.'
}

async function handleInstall() {
  if (isInstalled.value) {
    window.location.assign(props.storiesHref)
    return
  }

  if (!deferredPrompt.value) {
    showInstallHelp.value = true
    return
  }

  const prompt = deferredPrompt.value

  try {
    await prompt.prompt()
    const choice = await prompt.userChoice

    if (choice.outcome === 'accepted') {
      installStatus.value = 'Panda Pages installation accepted.'
    } else {
      installStatus.value = 'Installation dismissed. You can continue reading in your browser.'
    }
  } catch {
    installStatus.value = 'Installation could not start. Follow the browser instructions instead.'
    showInstallHelp.value = true
  } finally {
    deferredPrompt.value = null
  }
}

watch(showInstallHelp, async (open, previous) => {
  if (open && !previous) {
    installReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    return
  }

  if (!open && previous) {
    await nextTick()
    installReturnFocus?.focus({ preventScroll: true })
    installReturnFocus = null
  }
})

onMounted(() => {
  detectPlatform()
  window.addEventListener('beforeinstallprompt', captureInstallPrompt)
  window.addEventListener('appinstalled', markInstalled)
})

onBeforeUnmount(() => {
  window.removeEventListener('beforeinstallprompt', captureInstallPrompt)
  window.removeEventListener('appinstalled', markInstalled)
  installReturnFocus = null
})
</script>

<style scoped>
.panda-page,
.panda-page *,
.panda-page *::before,
.panda-page *::after {
  box-sizing: border-box;
}

.panda-page {
  --ink: #11110f;
  --paper: #f4f1e9;
  --white: #fffefa;
  --mist: #e7e3d9;
  --soft-ink: #5c5a54;
  --line: rgba(17, 17, 15, 0.16);
  --serif: "Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif;
  --sans: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  min-height: 100vh;
  overflow: hidden;
  background:
    radial-gradient(circle at 8% 8%, rgba(255, 255, 255, 0.9), transparent 22rem),
    var(--paper);
  color: var(--ink);
  font-family: var(--sans);
  -webkit-font-smoothing: antialiased;
}

.panda-page a {
  color: inherit;
  text-decoration: none;
}

.panda-page button {
  color: inherit;
  font: inherit;
}

.panda-page main:focus {
  outline: none;
}

.section-shell,
.site-header,
.site-footer {
  width: min(1180px, calc(100% - 40px));
  margin-inline: auto;
}

.skip-link {
  position: fixed;
  z-index: 100;
  top: 10px;
  left: 10px;
  transform: translateY(-150%);
  padding: 10px 14px;
  border: 2px solid var(--ink);
  background: var(--white);
  font-weight: 800;
}

.skip-link:focus {
  transform: translateY(0);
}

.site-header {
  position: relative;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 88px;
  border-bottom: 1px solid var(--line);
}

.brand,
.header-actions,
.hero-actions,
.trust-list,
.story-chip,
.story-card__link,
.site-footer nav,
.button {
  display: flex;
  align-items: center;
}

.brand {
  gap: 11px;
  font-weight: 900;
  letter-spacing: -0.04em;
}

.brand-name {
  font-size: 1.1rem;
}

.brand-logo {
  display: block;
  flex: 0 0 auto;
  object-fit: contain;
}

.brand-logo--header {
  width: 42px;
  height: 42px;
}

.brand-logo--footer {
  width: 38px;
  height: 38px;
}

.brand-mark {
  position: relative;
  display: inline-block;
  width: 34px;
  height: 30px;
  border: 2px solid var(--ink);
  border-radius: 48% 48% 52% 52%;
  background: var(--white);
}

.brand-mark--small {
  transform: scale(0.86);
}

.brand-ear,
.brand-eye,
.brand-nose {
  position: absolute;
  display: block;
  background: var(--ink);
}

.brand-ear {
  z-index: -1;
  top: -6px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
}

.brand-ear--left { left: -3px; }
.brand-ear--right { right: -3px; }

.brand-eye {
  top: 10px;
  width: 8px;
  height: 10px;
  border-radius: 50%;
  transform: rotate(18deg);
}

.brand-eye::after {
  content: "";
  position: absolute;
  top: 2px;
  left: 3px;
  width: 2px;
  height: 3px;
  border-radius: 50%;
  background: var(--white);
}

.brand-eye--left { left: 6px; }
.brand-eye--right { right: 6px; transform: rotate(-18deg); }

.brand-nose {
  bottom: 5px;
  left: 50%;
  width: 5px;
  height: 4px;
  transform: translateX(-50%);
  border-radius: 50% 50% 60% 60%;
}

.header-actions {
  gap: 24px;
}

.button {
  justify-content: center;
  gap: 10px;
  border: 1.5px solid var(--ink);
  border-radius: 999px;
  cursor: pointer;
  font-weight: 850;
  letter-spacing: -0.02em;
  transition: transform 180ms ease, box-shadow 180ms ease, background 180ms ease;
}

.button:hover {
  transform: translateY(-2px);
}

.button:focus-visible,
.text-link:focus-visible,
.story-card:focus-visible,
.story-chip:focus-visible,
.site-footer a:focus-visible,
.site-footer button:focus-visible,
.dialog-close:focus-visible {
  outline: 3px solid var(--ink);
  outline-offset: 4px;
}

.button--small {
  min-height: 44px;
  padding: 0 18px;
  font-size: 0.87rem;
}

.button--large {
  min-height: 56px;
  padding: 0 24px;
}

.button--ink {
  background: var(--ink);
  color: var(--white) !important;
  box-shadow: 0 8px 0 rgba(17, 17, 15, 0.12);
}

.button--paper {
  background: transparent;
  color: var(--ink);
}

.button--paper:hover {
  background: var(--white);
}

.button--full {
  width: 100%;
}

.button-icon {
  width: 19px;
  height: 19px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.9;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.button-icon--arrow {
  transition: transform 180ms ease;
}

.button:hover .button-icon--arrow {
  transform: translateX(3px);
}

.text-link {
  width: fit-content;
  border-bottom: 1px solid currentColor;
  font-weight: 800;
  line-height: 1.5;
}

.text-link--nav {
  border-bottom: 0;
  font-size: 0.9rem;
}

.hero {
  display: grid;
  grid-template-columns: minmax(0, 0.96fr) minmax(430px, 1.04fr);
  gap: clamp(30px, 7vw, 100px);
  align-items: center;
  min-height: 720px;
  padding-block: 76px 92px;
}

.eyebrow {
  margin: 0 0 22px;
  font-size: 0.77rem;
  font-weight: 900;
  letter-spacing: 0.16em;
  line-height: 1.4;
  text-transform: uppercase;
}

.eyebrow span {
  margin-right: 6px;
  font-size: 0.6rem;
}

.hero h1,
.section-heading h2,
.why-copy h2,
.install-copy h2,
.closing-section h2,
.install-dialog h2 {
  margin: 0;
  font-family: var(--serif);
  font-weight: 600;
  letter-spacing: -0.065em;
  line-height: 0.94;
}

.hero h1 {
  max-width: 700px;
  font-size: clamp(4.25rem, 7.5vw, 7.6rem);
}

.hero-intro {
  max-width: 590px;
  margin: 32px 0 0;
  color: var(--soft-ink);
  font-family: var(--serif);
  font-size: clamp(1.2rem, 2vw, 1.48rem);
  line-height: 1.55;
}

.hero-actions {
  flex-wrap: wrap;
  gap: 13px;
  margin-top: 38px;
}

.trust-list {
  flex-wrap: wrap;
  gap: 9px 24px;
  margin: 30px 0 0;
  padding: 0;
  color: var(--soft-ink);
  font-size: 0.78rem;
  font-weight: 750;
  list-style: none;
}

.trust-list li {
  position: relative;
  padding-left: 16px;
}

.trust-list li::before {
  content: "";
  position: absolute;
  top: 50%;
  left: 0;
  width: 6px;
  height: 6px;
  transform: translateY(-50%);
  border-radius: 50%;
  background: var(--ink);
}

.hero-visual {
  position: relative;
  min-height: 540px;
  isolation: isolate;
}

.hero-visual::before {
  content: "";
  position: absolute;
  z-index: -2;
  top: 50%;
  left: 50%;
  width: 470px;
  height: 470px;
  transform: translate(-50%, -50%);
  border-radius: 50%;
  background: var(--ink);
}

.hero-visual::after {
  content: "";
  position: absolute;
  z-index: -1;
  top: 50%;
  left: 50%;
  width: 530px;
  height: 530px;
  transform: translate(-50%, -50%);
  border: 1px solid rgba(17, 17, 15, 0.18);
  border-radius: 50%;
}

.orbit {
  position: absolute;
  z-index: -1;
  border: 1px solid rgba(255, 255, 255, 0.22);
  border-radius: 50%;
}

.orbit--one {
  top: 73px;
  right: 56px;
  width: 120px;
  height: 120px;
}

.orbit--two {
  bottom: 35px;
  left: 47px;
  width: 82px;
  height: 82px;
}

.storybook {
  position: absolute;
  top: 50%;
  left: 50%;
  display: grid;
  grid-template-columns: 1fr 1fr;
  width: 490px;
  height: 330px;
  transform: translate(-50%, -49%) rotate(-3deg);
  filter: drop-shadow(0 34px 32px rgba(0, 0, 0, 0.33));
}

.book-page {
  position: relative;
  overflow: hidden;
  background: #fffef8;
}

.book-page--left {
  border-radius: 10px 2px 3px 20px;
  box-shadow: inset -14px 0 18px -18px rgba(0, 0, 0, 0.5);
}

.book-page--right {
  border-radius: 2px 10px 20px 3px;
  box-shadow: inset 14px 0 18px -18px rgba(0, 0, 0, 0.5);
}

.book-page::before {
  content: "";
  position: absolute;
  inset: 0;
  opacity: 0.24;
  background-image: radial-gradient(rgba(17, 17, 15, 0.3) 0.5px, transparent 0.5px);
  background-size: 7px 7px;
}

.book-cover {
  position: absolute;
  z-index: -1;
  bottom: -10px;
  width: 52%;
  height: 99%;
  border-radius: 14px;
  background: #252522;
}

.book-cover--left { left: -9px; transform: rotate(-1deg); }
.book-cover--right { right: -9px; transform: rotate(1deg); }

.book-shadow {
  position: absolute;
  bottom: -27px;
  left: 7%;
  width: 86%;
  height: 32px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.32);
  filter: blur(15px);
}

.book-spine {
  position: absolute;
  z-index: 3;
  top: 0;
  bottom: 0;
  left: 50%;
  width: 2px;
  transform: translateX(-50%);
  background: linear-gradient(to bottom, transparent, rgba(17, 17, 15, 0.22), transparent);
}

.page-kicker,
.once-upon,
.page-line,
.page-number {
  position: absolute;
  z-index: 1;
}

.page-kicker {
  top: 44px;
  left: 36px;
  font-size: 0.52rem;
  font-weight: 900;
  letter-spacing: 0.18em;
  text-transform: uppercase;
}

.once-upon {
  top: 75px;
  left: 36px;
  margin: 0;
  font-family: var(--serif);
  font-size: 2.45rem;
  letter-spacing: -0.07em;
  line-height: 0.93;
}

.page-line {
  left: 37px;
  top: 184px;
  width: 155px;
  height: 3px;
  border-radius: 3px;
  background: rgba(17, 17, 15, 0.18);
  box-shadow: 0 13px rgba(17, 17, 15, 0.13), 0 26px rgba(17, 17, 15, 0.13), 0 39px rgba(17, 17, 15, 0.13);
}

.page-line--short,
.page-line--medium {
  display: none;
}

.page-number {
  bottom: 18px;
  left: 50%;
  font-family: var(--serif);
  font-size: 0.65rem;
}

.panda-portrait {
  position: absolute;
  z-index: 2;
  top: 47%;
  left: 50%;
  width: 170px;
  height: 190px;
  transform: translate(-50%, -50%);
}

.panda-ear {
  position: absolute;
  z-index: 1;
  top: 13px;
  width: 54px;
  height: 54px;
  border-radius: 50%;
  background: var(--ink);
}

.panda-ear--left { left: 11px; }
.panda-ear--right { right: 11px; }

.panda-face {
  position: absolute;
  z-index: 2;
  top: 26px;
  left: 50%;
  width: 145px;
  height: 130px;
  transform: translateX(-50%);
  border: 3px solid var(--ink);
  border-radius: 48% 48% 45% 45%;
  background: #fffef8;
}

.panda-patch {
  position: absolute;
  top: 43px;
  width: 36px;
  height: 45px;
  border-radius: 48%;
  background: var(--ink);
  transform: rotate(20deg);
}

.panda-patch--left { left: 23px; }
.panda-patch--right { right: 23px; transform: rotate(-20deg); }

.panda-patch i {
  position: absolute;
  top: 10px;
  left: 14px;
  width: 7px;
  height: 10px;
  border-radius: 50%;
  background: #fffef8;
}

.panda-muzzle {
  position: absolute;
  bottom: 17px;
  left: 50%;
  width: 52px;
  height: 35px;
  transform: translateX(-50%);
  border-radius: 50%;
  background: #f4f1e9;
}

.panda-muzzle i {
  position: absolute;
  top: 4px;
  left: 50%;
  width: 15px;
  height: 10px;
  transform: translateX(-50%);
  border-radius: 50%;
  background: var(--ink);
}

.panda-body {
  position: absolute;
  z-index: 1;
  bottom: 0;
  left: 50%;
  width: 118px;
  height: 74px;
  transform: translateX(-50%);
  border-radius: 52% 52% 20% 20%;
  background: var(--ink);
}

.spark {
  position: absolute;
  z-index: 2;
  color: var(--ink);
  font-size: 1rem;
}

.spark--one { top: 43px; right: 32px; }
.spark--two { bottom: 53px; left: 25px; font-size: 0.68rem; }
.spark--three { top: 25px; left: 43px; font-size: 2rem; }

.floating-note {
  position: absolute;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 13px 16px;
  border: 1px solid rgba(17, 17, 15, 0.12);
  border-radius: 14px;
  background: rgba(255, 254, 250, 0.96);
  box-shadow: 0 18px 50px rgba(0, 0, 0, 0.14);
  backdrop-filter: blur(8px);
}

.floating-note--top {
  top: 28px;
  right: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 3px;
  transform: rotate(3deg);
}

.floating-note--bottom {
  bottom: 22px;
  left: 2px;
  transform: rotate(-2deg);
}

.floating-note span,
.floating-note small {
  display: block;
  color: var(--soft-ink);
  font-size: 0.65rem;
}

.floating-note strong {
  font-family: var(--serif);
  font-size: 1rem;
}

.mini-panda {
  display: grid !important;
  width: 37px;
  height: 37px;
  place-items: center;
  border-radius: 50%;
  background: var(--ink);
  color: var(--white) !important;
  font-size: 0.8rem !important;
}

.story-strip {
  border-block: 1px solid var(--line);
  background: rgba(255, 254, 250, 0.42);
}

.story-strip__inner {
  display: grid;
  grid-template-columns: 160px 1fr;
  align-items: center;
  min-height: 132px;
}

.story-strip__label {
  margin: 0;
  font-size: 0.7rem;
  font-weight: 900;
  letter-spacing: 0.15em;
  text-transform: uppercase;
}

.story-strip__list {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
}

.story-chip {
  gap: 14px;
  min-height: 70px;
  padding: 0 20px;
  border-left: 1px solid var(--line);
  transition: background 180ms ease;
}

.story-chip:hover {
  background: var(--white);
}

.story-chip__number {
  color: #89867d;
  font-family: var(--serif);
  font-size: 0.8rem;
}

.story-chip strong,
.story-chip small {
  display: block;
}

.story-chip strong {
  margin-bottom: 4px;
  font-family: var(--serif);
  font-size: 0.95rem;
}

.story-chip small {
  color: var(--soft-ink);
  font-size: 0.67rem;
}

.story-chip svg,
.story-card__link svg {
  width: 17px;
  height: 17px;
  margin-left: auto;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 1.8;
  transition: transform 180ms ease;
}

.story-chip:hover svg,
.story-card:hover .story-card__link svg {
  transform: translateX(4px);
}

.library {
  padding-block: 134px 145px;
}

.section-heading {
  display: grid;
  grid-template-columns: 1.2fr 0.8fr;
  column-gap: 60px;
  align-items: end;
  margin-bottom: 64px;
}

.section-heading .eyebrow {
  grid-column: 1 / -1;
}

.section-heading h2,
.why-copy h2,
.install-copy h2,
.closing-section h2 {
  font-size: clamp(3rem, 5vw, 5.2rem);
}

.section-heading > p:last-child {
  max-width: 430px;
  margin: 0 0 4px auto;
  color: var(--soft-ink);
  font-family: var(--serif);
  font-size: 1.16rem;
  line-height: 1.65;
}

.story-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 18px;
}

.story-card {
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 22px;
  background: var(--white);
  transition: transform 220ms ease, box-shadow 220ms ease;
}

.story-card:hover {
  transform: translateY(-7px);
  box-shadow: 0 24px 55px rgba(17, 17, 15, 0.12);
}

.story-card__art {
  position: relative;
  height: 258px;
  overflow: hidden;
  border-bottom: 1px solid var(--line);
  background: #dad6cc;
}

.story-card--2 .story-card__art { background: var(--ink); }
.story-card--3 .story-card__art { background: #ece9e1; }

.story-card__content {
  padding: 29px 29px 31px;
}

.story-card__meta {
  color: #757269;
  font-size: 0.67rem;
  font-weight: 850;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}

.story-card h3 {
  margin: 12px 0 13px;
  font-family: var(--serif);
  font-size: 1.8rem;
  font-weight: 600;
  letter-spacing: -0.045em;
  line-height: 1.05;
}

.story-card p {
  min-height: 82px;
  margin: 0;
  color: var(--soft-ink);
  font-size: 0.9rem;
  line-height: 1.65;
}

.story-card__link {
  gap: 8px;
  margin-top: 24px;
  font-size: 0.78rem;
  font-weight: 850;
}

.pigs-art,
.beanstalk-art,
.oz-art {
  position: absolute;
  inset: 0;
}

.house {
  position: absolute;
  bottom: 38px;
  width: 76px;
  height: 86px;
  border: 3px solid var(--ink);
  background: var(--white);
}

.house::before {
  content: "";
  position: absolute;
  top: -35px;
  left: -10px;
  width: 65px;
  height: 65px;
  border-top: 3px solid var(--ink);
  border-left: 3px solid var(--ink);
  background: inherit;
  transform: rotate(45deg);
}

.house::after {
  content: "";
  position: absolute;
  bottom: 0;
  left: 26px;
  width: 21px;
  height: 38px;
  border: 2px solid var(--ink);
  background: #dad6cc;
}

.house--straw { left: 25px; transform: scale(0.78); }
.house--wood { left: 50%; transform: translateX(-50%) scale(0.9); }
.house--brick { right: 20px; }

.house--brick {
  background-image: linear-gradient(var(--line) 1px, transparent 1px), linear-gradient(90deg, var(--line) 1px, transparent 1px);
  background-size: 24px 16px;
}

.wind {
  position: absolute;
  top: 42px;
  left: 24px;
  width: 85px;
  height: 18px;
  border-top: 3px solid var(--ink);
  border-radius: 50%;
}

.wind--two { top: 70px; left: 2px; width: 60px; }

.stalk {
  position: absolute;
  bottom: -18px;
  left: 50%;
  width: 18px;
  height: 300px;
  border: 2px solid var(--white);
  border-radius: 50%;
  transform: translateX(-50%) rotate(7deg);
}

.leaf {
  position: absolute;
  width: 63px;
  height: 30px;
  border: 2px solid var(--white);
  border-radius: 100% 0 100% 0;
}

.leaf--one { bottom: 62px; left: 37%; transform: rotate(-28deg); }
.leaf--two { bottom: 120px; right: 30%; transform: scaleX(-1) rotate(-23deg); }
.leaf--three { bottom: 176px; left: 37%; transform: rotate(-22deg) scale(0.8); }

.cloud {
  position: absolute;
  width: 74px;
  height: 25px;
  border-radius: 999px;
  background: var(--white);
  opacity: 0.9;
}

.cloud::before,
.cloud::after {
  content: "";
  position: absolute;
  bottom: 0;
  border-radius: 50%;
  background: inherit;
}

.cloud::before { left: 11px; width: 38px; height: 38px; }
.cloud::after { right: 8px; width: 27px; height: 27px; }
.cloud--one { top: 40px; left: 25px; transform: scale(0.8); }
.cloud--two { top: 72px; right: 15px; transform: scale(0.55); }

.castle {
  position: absolute;
  top: 20px;
  left: 50%;
  transform: translateX(-50%);
  color: var(--white);
  font-size: 3.6rem;
}

.road {
  position: absolute;
  top: 128px;
  left: 50%;
  width: 85px;
  height: 185px;
  transform: translateX(-50%) perspective(180px) rotateX(44deg);
  background: repeating-linear-gradient(to bottom, var(--ink) 0 12px, transparent 12px 17px);
  clip-path: polygon(36% 0, 64% 0, 100% 100%, 0 100%);
}

.city {
  position: absolute;
  bottom: 122px;
  width: 42px;
  background: var(--ink);
}

.city::before {
  content: "";
  position: absolute;
  top: -28px;
  left: 50%;
  width: 0;
  height: 0;
  transform: translateX(-50%);
  border-right: 21px solid transparent;
  border-bottom: 29px solid var(--ink);
  border-left: 21px solid transparent;
}

.city--one { left: 31%; height: 56px; }
.city--two { left: 50%; height: 76px; transform: translateX(-50%); }
.city--three { right: 31%; height: 51px; }

.star {
  position: absolute;
  font-size: 0.8rem;
}

.star--one { top: 38px; left: 50px; }
.star--two { top: 68px; right: 48px; }

.center-action {
  display: flex;
  justify-content: center;
  margin-top: 46px;
}

.why-section {
  background: var(--ink);
  color: var(--white);
}

.why-section__inner {
  display: grid;
  grid-template-columns: 0.82fr 1.18fr;
  gap: 90px;
  padding-block: 135px;
}

.eyebrow--light {
  color: rgba(255, 254, 250, 0.64);
}

.why-copy h2 {
  max-width: 520px;
}

.why-copy > p:not(.eyebrow) {
  max-width: 510px;
  margin: 30px 0 0;
  color: rgba(255, 254, 250, 0.65);
  font-family: var(--serif);
  font-size: 1.15rem;
  line-height: 1.65;
}

.reading-quote {
  position: relative;
  margin-top: 67px;
  padding: 30px 0 0 50px;
  border-top: 1px solid rgba(255, 255, 255, 0.16);
}

.reading-quote__mark {
  position: absolute;
  top: 26px;
  left: 0;
  color: rgba(255, 255, 255, 0.38);
  font-family: var(--serif);
  font-size: 4rem;
  line-height: 1;
}

.reading-quote blockquote {
  margin: 0;
  font-family: var(--serif);
  font-size: 1.5rem;
  line-height: 1.3;
}

.benefit-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 1px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 22px;
  background: rgba(255, 255, 255, 0.14);
}

.benefit-card {
  min-height: 260px;
  padding: 35px;
  background: var(--ink);
}

.benefit-card__icon {
  display: grid;
  width: 50px;
  height: 50px;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.18);
  border-radius: 50%;
}

.benefit-card__icon :deep(svg) {
  width: 23px;
  height: 23px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.5;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.benefit-card h3 {
  margin: 35px 0 12px;
  font-family: var(--serif);
  font-size: 1.35rem;
  font-weight: 600;
}

.benefit-card p {
  margin: 0;
  color: rgba(255, 254, 250, 0.58);
  font-size: 0.84rem;
  line-height: 1.65;
}

.install-section {
  display: grid;
  grid-template-columns: 0.83fr 1.17fr;
  gap: clamp(70px, 10vw, 150px);
  align-items: center;
  min-height: 860px;
  padding-block: 120px;
}

.phone-frame {
  position: relative;
  width: 320px;
  height: 650px;
  margin: auto;
  padding: 13px;
  border: 3px solid var(--ink);
  border-radius: 50px;
  background: var(--ink);
  box-shadow: 28px 34px 0 #d8d4ca, 0 35px 70px rgba(17, 17, 15, 0.2);
  transform: rotate(-3deg);
}

.phone-frame::before,
.phone-frame::after {
  content: "";
  position: absolute;
  left: -8px;
  width: 5px;
  border-radius: 3px;
  background: var(--ink);
}

.phone-frame::before { top: 135px; height: 65px; }
.phone-frame::after { top: 215px; height: 42px; }

.phone-speaker {
  position: absolute;
  z-index: 4;
  top: 15px;
  left: 50%;
  width: 94px;
  height: 24px;
  transform: translateX(-50%);
  border-radius: 20px;
  background: var(--ink);
}

.phone-screen {
  position: relative;
  height: 100%;
  overflow: hidden;
  padding: 27px 19px 24px;
  border-radius: 37px;
  background: var(--white);
}

.phone-topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 0.59rem;
}

.phone-topbar strong {
  margin-right: auto;
  margin-left: 7px;
}

.phone-brand-logo {
  display: block;
  width: 22px;
  height: 22px;
  flex: 0 0 auto;
  object-fit: contain;
}

.phone-story-art {
  position: relative;
  height: 250px;
  margin: 22px 0 25px;
  overflow: hidden;
  border-radius: 18px;
  background: var(--ink);
}

.moon {
  position: absolute;
  top: 28px;
  right: 35px;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: var(--white);
  box-shadow: 0 0 0 10px rgba(255, 255, 255, 0.05);
}

.hill {
  position: absolute;
  border-radius: 50% 50% 0 0;
  background: #5b5b57;
}

.hill--back { right: -45px; bottom: -50px; width: 270px; height: 170px; }
.hill--front { left: -65px; bottom: -70px; width: 290px; height: 190px; background: var(--white); }

.tiny-panda {
  position: absolute;
  z-index: 2;
  bottom: 37px;
  left: 75px;
  width: 52px;
  height: 61px;
  border-radius: 48% 48% 42% 42%;
  background: var(--ink);
}

.tiny-panda::before,
.tiny-panda::after {
  content: "";
  position: absolute;
  top: -8px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--ink);
}

.tiny-panda::before { left: -3px; }
.tiny-panda::after { right: -3px; }

.tiny-panda i {
  position: absolute;
  top: 6px;
  left: 50%;
  width: 42px;
  height: 38px;
  transform: translateX(-50%);
  border-radius: 50%;
  background: var(--white);
}

.phone-kicker {
  font-size: 0.55rem;
  font-weight: 900;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}

.phone-screen h3 {
  margin: 9px 0 13px;
  font-family: var(--serif);
  font-size: 1.65rem;
  font-weight: 600;
  letter-spacing: -0.05em;
}

.phone-screen p {
  margin: 0;
  color: var(--soft-ink);
  font-family: var(--serif);
  font-size: 0.78rem;
  line-height: 1.65;
}

.phone-button {
  display: grid;
  height: 44px;
  margin-top: 25px;
  place-items: center;
  border-radius: 999px;
  background: var(--ink);
  color: var(--white);
  font-size: 0.67rem;
  font-weight: 800;
}

.install-copy > p:not(.eyebrow):not(.install-note) {
  max-width: 570px;
  margin: 31px 0 0;
  color: var(--soft-ink);
  font-family: var(--serif);
  font-size: 1.15rem;
  line-height: 1.65;
}

.check-list {
  display: grid;
  gap: 14px;
  margin: 33px 0 36px;
  padding: 0;
  list-style: none;
}

.check-list li {
  position: relative;
  padding-left: 30px;
  font-size: 0.9rem;
  font-weight: 750;
}

.check-list li::before {
  content: "✓";
  position: absolute;
  left: 0;
  display: grid;
  width: 20px;
  height: 20px;
  place-items: center;
  border-radius: 50%;
  background: var(--ink);
  color: var(--white);
  font-size: 0.66rem;
}

.install-note {
  margin: 18px 0 0;
  color: var(--soft-ink);
  font-size: 0.72rem;
}

.closing-section {
  position: relative;
  overflow: hidden;
  background: var(--ink);
  color: var(--white);
  text-align: center;
}

.closing-section::before,
.closing-section::after {
  content: "";
  position: absolute;
  top: 50%;
  width: 360px;
  height: 360px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 50%;
}

.closing-section::before { left: -190px; }
.closing-section::after { right: -190px; }

.closing-section__inner {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 610px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding-block: 100px;
}

.closing-panda {
  position: relative;
  width: 78px;
  height: 66px;
  margin-bottom: 28px;
  border-radius: 48% 48% 45% 45%;
  background: var(--white);
}

.closing-panda__ear {
  position: absolute;
  z-index: -1;
  top: -8px;
  width: 31px;
  height: 31px;
  border: 5px solid var(--white);
  border-radius: 50%;
  background: var(--ink);
}

.closing-panda__ear--left { left: -7px; }
.closing-panda__ear--right { right: -7px; }

.closing-panda__eye {
  position: absolute;
  top: 22px;
  width: 20px;
  height: 27px;
  border-radius: 50%;
  background: var(--ink);
  transform: rotate(20deg);
}

.closing-panda__eye::after {
  content: "";
  position: absolute;
  top: 7px;
  left: 8px;
  width: 5px;
  height: 6px;
  border-radius: 50%;
  background: var(--white);
}

.closing-panda__eye--left { left: 14px; }
.closing-panda__eye--right { right: 14px; transform: rotate(-20deg); }

.closing-section .eyebrow {
  color: rgba(255, 255, 255, 0.55);
}

.closing-section p:not(.eyebrow) {
  margin: 23px 0 32px;
  color: rgba(255, 255, 255, 0.6);
  font-family: var(--serif);
  font-size: 1.13rem;
}

.closing-section .button--paper {
  border-color: var(--white);
  color: var(--white);
}

.closing-section .button--paper:hover {
  background: var(--white);
  color: var(--ink);
}

.site-footer {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: center;
  min-height: 124px;
  gap: 28px;
}

.site-footer p {
  margin: 0;
  color: var(--soft-ink);
  font-family: var(--serif);
  font-size: 0.84rem;
  text-align: center;
}

.site-footer nav {
  justify-content: flex-end;
  gap: 20px;
}

.site-footer nav a,
.site-footer nav button {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  border: 0;
  background: none;
  cursor: pointer;
  font-size: 0.72rem;
  font-weight: 750;
}

.dialog-backdrop {
  position: fixed;
  z-index: 1000;
  inset: 0;
  background: rgba(17, 17, 15, 0.72);
  backdrop-filter: blur(8px);
}

.install-dialog {
  --ink: #11110f;
  --white: #fffefa;
  --soft-ink: #5c5a54;
  --line: rgba(17, 17, 15, 0.16);
  --serif: "Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif;
  --sans: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  position: fixed;
  z-index: 1001;
  top: 50%;
  left: 50%;
  width: min(480px, calc(100% - 40px));
  max-height: calc(100dvh - 40px);
  overflow-y: auto;
  transform: translate(-50%, -50%);
  padding: 42px;
  border: 0;
  border-radius: 24px;
  background: var(--white);
  color: var(--ink);
  font-family: var(--sans);
  box-shadow: 0 30px 90px rgba(0, 0, 0, 0.34);
}

.install-dialog,
.install-dialog * {
  box-sizing: border-box;
}

.dialog-close {
  position: absolute;
  top: 16px;
  right: 16px;
  display: grid;
  width: 44px;
  height: 44px;
  place-items: center;
  border: 1px solid var(--line);
  border-radius: 50%;
  background: transparent;
  cursor: pointer;
  font-size: 1.4rem;
}

.dialog-logo {
  display: block;
  width: 64px;
  height: 64px;
  margin-bottom: 24px;
  object-fit: contain;
}

.install-dialog h2 {
  font-size: 2.8rem;
}

.install-dialog > p:not(.eyebrow) {
  color: var(--soft-ink);
  line-height: 1.6;
}

.install-steps {
  display: grid;
  gap: 12px;
  margin: 26px 0 30px;
  padding-left: 22px;
  color: var(--soft-ink);
  line-height: 1.55;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  clip-path: inset(50%);
}

@media (max-width: 1050px) {
  .hero {
    grid-template-columns: 1fr;
    padding-top: 82px;
  }

  .hero-copy {
    max-width: 760px;
  }

  .hero-visual {
    width: min(610px, 100%);
    margin: 20px auto 0;
  }

  .section-heading,
  .why-section__inner,
  .install-section {
    grid-template-columns: 1fr;
  }

  .section-heading > p:last-child {
    max-width: 600px;
    margin: 28px 0 0;
  }

  .why-section__inner {
    gap: 70px;
  }

  .install-section {
    gap: 100px;
  }

  .install-copy {
    max-width: 700px;
  }
}

@media (max-width: 820px) {
  .text-link--nav {
    display: none;
  }

  .story-strip__inner {
    grid-template-columns: 1fr;
    padding-block: 22px;
  }

  .story-strip__label {
    margin-bottom: 12px;
  }

  .story-strip__list {
    grid-template-columns: 1fr;
  }

  .story-chip {
    border-top: 1px solid var(--line);
    border-left: 0;
  }

  .story-grid {
    grid-template-columns: 1fr;
  }

  .story-card {
    display: grid;
    grid-template-columns: minmax(240px, 0.8fr) 1.2fr;
  }

  .story-card__art {
    height: 100%;
    min-height: 320px;
    border-right: 1px solid var(--line);
    border-bottom: 0;
  }

  .story-card p {
    min-height: 0;
  }

  .site-footer {
    grid-template-columns: 1fr;
    justify-items: center;
    padding-block: 40px;
  }

  .site-footer nav {
    justify-content: center;
  }
}

@media (max-width: 620px) {
  .section-shell,
  .site-header,
  .site-footer {
    width: min(100% - 28px, 1180px);
  }

  .site-header {
    min-height: 74px;
  }

  .brand-name {
    font-size: 0.98rem;
  }

  .header-actions .button {
    padding-inline: 14px;
  }

  .header-actions .button-icon {
    display: none;
  }

  .hero {
    min-height: auto;
    padding-block: 70px 65px;
  }

  .hero h1 {
    font-size: clamp(3.5rem, 18vw, 5.6rem);
  }

  .hero-intro {
    font-size: 1.12rem;
  }

  .hero-actions {
    align-items: stretch;
    flex-direction: column;
  }

  .hero-actions .button {
    width: 100%;
  }

  .trust-list {
    align-items: flex-start;
    flex-direction: column;
  }

  .hero-visual {
    min-height: 410px;
  }

  .hero-visual::before {
    width: 340px;
    height: 340px;
  }

  .hero-visual::after {
    width: 380px;
    height: 380px;
  }

  .storybook {
    width: 340px;
    height: 230px;
  }

  .page-kicker { top: 28px; left: 24px; }
  .once-upon { top: 52px; left: 24px; font-size: 1.75rem; }
  .page-line { top: 128px; left: 24px; width: 105px; }
  .panda-portrait { transform: translate(-50%, -50%) scale(0.68); }

  .floating-note--top {
    top: 0;
    right: 2px;
  }

  .floating-note--bottom {
    bottom: 0;
  }

  .library {
    padding-block: 95px;
  }

  .section-heading h2,
  .why-copy h2,
  .install-copy h2,
  .closing-section h2 {
    font-size: clamp(2.8rem, 14vw, 4.3rem);
  }

  .story-card {
    display: block;
  }

  .story-card__art {
    height: 250px;
    min-height: 0;
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }

  .why-section__inner {
    padding-block: 95px;
  }

  .benefit-grid {
    grid-template-columns: 1fr;
  }

  .benefit-card {
    min-height: 230px;
  }

  .install-section {
    min-height: auto;
    padding-block: 100px;
  }

  .phone-frame {
    width: 270px;
    height: 550px;
    border-radius: 42px;
    box-shadow: 18px 25px 0 #d8d4ca, 0 25px 55px rgba(17, 17, 15, 0.2);
  }

  .phone-screen {
    border-radius: 30px;
  }

  .phone-story-art {
    height: 205px;
  }

  .phone-screen h3 {
    font-size: 1.35rem;
  }

  .phone-button {
    margin-top: 17px;
  }

  .closing-section__inner {
    min-height: 540px;
  }

  .install-dialog {
    padding: 35px 25px 28px;
  }

  .install-dialog h2 {
    font-size: 2.35rem;
  }
}

@media (prefers-reduced-motion: no-preference) {
  .storybook {
    animation: book-float 5s ease-in-out infinite;
  }

  .floating-note--top {
    animation: note-float 6s ease-in-out infinite;
  }

  .floating-note--bottom {
    animation: note-float 6s 1.2s ease-in-out infinite reverse;
  }

  @keyframes book-float {
    0%, 100% { transform: translate(-50%, -49%) rotate(-3deg); }
    50% { transform: translate(-50%, -52%) rotate(-1.5deg); }
  }

  @keyframes note-float {
    0%, 100% { translate: 0 0; }
    50% { translate: 0 -7px; }
  }
}
</style>
