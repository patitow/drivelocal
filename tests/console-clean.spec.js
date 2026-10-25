// Guarda de console: navega pelas telas, usa as buscas e toca um vídeo; falha com qualquer erro no
// console, exceção não tratada ou resposta 5xx vinda do app. Roda no servidor isolado das fixtures:
// npx playwright test -c playwright.media.config.js
const { test, expect } = require('@playwright/test')

// Ruído do navegador fora do controle do app (não esconde erro do nosso bundle)
const IGNORED = [
  /favicon\.ico/,
  /Failed to load resource: the server responded with a status of 4\d\d/, // 401/404 esperados ficam a cargo dos testes de cada fluxo
]

function watchConsole(page) {
  const problems = []
  page.on('console', (m) => {
    if (m.type() !== 'error') return
    const text = m.text()
    if (!IGNORED.some((re) => re.test(text))) problems.push(`console.error: ${text} @ ${m.location().url || '?'}`)
  })
  page.on('pageerror', (e) => problems.push(`exceção: ${e.message}`))
  page.on('response', (r) => {
    if (r.status() >= 500) problems.push(`${r.status()} ${r.url()}`)
  })
  return problems
}

const SCREENS = ['/colecao', '/colecao/search', '/colecao/topics', '/colecao/playlists', '/colecao/favorites', '/colecao/manage', '/drive', '/drive/E2E', '/configuracoes']

test('telas sem erro no console', async ({ page }) => {
  const problems = watchConsole(page)
  for (const url of SCREENS) {
    await page.goto(url)
    await page.waitForLoadState('networkidle')
  }
  expect(problems).toEqual([])
})

test('buscas digitadas, apagadas e deixadas paradas sem erro', async ({ page }) => {
  const problems = watchConsole(page)

  await page.goto('/colecao/search')
  const catalog = page.getByPlaceholder('Buscar séries, filmes, criadores')
  await catalog.fill('direct')
  await page.waitForTimeout(1500)
  await catalog.fill('')
  await page.waitForTimeout(5000) // barra de busca parada

  await page.goto('/drive/E2E')
  const files = page.getByPlaceholder('Buscar arquivos...')
  await files.fill('remux')
  await page.waitForTimeout(1500)
  await files.fill('')
  await page.waitForTimeout(5000)

  expect(problems).toEqual([])
})

test('abrir, tocar e fechar um vídeo sem erro', async ({ page }) => {
  const problems = watchConsole(page)
  await page.goto('/drive/E2E')
  await page.getByText('direct_h264_aac.mkv', { exact: true }).first().click()
  await page.waitForSelector('video[src]', { timeout: 15000 })
  await page.evaluate(() => document.querySelector('video').play())
  await expect.poll(() => page.evaluate(() => document.querySelector('video').currentTime), { timeout: 20000 }).toBeGreaterThan(1)
  await page.keyboard.press('Escape')
  await page.waitForTimeout(1000)
  expect(problems).toEqual([])
})
