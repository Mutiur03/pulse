import { defineConfig } from '@playwright/test';

// Runs against a running stack (make compose-up, then pnpm seed).
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 15_000 },
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.WEB_URL ?? 'http://localhost:3000',
    channel: process.env.PW_CHANNEL || undefined,
    viewport: { width: 1440, height: 900 },
    colorScheme: 'dark',
    trace: 'retain-on-failure',
  },
});
