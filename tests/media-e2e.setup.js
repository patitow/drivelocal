// Sobe um servidor DriveLocal isolado para o e2e de mídia (playwright.media.config.js):
// fixtures geradas com ffmpeg, binário Go e dist do client em uma pasta temporária,
// usuário admin/admin criado só ali (NODE_ENV=development). Nunca toca nos dados reais.
const { execFileSync, spawn } = require('child_process')
const fs = require('fs')
const os = require('os')
const path = require('path')

const ROOT = path.resolve(__dirname, '..')
const PORT = process.env.MEDIA_E2E_PORT || '18732'
const WORK = process.env.MEDIA_E2E_DIR || path.join(os.tmpdir(), 'drivelocal-media-e2e')
// código Go usado para fixtures e binário (padrão: server-go do repositório)
const GO_DIR = process.env.MEDIA_E2E_GO_DIR || path.join(ROOT, 'server-go')

async function waitHealth(url, ms) {
  const end = Date.now() + ms
  while (Date.now() < end) {
    try {
      const r = await fetch(url)
      if (r.ok) return
    } catch { /* ainda subindo */ }
    await new Promise((r) => setTimeout(r, 300))
  }
  throw new Error(`servidor de teste não respondeu em ${url}`)
}

module.exports = async () => {
  fs.mkdirSync(WORK, { recursive: true })
  const media = path.join(WORK, 'media')
  const run = (cmd, args, opts) => execFileSync(cmd, args, { stdio: 'inherit', ...opts })

  // 1) fixtures (o próprio teste Go sabe gerá-las)
  run('go', ['test', '-count=1', '-run', 'TestWriteE2EFixtures', '.'], {
    cwd: GO_DIR,
    env: { ...process.env, E2E_FIXTURES_DIR: path.join(media, 'E2E') },
  })
  // 2) binário e client
  const bin = path.join(WORK, process.platform === 'win32' ? 'drivelocal-e2e.exe' : 'drivelocal-e2e')
  run('go', ['build', '-o', bin, '.'], { cwd: GO_DIR })
  if (!process.env.MEDIA_E2E_SKIP_CLIENT_BUILD) {
    run('npx', ['vite', 'build', '--outDir', path.join(WORK, 'dist'), '--emptyOutDir'], { cwd: path.join(ROOT, 'client'), shell: true })
  }
  // estado limpo a cada execução: usuários e cache de legendas/fontes
  fs.rmSync(path.join(WORK, 'server'), { recursive: true, force: true })
  fs.rmSync(path.join(WORK, 'cache'), { recursive: true, force: true })
  fs.mkdirSync(path.join(WORK, 'server', 'data'), { recursive: true })

  const log = fs.openSync(path.join(WORK, 'server.log'), 'w')
  const proc = spawn(bin, [], {
    cwd: WORK,
    env: { ...process.env, PORT, MEDIA_PATH: media, MEDIA_CACHE_DIR: path.join(WORK, 'cache'), NODE_ENV: 'development' },
    stdio: ['ignore', log, log],
  })
  process.env.MEDIA_E2E_URL = `http://127.0.0.1:${PORT}`
  await waitHealth(`${process.env.MEDIA_E2E_URL}/api/health`, 30000)

  // um login só para a suíte inteira (o servidor limita tentativas de login por minuto)
  const { chromium } = require('@playwright/test')
  const browser = await chromium.launch({ channel: 'chrome', headless: true })
  const page = await browser.newPage()
  await page.goto(process.env.MEDIA_E2E_URL)
  await page.fill('#username', 'admin')
  await page.fill('#password', 'admin')
  await page.keyboard.press('Enter')
  await page.waitForFunction(() => !!localStorage.getItem('auth_token'), null, { timeout: 15000 })
  await page.context().storageState({ path: path.join(WORK, 'auth-state.json') })
  await browser.close()

  return async () => {
    proc.kill()
  }
}
