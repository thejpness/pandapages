import { env } from 'node:process'
import { defineConfig, devices } from '@playwright/test'

const inCI = Boolean(env.CI)

export default defineConfig({
  testDir: './tests/browser',
  outputDir: './test-results',
  fullyParallel: true,
  forbidOnly: inCI,
  retries: inCI ? 1 : 0,
  workers: inCI ? 2 : undefined,
  timeout: 30_000,
  expect: {
    timeout: 6_000,
  },
  reporter: inCI
    ? [
        ['line'],
        ['html', { open: 'never', outputFolder: 'playwright-report' }],
      ]
    : [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4173',
    serviceWorkers: 'block',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
      },
    },
    {
      name: 'webkit-paged-core',
      grep: /@paged-core/,
      use: {
        ...devices['iPhone 13'],
      },
    },
  ],
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !inCI,
    stdout: 'ignore',
    stderr: 'pipe',
    timeout: 30_000,
  },
})
