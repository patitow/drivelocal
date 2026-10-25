// O Chrome só destrói o player de um <video> quando o elemento é coletado e recusa vídeo novo
// quando passam de 1000 vivos na aba ("too many WebMediaPlayers"): tudo para de tocar até
// recarregar. Todo <video> que sai da tela precisa soltar o player na hora.
const { test, expect } = require('@playwright/test')

// <video> que ainda existem no heap depois de forçar a coleta (vivos ou fora do DOM)
async function videosInHeap(page) {
  const cdp = await page.context().newCDPSession(page)
  for (let i = 0; i < 3; i++) await cdp.send('HeapProfiler.collectGarbage')
  const chunks = []
  cdp.on('HeapProfiler.addHeapSnapshotChunk', (c) => chunks.push(c.chunk))
  await cdp.send('HeapProfiler.takeHeapSnapshot', { reportProgress: false })
  await cdp.detach()
  const { snapshot: { meta }, nodes, strings } = JSON.parse(chunks.join(''))
  const nf = meta.node_fields.length
  const iType = meta.node_fields.indexOf('type')
  const iName = meta.node_fields.indexOf('name')
  const native = meta.node_types[0].indexOf('native')
  let n = 0
  for (let i = 0; i < nodes.length; i += nf) {
    if (nodes[i + iType] === native && /^(Detached )?<video/.test(strings[nodes[i + iName]])) n++
  }
  return n
}

// abre e fecha o player; espera pelo locator, que não guarda o elemento como o ElementHandle de waitForSelector
async function openAndClose(page) {
  await page.getByText('direct_h264_aac.mkv', { exact: true }).first().click()
  await page.locator('video[src]').waitFor({ timeout: 15000 })
  await expect.poll(() => page.evaluate(() => document.querySelector('video').readyState), { timeout: 15000 }).toBeGreaterThan(0)
  await page.keyboard.press('Escape')
  await expect(page.locator('video')).toHaveCount(0)
}

// marca todo elemento de mídia que recebeu src, para achá-lo depois de sair do DOM
async function trackMedia(page) {
  await page.addInitScript(() => {
    const seen = (window.__media = [])
    const note = (el) => { if (!seen.includes(el)) seen.push(el) }
    const d = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'src')
    Object.defineProperty(HTMLMediaElement.prototype, 'src', { ...d, set(v) { if (v) note(this); d.set.call(this, v) } })
    const setAttr = Element.prototype.setAttribute
    Element.prototype.setAttribute = function (n, v) {
      if (this instanceof HTMLMediaElement && n === 'src' && v) note(this)
      return setAttr.call(this, n, v)
    }
  })
}

// fora do DOM e ainda com player (networkState != NETWORK_EMPTY)
const leaked = (page) => page.evaluate(() => window.__media.filter((el) => !el.isConnected && el.networkState !== HTMLMediaElement.NETWORK_EMPTY).length)

test('fechar o player solta o <video>', async ({ page }) => {
  await trackMedia(page)
  await page.goto('/drive/E2E')
  for (let i = 0; i < 3; i++) await openAndClose(page)
  expect(await page.evaluate(() => window.__media.length)).toBeGreaterThanOrEqual(3)
  expect(await leaked(page)).toBe(0)
})

// sem o rastreador (que guarda os elementos de propósito): fechado, o <video> some do heap
test('o <video> fechado é coletado', async ({ page }) => {
  test.setTimeout(90000)
  await page.goto('/drive/E2E')
  for (let i = 0; i < 3; i++) await openAndClose(page)
  expect(await videosInHeap(page)).toBe(0)
})
