// Sincronia áudio x vídeo x legenda medida no Chrome real (fixtures sync_* de server-go/media_fixtures_test.go:
// flash branco + bipe começando em cada segundo inteiro + legenda "SYNC k" de k a k+0,5 s).
// Roda junto com o e2e de mídia: npx playwright test -c playwright.media.config.js
//
// Medidas (ms, ver tests/sync-probe.js):
//  - clock: tempo de arquivo que o player atribui ao quadro do flash - tempo real do flash. É o relógio das
//    legendas (overlay, JASSUB, libpgs) e da barra de progresso.
//  - av: bipe - flash, na saída. Tem um viés fixo da própria medição (latência de saída do WebAudio vs.
//    compositor, ~50-70 ms no Chrome headless), por isso é comparado com o direct do mesmo arquivo.
//  - cue: legenda DOM na tela - flash.
const { test, expect } = require('@playwright/test')
const path = require('path')

const PROBE = path.join(__dirname, 'sync-probe.js')
const TOL = 40

async function openVideo(page, name) {
  await page.goto('/drive/E2E')
  await page.getByText(name, { exact: true }).first().click()
  await page.waitForSelector('video[src]', { timeout: 15000 })
  await page.evaluate(() => window.__syncProbe.install(document.querySelector('video')))
}

async function play(page) {
  await expect(page.getByText('Carregando vídeo...')).toBeHidden({ timeout: 60000 })
  await page.locator('video').hover()
  await page.getByRole('button', { name: 'Reproduzir' }).click()
}

// tempo do arquivo segundo o próprio player (data-time-base + currentTime)
function fileTime(page) {
  return page.evaluate(() => {
    const v = document.querySelector('video')
    return Number(v.dataset.timeBase || 0) + v.currentTime
  })
}

async function waitPast(page, t, timeout = 60000) {
  await expect.poll(() => fileTime(page), { timeout, intervals: [200] }).toBeGreaterThan(t)
}

async function pickMenu(page, button, item) {
  await page.locator('video').hover()
  await page.getByRole('button', { name: button, exact: true }).click()
  await page.getByRole('menuitemradio', { name: item }).click()
}

async function keys(page, key, n) {
  await page.evaluate(() => document.activeElement && document.activeElement.blur())
  for (let i = 0; i < n; i++) await page.keyboard.press(key)
}

const stat = (a) => {
  if (!a.length) return null
  const s = a.slice().sort((x, y) => x - y)
  return { n: a.length, min: Math.round(s[0]), max: Math.round(s[s.length - 1]), med: Math.round(s[s.length >> 1]) }
}

// Espera o stream atual estar de fato tocando e anda mais 1 s. Numa troca de stream o player já usa a
// base nova enquanto o <video> antigo ainda toca, e o primeiro quadro do stream novo é pintado antes do
// seek que pula a folga inicial: nada disso é o que o usuário assiste, então não entra na medida.
async function settle(page) {
  await expect
    .poll(() => page.evaluate(() => {
      const v = document.querySelector('video')
      return v.readyState >= 3 && !v.seeking && !v.paused && v.currentTime > 0.3
    }), { timeout: 60000, intervals: [100] })
    .toBe(true)
  await waitPast(page, (await fileTime(page)) + 1)
}

// coleta ~`secs` segundos de medições a partir de agora
async function measure(page, label, secs = 4.5) {
  await settle(page)
  await page.evaluate(() => window.__syncProbe.reset())
  const t0 = await fileTime(page)
  await waitPast(page, t0 + secs)
  const r = await page.evaluate(() => window.__syncProbe.report())
  const pick = (k) => r.rows.map((x) => x[k]).filter((v) => v !== null && v !== undefined)
  const out = { label, from: Math.round(t0 * 10) / 10, clock: stat(pick('clockErrMs')), av: stat(pick('avMs')), cue: stat(pick('cueMs')) }
  if (process.env.SYNC_DEBUG) console.log(JSON.stringify(r.rows.slice(0, 3)))
  console.log(`[sync] ${label.padEnd(34)} clock=${JSON.stringify(out.clock)} av=${JSON.stringify(out.av)} cue=${JSON.stringify(out.cue)}`)
  return out
}

// Todas as amostras dentro de want ± TOL
function within(s, want, what) {
  expect(s, `${what}: sem amostras`).not.toBeNull()
  expect(s.n, `${what}: poucas amostras`).toBeGreaterThanOrEqual(3)
  expect(s.min, `${what} min=${s.min} (quero ${want}±${TOL})`).toBeGreaterThanOrEqual(want - TOL)
  expect(s.max, `${what} max=${s.max} (quero ${want}±${TOL})`).toBeLessThanOrEqual(want + TOL)
}

// m: medição; base: av do direct (viés da medição); audio/sub: ajuste manual esperado (ms)
function assertSync(m, base, { audio = 0, sub = 0 } = {}) {
  within(m.clock, 0, `${m.label} relógio`)
  within(m.av, base + audio, `${m.label} áudio-vídeo`)
  within(m.cue, sub, `${m.label} legenda`)
}

// viés da medição a/v no direct (arquivo cru tocado pelo próprio Chrome, sem nada do servidor)
async function directBaseline(page) {
  await openVideo(page, 'sync_h264_aac.mkv')
  await expect(page.locator('video')).toHaveAttribute('data-playback-mode', 'direct')
  await play(page)
  await waitPast(page, 0.5)
  await pickMenu(page, 'Legendas', /arquivo/)
  const m = await measure(page, 'h264 direct início')
  within(m.clock, 0, 'direct relógio')
  within(m.cue, 0, 'direct legenda')
  expect(m.av.max - m.av.min, 'direct a/v instável').toBeLessThanOrEqual(2 * TOL)
  return m
}

test.describe('sincronia', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript({ path: PROBE })
    // sem ajuste manual salvo de outro teste (a não ser que o teste peça para manter)
    await page.addInitScript(() => {
      if (sessionStorage.getItem('keep-sync')) return
      for (const k of Object.keys(localStorage)) if (k.startsWith('drivelocal:sync:')) localStorage.removeItem(k)
    })
  })

  test('direct e remux (troca de áudio, seek) ficam em sincronia', async ({ page }) => {
    test.setTimeout(300000)
    const base = (await directBaseline(page)).av.med
    await keys(page, 'ArrowRight', 2)
    await waitPast(page, 14)
    assertSync(await measure(page, 'h264 direct após seek'), base)

    // trocar a faixa de áudio força remux a partir do keyframe anterior
    await pickMenu(page, 'Faixa de áudio', /Inglês/)
    await expect.poll(() => page.evaluate(() => document.querySelector('video').src)).toContain('audio=1')
    await waitPast(page, 17)
    assertSync(await measure(page, 'h264 remux (troca de áudio)'), base)
    await keys(page, 'ArrowRight', 2)
    await waitPast(page, 28)
    assertSync(await measure(page, 'h264 remux após seek'), base)
    await page.keyboard.press('Escape')

    // como o filme: HEVC 10-bit + E-AC-3 5.1 => remux desde o início
    await openVideo(page, 'sync_hevc10_eac3.mkv')
    await expect(page.locator('video')).toHaveAttribute('data-playback-mode', 'remux')
    await play(page)
    await waitPast(page, 0.5)
    await pickMenu(page, 'Legendas', /SYNC SRT/)
    assertSync(await measure(page, 'hevc10 remux início'), base)
    await keys(page, 'ArrowRight', 2)
    await waitPast(page, 14)
    assertSync(await measure(page, 'hevc10 remux após seek'), base)
    await keys(page, 'ArrowRight', 3)
    await waitPast(page, 30)
    assertSync(await measure(page, 'hevc10 remux após 2º seek'), base)
  })

  test('transcode fica em sincronia', async ({ page }) => {
    test.setTimeout(300000)
    const base = (await directBaseline(page)).av.med
    await page.keyboard.press('Escape')
    // navegador "sem nada": força o transcode
    await page.route('**/api/media-info/**', (route) => {
      const url = new URL(route.request().url())
      url.searchParams.set('caps', 'none')
      return route.continue({ url: url.toString() })
    })
    await openVideo(page, 'sync_hevc10_eac3.mkv')
    await expect(page.locator('video')).toHaveAttribute('data-playback-mode', 'transcode')
    await play(page)
    await waitPast(page, 0.5)
    await pickMenu(page, 'Legendas', /SYNC SRT/)
    assertSync(await measure(page, 'hevc10 transcode início'), base)
    await keys(page, 'ArrowRight', 2)
    await waitPast(page, 14)
    assertSync(await measure(page, 'hevc10 transcode após seek'), base)
  })

  test('ajuste manual: J/K (áudio) e G/H (legenda), salvo por arquivo', async ({ page }) => {
    test.setTimeout(300000)
    const base = (await directBaseline(page)).av.med
    const video = page.locator('video')

    // K x4 = áudio +200 ms (som depois da imagem): o servidor gera o stream com ?adelay=200
    await keys(page, 'k', 4)
    await expect(page.getByTestId('sync-osd')).toHaveText(/Áudio: \+200 ms/)
    await expect.poll(() => video.evaluate((v) => v.src)).toContain('adelay=200')
    await waitPast(page, (await fileTime(page)) + 1)
    assertSync(await measure(page, 'áudio +200 ms'), base, { audio: 200 })

    // J x7 = áudio −150 ms (som antes da imagem, como para fone Bluetooth)
    await keys(page, 'j', 7)
    await expect.poll(() => video.evaluate((v) => v.src)).toContain('adelay=-150')
    await waitPast(page, (await fileTime(page)) + 1)
    assertSync(await measure(page, 'áudio −150 ms'), base, { audio: -150 })

    // H x6 = legenda +300 ms (aparece depois); não recarrega o stream
    const src = await video.evaluate((v) => v.src)
    await keys(page, 'h', 6)
    await expect(page.getByTestId('sync-osd')).toHaveText(/Legenda: \+300 ms/)
    assertSync(await measure(page, 'legenda +300 ms (áudio −150)'), base, { audio: -150, sub: 300 })
    expect(await video.evaluate((v) => v.src)).toBe(src)

    // G x10 = legenda −200 ms (aparece antes)
    await keys(page, 'g', 10)
    assertSync(await measure(page, 'legenda −200 ms (áudio −150)'), base, { audio: -150, sub: -200 })

    // salvo por arquivo: reabrir mostra os ajustes nos menus e já pede o stream com adelay
    await page.evaluate(() => sessionStorage.setItem('keep-sync', '1'))
    await page.keyboard.press('Escape')
    await openVideo(page, 'sync_h264_aac.mkv')
    await expect.poll(() => video.evaluate((v) => v.src)).toContain('adelay=-150')
    await expect(page.getByText('Carregando vídeo...')).toBeHidden({ timeout: 60000 })
    await video.hover()
    await page.getByRole('button', { name: 'Faixa de áudio', exact: true }).click()
    const audioRow = page.getByRole('group', { name: 'Atraso do áudio' })
    await expect(audioRow).toContainText('−150 ms')
    // Redefinir volta ao arquivo cru (direct)
    await audioRow.getByRole('button', { name: 'Redefinir' }).click()
    await expect(audioRow).toContainText('0 ms')
    await expect(video).toHaveAttribute('data-playback-mode', 'direct', { timeout: 15000 })
    await expect(page.getByText('Carregando vídeo...')).toBeHidden({ timeout: 60000 })
    await video.hover()
    // a troca de stream pode ter fechado o menu; se não, fecha pelo mesmo botão
    if (await audioRow.isVisible()) await page.getByRole('button', { name: 'Faixa de áudio', exact: true }).click()
    await page.getByRole('button', { name: 'Legendas', exact: true }).click()
    const subRow = page.getByRole('group', { name: 'Atraso da legenda' })
    await expect(subRow).toContainText('−200 ms')
    await subRow.getByRole('button', { name: 'Redefinir' }).click()
    await expect(subRow).toContainText('0 ms')
    expect(await page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('drivelocal:sync:')))).toEqual([])
  })
})
