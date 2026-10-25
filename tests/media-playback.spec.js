// E2E do player com fixtures geradas pelo ffmpeg (server-go/media_fixtures_test.go).
// Roda contra o servidor isolado de tests/media-e2e.setup.js: npx playwright test -c playwright.media.config.js
// (o login é feito uma vez no setup e reaproveitado via storageState)
const { test, expect } = require('@playwright/test')

const FOLDER = 'E2E'

async function openVideo(page, name) {
  await page.goto(`/drive/${FOLDER}`)
  await page.getByText(name, { exact: true }).first().click()
  await page.waitForSelector('video[src]', { timeout: 15000 })
}

// estado do <video> em tempo de arquivo (offset do stream vem do ?start=)
function videoState(page) {
  return page.evaluate(() => {
    const v = document.querySelector('video')
    const start = Number(new URL(v.currentSrc || v.src, location.href).searchParams.get('start') || 0)
    return {
      mode: v.dataset.playbackMode,
      src: v.currentSrc,
      t: start + v.currentTime,
      local: v.currentTime,
      w: v.videoWidth,
      audioBytes: v.webkitAudioDecodedByteCount,
      paused: v.paused,
      err: v.error && v.error.code,
    }
  })
}

async function play(page) {
  await expect(page.getByText('Carregando vídeo...')).toBeHidden({ timeout: 60000 })
  await page.locator('video').hover()
  await page.getByRole('button', { name: 'Reproduzir' }).click()
}

// espera o tempo de arquivo passar de `min` (reprodução avançando de verdade)
async function waitPlayingPast(page, min, timeout = 60000) {
  await expect.poll(async () => (await videoState(page)).t, { timeout, intervals: [250] }).toBeGreaterThan(min)
}

// setas só valem com o foco fora dos controles (o botão de play fica focado após o clique)
async function pressArrows(page, n) {
  await page.evaluate(() => document.activeElement && document.activeElement.blur())
  for (let i = 0; i < n; i++) await page.keyboard.press('ArrowRight')
}

async function pickMenu(page, button, item) {
  await page.locator('video').hover()
  await page.getByRole('button', { name: button, exact: true }).click()
  await page.getByRole('menuitemradio', { name: item }).click()
}

// conta pixels de uma cor no recorte do container do vídeo, com o <video> escondido
// (o canvas da legenda fica sozinho sobre o fundo preto)
async function countColor(page, kind) {
  await page.evaluate(() => { document.querySelector('video').style.opacity = '0' })
  await page.waitForTimeout(400)
  const shot = await page.locator('video').screenshot()
  await page.evaluate(() => { document.querySelector('video').style.opacity = '' })
  return page.evaluate(async ({ b64, kind }) => {
    const blob = await (await fetch(`data:image/png;base64,${b64}`)).blob()
    const bmp = await createImageBitmap(blob)
    const c = new OffscreenCanvas(bmp.width, bmp.height)
    const ctx = c.getContext('2d')
    ctx.drawImage(bmp, 0, 0)
    const d = ctx.getImageData(0, 0, bmp.width, bmp.height).data
    let n = 0
    for (let i = 0; i < d.length; i += 4) {
      const [r, g, b] = [d[i], d[i + 1], d[i + 2]]
      if (kind === 'yellow' ? r > 200 && g > 200 && b < 90 : r > 170 && g < 90 && b < 90) n++
    }
    return n
  }, { b64: shot.toString('base64'), kind })
}

// modo esperado com as caps do Chrome desta máquina (HEVC 10-bit em hardware)
const MATRIX = [
  { name: 'direct_h264_aac.mkv', mode: 'direct' },
  { name: 'remux_h264_ac3.mkv', mode: 'remux' }, // AC-3 toca mudo no Chrome → áudio vira AAC
  { name: 'remux_h264_ac3only.mkv', mode: 'remux' },
  { name: 'remux_h264_aac.m2ts', mode: 'remux' }, // MPEG-TS (Blu-ray) não abre no <video>
  { name: 'transcode_mpeg4_mp3.avi', mode: 'transcode' }, // MPEG-4 Part 2 sem imagem no Chrome
  { name: 'transcode_hevc10_eac3.mkv', mode: 'remux' }, // HEVC 10 ok aqui; só o E-AC-3 precisa de remux
  { name: 'direct_h264_noaudio.mp4', mode: 'direct', silent: true }, // sem faixa de áudio: toca só a imagem
]

for (const fx of MATRIX) {
  test(`toca ${fx.name} em modo ${fx.mode} com imagem${fx.silent ? '' : ' e som'}`, async ({ page }) => {
    await openVideo(page, fx.name)
    await expect.poll(async () => (await videoState(page)).mode).toBe(fx.mode)
    await play(page)
    await waitPlayingPast(page, 2)
    const s = await videoState(page)
    expect(s.err).toBeNull()
    expect(s.w).toBeGreaterThan(0)
    if (!fx.silent) expect(s.audioBytes).toBeGreaterThan(0)
    await expect(page.getByText('Este vídeo não pôde ser reproduzido')).toHaveCount(0)
  })
}

test('seek em direct, remux e transcode continua tocando a partir do ponto pedido', async ({ page }) => {
  for (const name of ['direct_h264_aac.mkv', 'remux_h264_ac3.mkv', 'transcode_mpeg4_mp3.avi']) {
    await openVideo(page, name)
    await play(page)
    await waitPlayingPast(page, 1)
    // setas = ±5 s; três vezes leva para ~16 s
    await pressArrows(page, 3)
    await waitPlayingPast(page, 15.5)
    const s = await videoState(page)
    expect(s.t, name).toBeLessThan(25)
    if (name !== 'direct_h264_aac.mkv') expect(s.src, name).toContain('start=')
    await page.keyboard.press('Escape')
  }
})

test('troca de faixa de áudio mantém a posição e passa a remuxar', async ({ page }) => {
  await openVideo(page, 'direct_h264_aac.mkv')
  await play(page)
  await waitPlayingPast(page, 3)
  const before = (await videoState(page)).t
  await pickMenu(page, 'Faixa de áudio', /Inglês/)
  await expect.poll(async () => (await videoState(page)).src).toContain('audio=1')
  await waitPlayingPast(page, before + 1)
  const s = await videoState(page)
  expect(s.mode).toBe('remux')
  expect(s.t).toBeGreaterThan(before - 0.5)
  expect(s.audioBytes).toBeGreaterThan(0)
  // volta para a faixa original: modo direct de novo
  await pickMenu(page, 'Faixa de áudio', /Japonês/)
  await expect.poll(async () => (await videoState(page)).mode).toBe('direct')
  await waitPlayingPast(page, s.t + 0.5)
})

test('legendas SRT (embutida e externa), ASS (JASSUB) e PGS (libpgs)', async ({ page }) => {
  const fontRequests = []
  page.on('response', (r) => { if (r.url().includes('/api/media-font/')) fontRequests.push(r.status()) })
  await openVideo(page, 'direct_h264_aac.mkv')
  await play(page)
  await waitPlayingPast(page, 1)

  await pickMenu(page, 'Legendas', /Português · SRT/)
  await expect(page.getByTestId('subtitle-cue')).toHaveText('Legenda SRT')

  await pickMenu(page, 'Legendas', /arquivo/)
  await expect(page.getByTestId('subtitle-cue')).toHaveText('Legenda externa')

  await pickMenu(page, 'Legendas', /Inglês · ASS/)
  await expect(page.getByTestId('subtitle-cue')).toHaveCount(0)
  await expect(page.locator('canvas[data-testid="ass-canvas"]')).toHaveCount(1)
  await expect.poll(() => countColor(page, 'yellow'), { timeout: 20000 }).toBeGreaterThan(200)
  expect(fontRequests).toContain(200) // fonte anexada ao mkv servida pelo endpoint

  await pickMenu(page, 'Legendas', /Espanhol · PGS/)
  await expect(page.locator('canvas[data-testid="ass-canvas"]')).toHaveCount(0)
  await expect.poll(() => countColor(page, 'red'), { timeout: 20000 }).toBeGreaterThan(500)

  await pickMenu(page, 'Legendas', 'Desativada')
  await expect.poll(() => countColor(page, 'red')).toBeLessThan(50)

  // escolha persistida para os próximos arquivos
  expect(await page.evaluate(() => localStorage.getItem('player_sub_lang'))).toBe('off')
})

test('ASS segue o tempo do arquivo no modo transcode (timeOffset) e a preferência abre a legenda sozinha', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => localStorage.setItem('player_sub_lang', 'por'))
  // caps=none simula um navegador sem HEVC: o servidor manda transcodificar
  await page.route('**/api/media-info/**', (route) => {
    const url = new URL(route.request().url())
    url.searchParams.set('caps', 'none')
    return route.continue({ url: url.toString() })
  })
  await openVideo(page, 'transcode_hevc10_eac3.mkv')
  await expect.poll(async () => (await videoState(page)).mode).toBe('transcode')
  await play(page)
  await waitPlayingPast(page, 1)
  await pressArrows(page, 2)
  await waitPlayingPast(page, 10.5)
  expect((await videoState(page)).src).toContain('start=')
  await expect(page.locator('canvas[data-testid="ass-canvas"]')).toHaveCount(1)
  await expect.poll(() => countColor(page, 'yellow'), { timeout: 20000 }).toBeGreaterThan(200)
})

test('fallback: servidor diz direct mas o navegador não toca → sobe para remux/transcode', async ({ page }) => {
  // força a decisão errada para simular um navegador que mentiu no canPlayType
  await page.route('**/api/media-info/**', async (route) => {
    const res = await route.fetch()
    const body = await res.json()
    body.playback = { mode: 'direct', reason: 'forçado pelo teste' }
    body.audio = body.audio.map((a) => ({ ...a, playable: true }))
    await route.fulfill({ response: res, json: body })
  })
  // AVI com MPEG-4: o Chrome não abre o contêiner → erro → remux (servidor sobe para transcode)
  await openVideo(page, 'transcode_mpeg4_mp3.avi')
  await expect.poll(async () => (await videoState(page)).mode, { timeout: 30000 }).not.toBe('direct')
  await play(page)
  await waitPlayingPast(page, 2)
  expect((await videoState(page)).w).toBeGreaterThan(0)

  // só AC-3: o Chrome recusa o arquivo (erro de formato) → remux com áudio em AAC
  // (com AC-3 + AAC o Chrome pula sozinho para a faixa AAC, por isso o fixture tem uma faixa só)
  await page.keyboard.press('Escape')
  await openVideo(page, 'remux_h264_ac3only.mkv')
  await expect.poll(async () => (await videoState(page)).mode).toBe('direct')
  await play(page)
  await expect.poll(async () => (await videoState(page)).mode, { timeout: 30000 }).toBe('remux')
  await waitPlayingPast(page, 3)
  expect((await videoState(page)).audioBytes).toBeGreaterThan(0)
})
