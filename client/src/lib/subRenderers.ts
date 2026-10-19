// Renderizadores de legenda que desenham num canvas sobre o <video>:
// ASS/SSA via JASSUB (libass em wasm) e PGS (Blu-ray) via libpgs.
// Os pacotes são carregados sob demanda para não pesar no bundle inicial.
// O canvas é inserido logo após o <video>, dentro do container que vai para tela cheia.

export interface SubRenderer {
  setOffset(offset: number): void
  destroy(): void
}

export async function createAssRenderer(video: HTMLVideoElement, subUrl: string, fontUrls: string[], offset: number): Promise<SubRenderer> {
  const [{ default: JASSUB }, workerUrl, wasmUrl, modernWasmUrl, defaultFontUrl] = await Promise.all([
    import('jassub'),
    import('jassub/dist/worker/worker.js?worker&url').then((m) => m.default as string),
    import('jassub/dist/wasm/jassub-worker.wasm?url').then((m) => m.default as string),
    import('jassub/dist/wasm/jassub-worker-modern.wasm?url').then((m) => m.default as string),
    import('jassub/dist/default.woff2?url').then((m) => m.default as string),
  ])
  const abs = (u: string) => new URL(u, window.location.href).href
  const instance = new JASSUB({
    video,
    subUrl,
    workerUrl: abs(workerUrl),
    wasmUrl: abs(wasmUrl),
    modernWasmUrl: abs(modernWasmUrl),
    // fontes anexadas ao mkv; o que faltar cai na fonte padrão (Liberation Sans)
    fonts: fontUrls,
    availableFonts: { 'liberation sans': abs(defaultFontUrl) },
    defaultFont: 'liberation sans',
    queryFonts: false,
    timeOffset: offset,
  })
  instance._canvas.dataset.testid = 'ass-canvas'
  return {
    setOffset: (o) => { instance.timeOffset = o },
    destroy: () => { void instance.destroy() },
  }
}

export async function createPgsRenderer(video: HTMLVideoElement, subUrl: string, offset: number): Promise<SubRenderer> {
  const [{ PgsRenderer }, workerUrl] = await Promise.all([
    import('libpgs'),
    import('libpgs/dist/libpgs.worker.js?url').then((m) => m.default as string),
  ])
  const before = new Set(video.parentElement?.querySelectorAll('canvas') ?? [])
  const renderer = new PgsRenderer({
    workerUrl: new URL(workerUrl, window.location.href).href,
    video,
    subUrl,
    timeOffset: offset,
    aspectRatio: 'contain',
  })
  // marca o canvas criado pela lib (para o e2e)
  video.parentElement?.querySelectorAll('canvas').forEach((c) => {
    if (!before.has(c)) c.dataset.testid = 'pgs-canvas'
  })
  await renderer.ready
  // libpgs 0.9 começa com o índice 0 como "já desenhado": se a 1ª legenda do arquivo estiver
  // na tela ao ativar, ela não aparece. Passa por um tempo sem legenda e volta para forçar o desenho.
  renderer.timeOffset = -1e9
  renderer.timeOffset = offset
  return {
    setOffset: (o) => { renderer.timeOffset = o },
    destroy: () => renderer.dispose(),
  }
}
