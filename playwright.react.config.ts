import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e-react',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: 'line',
  use: {
    baseURL: 'http://127.0.0.1:4174',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [{ name: 'react-chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'pnpm build && node tests/e2e-react/static-server.mjs',
    url: 'http://127.0.0.1:4174/dashboard',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
