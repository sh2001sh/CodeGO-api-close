import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  testIgnore: ['**/zz-*.spec.ts'],
  fullyParallel: true,
  workers: 4,
  use: { baseURL: process.env.V3_REAL_URL || 'http://127.0.0.1:3100', trace: 'retain-on-failure' },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: process.env.V3_REAL_URL
    ? undefined
    : {
        // CI serves the frontend job's exact artifact; local runs build it first.
        command: process.env.CI
          ? 'node node_modules/@rsbuild/core/bin/rsbuild.js preview'
          : 'node node_modules/@rsbuild/core/bin/rsbuild.js build && node node_modules/@rsbuild/core/bin/rsbuild.js preview',
        url: 'http://127.0.0.1:3100/sign-in',
        reuseExistingServer: false,
        timeout: 60_000,
      },
})
