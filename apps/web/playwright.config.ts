import { defineConfig, devices } from '@playwright/test';

/**
 * E2E smoke test of the main flow against a real API + Postgres.
 *
 * Requires a Postgres database (E2E_DATABASE_URL). Playwright starts the Go
 * API (with the probe scheduler off so seeded state is deterministic) and a
 * production build of the web app, then drives Chromium.
 */
const API_PORT = 8090;
const WEB_PORT = 4173;
const databaseUrl =
  process.env.E2E_DATABASE_URL ?? 'postgres://pulseops:pulseops@localhost:5432/pulseops_e2e?sslmode=disable';

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://localhost:${WEB_PORT}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    {
      name: 'mobile',
      dependencies: ['desktop'],
      use: { ...devices['Desktop Chrome'], viewport: { width: 375, height: 812 } },
    },
  ],
  webServer: [
    {
      command: process.env.E2E_API_BIN ?? 'go run ./cmd/pulseops',
      cwd: '../api',
      url: `http://localhost:${API_PORT}/readyz`,
      timeout: 180_000,
      reuseExistingServer: false,
      stdout: 'ignore',
      stderr: 'pipe',
      env: {
        DATABASE_URL: databaseUrl,
        PORT: String(API_PORT),
        SCHEDULER_ENABLED: 'false',
        DEMO_ENABLED: 'true',
        DEMO_RESET_ON_BOOT: 'true',
        CORS_ORIGINS: `http://localhost:${WEB_PORT}`,
        PUBLIC_APP_URL: `http://localhost:${WEB_PORT}`,
        LOG_LEVEL: 'warn',
      },
    },
    {
      command: `npx vite build && npx vite preview --port ${WEB_PORT} --strictPort`,
      url: `http://localhost:${WEB_PORT}`,
      timeout: 120_000,
      reuseExistingServer: false,
      env: { VITE_API_URL: `http://localhost:${API_PORT}` },
    },
  ],
});
