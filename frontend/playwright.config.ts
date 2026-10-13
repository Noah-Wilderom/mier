import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  // Software WebKit rendering and language tooling can exhaust a small desktop
  // or CI host when several browser engines run together.
  workers: 1,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: { baseURL: 'http://127.0.0.1:9255', viewport: { width: 1440, height: 1000 }, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
    // Headless Linux hosts often expose a /dev/dri node without GPU permission.
    // Mesa software rendering keeps WebKit's paint/animation frames responsive.
    { name: 'webkit', use: { browserName: 'webkit', launchOptions: { env: process.platform === 'linux' ? { ...process.env, LIBGL_ALWAYS_SOFTWARE: '1' } : undefined } } },
  ],
  webServer: { command: 'npm run dev -- --port 9255', url: 'http://127.0.0.1:9255', reuseExistingServer: false, timeout: 60000 },
})
