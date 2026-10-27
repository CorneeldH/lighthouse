import { defineConfig, devices } from '@playwright/test'

// The e2e tests run against a built lighthouse binary (LIGHTHOUSE_BIN), not the Vite dev server.
export default defineConfig({
  testDir: 'e2e',
  // vitest only picks up *.test.* and *.spec.*, so it never runs these.
  testMatch: '**/*.e2e.ts',
  retries: 0,
  reporter: 'list',
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
