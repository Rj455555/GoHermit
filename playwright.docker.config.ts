import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e-docker',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'line',
  use: {
    baseURL: process.env.GOHERMIT_DOCKER_BASE_URL ?? 'http://127.0.0.1:18787',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [{ name: 'docker-chromium', use: { ...devices['Desktop Chrome'] } }],
})
