// Varre a biblioteca inteira de um servidor DriveLocal e confere que cada vídeo toca no Chrome:
// pede /api/media-info com as caps do navegador, abre o stream no modo que o servidor escolheu e,
// como o player, sobe direct → remux → transcode se não houver imagem, se der erro ou se o tempo
// não andar. Nada de caminho fixo aqui: a lista vem do servidor; o relatório fica fora do repo.
//
// Uso: node tests/library-playback.sweep.js [pasta-inicial]
//   DL_URL (padrão http://127.0.0.1:8080), DL_USER/DL_PASS (admin/admin),
//   SWEEP_OUT (relatório JSON, padrão no tmp), SWEEP_CONCURRENCY (padrão 4), SWEEP_LIMIT (0 = todos)
// Sai com código 1 se algum vídeo não tocar em nenhum modo.
const { chromium } = require('@playwright/test')
const fs = require('fs')
const os = require('os')
const path = require('path')

const BASE = process.env.DL_URL || 'http://127.0.0.1:8080'
const OUT = process.env.SWEEP_OUT || path.join(os.tmpdir(), 'drivelocal-playback-sweep.json')
const CONCURRENCY = Number(process.env.SWEEP_CONCURRENCY || 4)
const LIMIT = Number(process.env.SWEEP_LIMIT || 0)
const VIDEO_EXT = /\.(mp4|avi|mov|wmv|flv|webm|mkv|m4v|m2ts|mts|mpg|mpeg)$/i
const MODES = ['direct', 'remux', 'transcode']

const enc = (p) => p.split('/').map(encodeURIComponent).join('/')

// o token de acesso expira no meio de uma varredura longa: `auth.token` é renovado num 401
const auth = { token: '' }
async function login() {
  const r = await fetch(`${BASE}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: process.env.DL_USER || 'admin', password: process.env.DL_PASS || 'admin' }),
  })
  if (!r.ok) throw new Error(`login falhou: ${r.status}`)
  auth.token = (await r.json()).token
}

// o servidor limita /api/* a 600 req/min por IP: num 429, espera o Retry-After e tenta de novo
async function api(url) {
  for (let attempt = 0; ; attempt++) {
    const r = await fetch(BASE + url, { headers: { Authorization: `Bearer ${auth.token}` } })
    if (r.status === 401 && attempt === 0) {
      await login()
      continue
    }
    if (r.status === 429 && attempt < 5) {
      await new Promise((ok) => setTimeout(ok, Number(r.headers.get('retry-after') || 60) * 1000))
      continue
    }
    if (!r.ok) throw new Error(`${r.status} ${url}`)
    return r.json()
  }
}

// todos os vídeos abaixo de `root`, paginando /api/files; pasta que não abre entra em `broken`
async function listVideos(root, broken) {
  const out = []
  const dirs = [root]
  while (dirs.length) {
    const dir = dirs.pop()
    for (let offset = 0; ; ) {
      let page
      try {
        page = await api(`/api/files?${new URLSearchParams({ path: dir, limit: 1000, offset })}`)
      } catch (e) {
        broken.push({ file: dir, ok: false, why: `pasta não abre: ${e.message}` })
        break
      }
      for (const it of page.items || []) {
        if (it.isDirectory) dirs.push(it.path)
        else if (VIDEO_EXT.test(it.name)) out.push(it.path)
      }
      offset += (page.items || []).length
      if (!page.pagination?.hasMore || !page.items?.length) break
    }
  }
  return out.sort()
}

// roda dentro da página: abre `src` num <video> mudo e diz se há imagem e se o tempo anda
function probeInPage({ src, needVideo }) {
  return new Promise((resolve) => {
    const v = document.createElement('video')
    v.muted = true
    v.preload = 'auto'
    let done = false
    const finish = (ok, why) => {
      if (done) return
      done = true
      const r = { ok, why, w: v.videoWidth, t: v.currentTime }
      v.removeAttribute('src')
      v.load()
      resolve(r)
    }
    const timer = setTimeout(() => finish(false, `timeout rs=${v.readyState}`), 45000)
    v.onerror = () => { clearTimeout(timer); finish(false, `erro ${v.error?.code} ${v.error?.message || ''}`.trim()) }
    v.onloadeddata = async () => {
      if (needVideo && v.videoWidth === 0) { clearTimeout(timer); return finish(false, 'sem imagem') }
      const t0 = v.currentTime
      try { await v.play() } catch (e) { clearTimeout(timer); return finish(false, `play: ${e.message}`) }
      const until = Date.now() + 8000
      while (Date.now() < until && !done) {
        await new Promise((r) => setTimeout(r, 200))
        if (v.currentTime - t0 > 0.4 || v.ended) { clearTimeout(timer); return finish(true, '') }
      }
      clearTimeout(timer)
      finish(false, `tempo parado em ${v.currentTime.toFixed(2)}`)
    }
    v.src = src
  })
}

async function checkOne(page, caps, file) {
  const mi = await api(`/api/media-info/${enc(file)}?caps=${encodeURIComponent(caps)}`)
  const needVideo = (mi.videoIndex ?? 0) >= 0 && !!mi.videoCodec
  const first = MODES.indexOf(mi.playback?.mode || 'direct')
  const tries = []
  for (const mode of MODES.slice(first)) {
    const src = `${BASE}/api/stream/${enc(file)}?token=${encodeURIComponent(auth.token)}${mode === 'direct' ? '' : `&mode=${mode}`}`
    const r = await page.evaluate(probeInPage, { src, needVideo })
    tries.push({ mode, ...r })
    if (r.ok) return { file, ok: true, mode, chosen: MODES[first], audio: mi.audio?.length ?? 0, tries }
  }
  return { file, ok: false, chosen: MODES[first], video: mi.videoCodec, audio: mi.audio?.length ?? 0, tries }
}

async function main() {
  const root = process.argv[2] || ''
  await login()

  const results = []
  let files = await listVideos(root, results)
  if (LIMIT > 0) files = files.slice(0, LIMIT)
  console.log(`${files.length} vídeos`)
  for (const r of results) console.log(`FALHOU: ${r.file} ${r.why}`)

  // Chrome de verdade: o Chromium do Playwright não tem H.264/AAC
  const browser = await chromium.launch({ channel: 'chrome', headless: true, args: ['--autoplay-policy=no-user-gesture-required'] })
  const ctx = await browser.newContext()
  let next = 0
  const worker = async () => {
    const page = await ctx.newPage()
    await page.goto(`${BASE}/api/health`)
    const caps = await page.evaluate(() => {
      const v = document.createElement('video')
      const probes = [['h264hi10', 'video/mp4; codecs="avc1.6E0028"'], ['hevc', 'video/mp4; codecs="hvc1.1.6.L120.90"'],
        ['hevc10', 'video/mp4; codecs="hvc1.2.4.L120.90"'], ['av1', 'video/mp4; codecs="av01.0.08M.08"'],
        ['vp9', 'video/webm; codecs="vp9"'], ['mkv', 'video/x-matroska']]
      return probes.filter(([, t]) => v.canPlayType(t) !== '').map(([n]) => n).join(',') || 'none'
    })
    while (next < files.length) {
      const file = files[next++]
      let r
      try { r = await checkOne(page, caps, file) } catch (e) { r = { file, ok: false, why: String(e.message || e) } }
      results.push(r)
      const tag = r.ok ? (r.mode === r.chosen ? 'ok' : `ok via ${r.mode}`) : 'FALHOU'
      if (!r.ok || r.mode !== r.chosen) console.log(`[${results.length}/${files.length}] ${tag}: ${file} ${JSON.stringify(r.tries || r.why)}`)
      else if (results.length % 100 === 0) console.log(`[${results.length}/${files.length}]`)
    }
    await page.close()
  }
  await Promise.all(Array.from({ length: CONCURRENCY }, worker))
  await browser.close()

  const failed = results.filter((r) => !r.ok)
  const fellBack = results.filter((r) => r.ok && r.mode !== r.chosen)
  fs.writeFileSync(OUT, JSON.stringify({ total: results.length, failed: failed.length, fellBack: fellBack.length, results }, null, 1))
  console.log(`\n${results.length} vídeos: ${results.length - failed.length} tocam (${fellBack.length} só depois de subir de modo), ${failed.length} falharam`)
  console.log(`relatório: ${OUT}`)
  process.exit(failed.length ? 1 : 0)
}

main().catch((e) => { console.error(e); process.exit(2) })
