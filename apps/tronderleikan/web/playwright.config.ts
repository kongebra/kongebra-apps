import { defineConfig, devices } from '@playwright/test'

// Røyktester kjører mot den bygde SSR-serveren (.output) + en mock av
// Go-tjenestene (tests/mock-services.mjs). Krever `npm run build` først.
const APP_PORT = 3210
const MOCK_PORT = 4599

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://127.0.0.1:${APP_PORT}`,
    trace: 'on-first-retry',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: [
    {
      command: `node tests/mock-services.mjs`,
      port: MOCK_PORT,
      env: { MOCK_PORT: String(MOCK_PORT) },
      reuseExistingServer: !process.env.CI,
    },
    {
      command: `node .output/server/index.mjs`,
      port: APP_PORT,
      reuseExistingServer: !process.env.CI,
      env: {
        PORT: String(APP_PORT),
        HOST: '127.0.0.1',
        PLATFORM_URL: `http://127.0.0.1:${MOCK_PORT}`,
        ROSTER_URL: `http://127.0.0.1:${MOCK_PORT}`,
        COMPETITION_URL: `http://127.0.0.1:${MOCK_PORT}`,
        AUTH_ISSUER: `http://127.0.0.1:${MOCK_PORT}`,
        AUTH_CLIENT_ID: 'tronderleikan-web',
        SESSION_SECRET: 'test-session-secret-min-32-tegn-lang!!',
        NODE_ENV: 'production',
      },
    },
  ],
})
