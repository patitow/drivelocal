// @ts-check
// E2E de reprodução de mídia contra um servidor isolado (ver tests/media-e2e.setup.js).
// Uso: npx playwright test -c playwright.media.config.js
const { defineConfig } = require('@playwright/test')
const os = require('os')
const path = require('path')

const WORK = process.env.MEDIA_E2E_DIR || path.join(os.tmpdir(), 'drivelocal-media-e2e')

module.exports = defineConfig({
  testDir: './tests',
  testMatch: ['media-playback.spec.js', 'media-sync.spec.js'],
  globalSetup: require.resolve('./tests/media-e2e.setup.js'),
  // o servidor limita transcodes simultâneos: um teste por vez
  workers: 1,
  timeout: 120 * 1000,
  reporter: 'list',
  use: {
    baseURL: `http://127.0.0.1:${process.env.MEDIA_E2E_PORT || '18732'}`,
    // Chrome real: o Chromium do Playwright não tem H.264/AAC
    channel: 'chrome',
    // sessão criada uma vez no globalSetup
    storageState: path.join(WORK, 'auth-state.json'),
    headless: true,
    viewport: { width: 1280, height: 720 },
    launchOptions: { args: ['--autoplay-policy=no-user-gesture-required'] },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
})
