import { defineConfig, devices } from '@playwright/test'

// Røyktester kjører mot den bygde SSR-serveren (.output). Krever `npm run build`
// først. Appen serveres under /admin (basePath), så testene navigerer til
// /admin/*. Testene er bakgrunns-frie: gaten redirecter FØR noe tjenestekall, og
// healthz trenger ingen backend. Env-verdiene under er derfor rene plassholdere
// (aldri kontaktet); full backend-E2E hører til arbeidspakke 1.6.
const APP_PORT = 3211

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
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: `node .output/server/index.mjs`,
      port: APP_PORT,
      reuseExistingServer: !process.env.CI,
      env: {
        PORT: String(APP_PORT),
        HOST: '127.0.0.1',
        PLATFORM_URL: 'http://127.0.0.1:9',
        AUTH_ISSUER: 'http://127.0.0.1:9',
        AUTH_CLIENT_ID: 'tronderleikan-admin',
        SESSION_SECRET: 'test-session-secret-min-32-tegn-lang!!',
        NODE_ENV: 'production',
      },
    },
  ],
})
