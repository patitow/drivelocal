// Sonda de sincronia injetada na página pelo e2e (tests/media-sync.spec.js).
// Mede, no relógio de performance.now():
//  - quando cada flash branco do fixture é exibido (requestVideoFrameCallback + leitura do quadro);
//  - quando cada bipe sai no áudio (AudioWorklet no grafo do <video>, convertido com getOutputTimestamp);
//  - quando a legenda de texto (overlay DOM) com "SYNC k" aparece.
// Também guarda o tempo de arquivo que o player atribui ao quadro do flash (data-time-base + mediaTime),
// que é o relógio usado pelas legendas (overlay, JASSUB e libpgs).
window.__syncProbe = (() => {
  const st = { flashes: [], beeps: [], cues: [], stamps: [], installed: false }
  const WORKLET = `
    class Onset extends AudioWorkletProcessor {
      constructor() { super(); this.quiet = 0; this.cnt = -1 }
      process(inputs, outputs) {
        const inp = inputs[0]
        const out = outputs[0]
        for (let c = 0; c < out.length; c++) if (inp[c]) out[c].set(inp[c])
        const ch = inp[0]
        if (!ch) return true
        for (let i = 0; i < ch.length; i++) {
          let a = 0
          for (let c = 0; c < inp.length; c++) a = Math.max(a, Math.abs(inp[c][i]))
          if (this.cnt >= 0) {
            // conta cruzamentos por zero nos 40 ms seguintes ao início do bipe => frequência
            const s = ch[i]
            if ((s >= 0) !== (this.prev >= 0)) this.zc++
            this.prev = s
            if (++this.cnt >= sampleRate * 0.04) {
              this.port.postMessage({ t: this.t0, freq: this.zc / 2 / 0.04 })
              this.cnt = -1
            }
          }
          if (a > 0.05) {
            if (this.quiet > sampleRate * 0.3 && this.cnt < 0) {
              this.t0 = (currentFrame + i) / sampleRate
              this.cnt = 0
              this.zc = 0
              this.prev = ch[i]
            }
            this.quiet = 0
          } else this.quiet++
        }
        return true
      }
    }
    registerProcessor('onset', Onset)`

  // base de tempo que o player usa (data-time-base): tempo do arquivo = base + video.currentTime
  function fileOffset(video) {
    return Number(video.dataset.timeBase || 0)
  }

  async function install(video) {
    if (st.installed) return
    st.installed = true
    const ctx = new AudioContext({ latencyHint: 'interactive' })
    st.ctx = ctx
    const url = URL.createObjectURL(new Blob([WORKLET], { type: 'application/javascript' }))
    await ctx.audioWorklet.addModule(url)
    const node = new AudioWorkletNode(ctx, 'onset', { outputChannelCount: [2] })
    node.port.onmessage = (e) => st.beeps.push({ ctxTime: e.data.t, id: (((Math.round((e.data.freq - 400) / 150)) % 7) + 7) % 7 })
    ctx.createMediaElementSource(video).connect(node).connect(ctx.destination)
    await ctx.resume()

    const canvas = document.createElement('canvas')
    canvas.width = 24
    canvas.height = 8
    const g = canvas.getContext('2d', { willReadFrequently: true })
    let on = false
    const onFrame = (now, md) => {
      g.drawImage(video, 0, 0, 24, 8)
      const d = g.getImageData(0, 0, 24, 8).data
      const px = (x, y) => d[(y * 24 + x) * 4]
      const bright = px(12, 2) > 128 // metade de cima: flash
      if (bright && !on) {
        // metade de baixo: número do segundo em binário (6 colunas)
        let sec = 0
        for (let b = 0; b < 6; b++) if (px(b * 4 + 2, 6) > 128) sec |= 1 << b
        st.flashes.push({ perf: md.expectedDisplayTime, mediaTime: md.mediaTime, offset: fileOffset(video), sec })
      }
      on = bright
      video.requestVideoFrameCallback(onFrame)
    }
    video.requestVideoFrameCallback(onFrame)

    const stamp = () => {
      const ts = ctx.getOutputTimestamp()
      if (ts.performanceTime > 0) st.stamps.push({ ctx: ts.contextTime, perf: ts.performanceTime })
      if (st.stamps.length > 4000) st.stamps.splice(0, 2000)
      requestAnimationFrame(stamp)
    }
    requestAnimationFrame(stamp)

    let lastCue = ''
    new MutationObserver(() => {
      const el = document.querySelector('[data-testid="subtitle-cue"]')
      const txt = el ? el.textContent : ''
      if (txt !== lastCue) {
        lastCue = txt
        const m = /SYNC (\d+)/.exec(txt)
        // a mudança aparece no próximo quadro pintado
        if (m) requestAnimationFrame((t) => st.cues.push({ k: Number(m[1]), perf: t }))
      }
    }).observe(document.body, { subtree: true, childList: true, characterData: true })
  }

  function toPerf(ctxTime) {
    // par (contextTime, performanceTime) mais próximo do bipe
    let best = null
    for (const s of st.stamps) if (!best || Math.abs(s.ctx - ctxTime) < Math.abs(best.ctx - ctxTime)) best = s
    if (!best) return null
    return best.perf + (ctxTime - best.ctx) * 1000
  }

  function reset() {
    st.flashes = []
    st.beeps = []
    st.cues = []
  }

  // pareia flash x bipe do mesmo segundo e flash x legenda; ms positivos = áudio/legenda depois da imagem
  function report() {
    const beeps = st.beeps.map((b) => ({ perf: toPerf(b.ctxTime), id: b.id })).filter((b) => b.perf !== null)
    const rows = []
    for (const f of st.flashes) {
      const fileTime = f.offset + f.mediaTime
      const k = f.sec // segundo realmente na tela (codificado no quadro)
      // bipe do MESMO segundo (frequência identifica k mod 7), o mais próximo do flash
      let av = null
      for (const b of beeps)
        if (b.id === k % 7 && Math.abs(b.perf - f.perf) < 3000 && (av === null || Math.abs(b.perf - f.perf) < Math.abs(av))) av = b.perf - f.perf
      const cue = st.cues.find((c) => c.k === k)
      rows.push({
        k,
        mediaTime: f.mediaTime,
        offset: f.offset,
        clockErrMs: (fileTime - k) * 1000, // tempo de arquivo atribuído pelo player - tempo real do quadro
        avMs: av, // bipe - flash
        cueMs: cue ? cue.perf - f.perf : null, // legenda DOM - flash
      })
    }
    return { rows, flashes: st.flashes.length, beeps: beeps.length, stamps: st.stamps.length, ctxState: st.ctx && st.ctx.state }
  }

  return { install, reset, report }
})()
