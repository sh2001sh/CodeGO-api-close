import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  workers: 4,
  use: { baseURL: process.env.V3_REAL_URL ?? 'http://127.0.0.1:3100', trace: 'retain-on-failure' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: process.env.V3_REAL_URL
    ? undefined
    : {
        command: 'node node_modules/@rsbuild/core/bin/rsbuild.js dev',
        url: 'http://127.0.0.1:3100/sign-in',
        reuseExistingServer: !process.env.CI,
        timeout: 60_000,
      },
})
