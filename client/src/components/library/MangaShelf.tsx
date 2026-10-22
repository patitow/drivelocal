// Estante de Mangás: página de papel (como no design aprovado), obra e leitor.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, ArrowRight, BookOpen, Check, ChevronLeft, CloudOff, Columns2, Download, Loader2, Rows3, X, ZoomIn, ZoomOut } from 'lucide-react'
import { buildStreamUrlSync, buildThumbnailUrlSync, cn } from '@/lib/utils'
import { library, type LibraryItem, type MangaChapter, type MangaWork } from '@/lib/library'
import { MetaBlurb, WebImg, useArt, useExtMeta } from './ExtMeta'
import { EditEntryButton } from './EditEntryButton'
import { Status, useLoad } from './catalogShared'
import { useNavParam } from '@/lib/nav'
import {
  downloadChapter, downloadedChapters, isNetworkError, offlinePages, offlinePageUrl, offlineSupported, offlineWorks, rememberWorks, removeChapter,
  useProgress, type ReadState,
} from './mangaStore'

const INK = '#181715'
const PAPER = '#F4F0E6'
const SHEET = '#FFFDF7'
const PANEL = '#E8E2D2'
const RULE = '#D3CCB8'
const MUTED = '#4A4740'

const MODE_KEY = 'drivelocal:manga:mode'
const SPREAD_KEY = 'drivelocal:manga:spread'

function loadSpread() {
  try {
    return localStorage.getItem(SPREAD_KEY) === '1'
  } catch {
    return false
  }
}

type Mode = 'scroll' | 'ltr' | 'rtl'
function loadMode(): Mode {
  try {
    const m = localStorage.getItem(MODE_KEY)
    return m === 'ltr' || m === 'rtl' ? m : 'scroll'
  } catch {
    return 'scroll'
  }
}

const natural = (a: string, b: string) => a.localeCompare(b, 'pt-BR', { numeric: true, sensitivity: 'base' })

// ---------- peças de papel ----------

// web: capa da ficha do cadastro, que vence a primeira página.
function Cover({ path, web, alt, className }: { path: string; web?: string; alt: string; className?: string }) {
  const [failed, setFailed] = useState(false)
  const [webFailed, setWebFailed] = useState(false)
  if (web && !webFailed) return <WebImg src={web} alt={alt} onFail={() => setWebFailed(true)} className={cn('object-cover', className)} />
  if (failed) {
    return (
      <span className={cn('flex items-center justify-center', className)}
        style={{ backgroundImage: `radial-gradient(${INK} 1.4px, transparent 1.9px)`, backgroundSize: '8px 8px', opacity: 0.55 }} />
    )
  }
  return <img src={buildThumbnailUrlSync(path)} alt={alt} loading="lazy" decoding="async" draggable={false} onError={() => setFailed(true)} className={cn('object-cover', className)} />
}

function PaperChip({ on, children, onClick }: { on: boolean; children: React.ReactNode; onClick: () => void }) {
  return (
    <button type="button" role="radio" aria-checked={on} onClick={onClick}
      className="h-[38px] rounded-[10px] border-2 px-4 text-sm transition-colors"
      style={on ? { borderColor: INK, background: INK, color: PAPER } : { borderColor: RULE, color: INK }}>
      {children}
    </button>
  )
}

function WorkCard({ w, state, onOpen }: { w: MangaWork; state?: ReadState; onOpen: () => void }) {
  const caps = w.chapters.length
  const read = state?.done.length ?? 0
  const art = useArt(`manga:${w.dir}`)
  return (
    <button type="button" onClick={onOpen} aria-label={`Abrir ${w.title}`} data-testid="manga-card"
      className="group flex min-w-0 flex-col gap-2.5 text-left outline-none">
      <span className="relative block aspect-[2/3]">
        <span className="absolute inset-0 translate-x-2 -translate-y-1.5 rounded border transition-transform duration-200 group-hover:translate-x-3 group-hover:-translate-y-2.5"
          style={{ background: RULE, borderColor: INK }} />
        <span className="absolute inset-0 overflow-hidden rounded border-2 group-focus-visible:ring-4 group-focus-visible:ring-[#181715]/30"
          style={{ background: SHEET, borderColor: INK }}>
          <Cover path={w.cover} web={art?.cover} alt="" className="h-full w-full" />
          <span className="absolute bottom-2 right-2 px-2 py-0.5 text-xs font-semibold" style={{ background: INK, color: PAPER }}>
            {caps} {isVolumes(w) ? (caps === 1 ? 'vol.' : 'vols.') : caps === 1 ? 'cap.' : 'caps.'}
          </span>
          {read > 0 && (
            <span className="absolute left-2 top-2 flex items-center gap-1 px-2 py-0.5 text-xs font-semibold" style={{ background: SHEET, color: INK, border: `1.5px solid ${INK}` }}>
              {read >= caps ? <><Check className="h-3 w-3" />Lido</> : `${read}/${caps} lidos`}
            </span>
          )}
        </span>
      </span>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="line-clamp-2 text-sm font-semibold leading-snug" title={w.dir}>{w.title}</span>
        <span className="truncate text-[13px]" style={{ color: MUTED }}>{w.author || `${w.pages} páginas`}</span>
      </span>
    </button>
  )
}

function ContinueCard({ w, s, onOpen }: { w: MangaWork; s: ReadState; onOpen: () => void }) {
  const ch = w.chapters.find((c) => c.dir === s.chapter)
  const pct = s.pages > 0 ? Math.round(((s.page + 1) / s.pages) * 100) : 0
  return (
    <button type="button" onClick={onOpen} aria-label={`Continuar ${w.title}`}
      className="flex min-w-0 gap-4 rounded-[14px] border-2 p-3.5 text-left transition-transform hover:-translate-x-0.5 hover:-translate-y-0.5"
      style={{ background: SHEET, borderColor: INK, boxShadow: `4px 4px 0 ${INK}` }}>
      <span className="relative block aspect-[2/3] w-[76px] shrink-0 overflow-hidden rounded border" style={{ background: PAPER, borderColor: INK }}>
        <Cover path={ch?.cover || w.cover} alt="" className="h-full w-full" />
      </span>
      <span className="flex min-w-0 flex-1 flex-col justify-center gap-1.5">
        <span className="truncate text-base font-bold">{w.title}</span>
        <span className="truncate text-[13px]" style={{ color: MUTED }}>{ch?.title ?? 'Capítulo'}</span>
        <span className="h-1.5 rounded-full" style={{ background: PANEL }}>
          <span className="block h-full rounded-full" style={{ background: INK, width: `${pct}%` }} />
        </span>
        <span className="text-xs" style={{ color: '#7A766B' }}>página {s.page + 1} de {s.pages}</span>
      </span>
    </button>
  )
}

// ---------- obra ----------

// Obras juntadas por volume (o servidor nomeia cada parte "Volume N").
const isVolumes = (w: MangaWork) => w.chapters.every((c) => c.title.startsWith('Volume '))
const unit = (w: MangaWork, n: number) => (isVolumes(w) ? (n === 1 ? 'volume' : 'volumes') : n === 1 ? 'capítulo' : 'capítulos')

function WorkPage({ w, state, downloaded, onBack, onRead }: {
  w: MangaWork
  state?: ReadState
  downloaded: Set<string>
  onBack: () => void
  onRead: (chapter: number, page: number) => void
}) {
  const meta = useExtMeta('manga', w.title, `manga:${w.dir}`)
  const art = useArt(`manga:${w.dir}`)
  const curIdx = state ? Math.max(0, w.chapters.findIndex((c) => c.dir === state.chapter)) : 0
  const done = new Set(state?.done ?? [])
  const top = useRef<HTMLDivElement>(null)
  useEffect(() => { top.current?.scrollIntoView?.({ block: 'start' }) }, [w.dir])
  return (
    <div ref={top} className="scroll-mt-10 space-y-8">
      <button type="button" onClick={onBack} className="flex items-center gap-1 text-sm font-medium" style={{ color: MUTED }}>
        <ChevronLeft className="h-4 w-4" />Mangás
      </button>
      <header className="flex flex-col gap-7 border-b-4 pb-8 sm:flex-row sm:items-end" style={{ borderColor: INK }}>
        <span className="relative block aspect-[2/3] w-40 shrink-0 sm:w-52">
          <span className="absolute inset-0 translate-x-2.5 -translate-y-2 rounded border" style={{ background: RULE, borderColor: INK }} />
          <span className="absolute inset-0 overflow-hidden rounded border-2" style={{ borderColor: INK, background: SHEET }}>
            <Cover path={w.cover} web={art?.cover} alt={`Capa de ${w.title}`} className="h-full w-full" />
          </span>
        </span>
        <div className="min-w-0 flex-1 space-y-3">
          <h2 className="break-words font-display text-4xl font-extrabold leading-[0.95] tracking-tighter sm:text-6xl">{w.title}</h2>
          <p className="text-base" style={{ color: MUTED }}>
            {[w.author, meta?.year ? String(meta.year) : '', `${w.chapters.length} ${unit(w, w.chapters.length)}`, `${w.pages} páginas`].filter(Boolean).join(' · ')}
          </p>
          {meta && <MetaBlurb meta={meta} tone="paper" />}
          <div className="flex flex-wrap gap-2.5 pt-2">
            <button type="button" onClick={() => onRead(curIdx, state ? state.page : 0)}
              className="flex h-12 items-center gap-2 rounded-full px-7 text-base font-semibold" style={{ background: INK, color: PAPER }}>
              <BookOpen className="h-5 w-5" />
              {state ? `Continuar · ${w.chapters[curIdx]?.title}, pág. ${state.page + 1}` : 'Ler do início'}
            </button>
            {state && (
              <button type="button" onClick={() => onRead(0, 0)} className="h-12 rounded-full border-2 px-6 text-base font-medium" style={{ borderColor: INK }}>
                Recomeçar
              </button>
            )}
            <EditEntryButton entryRef={`manga:${w.dir}`} className="border-2 hover:bg-transparent" style={{ borderColor: INK }} />
          </div>
        </div>
      </header>
      <section aria-label="Capítulos" className="space-y-3.5">
        <h3 className="font-display text-[22px] font-bold tracking-tight">{isVolumes(w) ? 'Volumes' : 'Capítulos'}</h3>
        <ol className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {w.chapters.map((c, i) => {
            const current = state?.chapter === c.dir
            const read = done.has(c.dir)
            return (
              <li key={c.dir}>
                <button type="button" onClick={() => onRead(i, current && state ? state.page : 0)}
                  className="flex w-full min-w-0 items-center gap-3.5 rounded-xl border-2 p-2.5 text-left transition-colors hover:bg-[#FFFDF7]"
                  style={{ borderColor: current ? INK : RULE, background: current ? SHEET : undefined }}>
                  <span className="relative block aspect-[2/3] w-12 shrink-0 overflow-hidden rounded-sm border" style={{ borderColor: INK }}>
                    <Cover path={c.cover} alt="" className="h-full w-full" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[15px] font-semibold">{c.title}</span>
                    <span className="text-[13px]" style={{ color: MUTED }}>
                      {c.pages} páginas{current && state ? ` · parou na ${state.page + 1}` : ''}
                    </span>
                  </span>
                  {downloaded.has(c.dir) && <CloudOff className="h-4 w-4 shrink-0" style={{ color: MUTED }} aria-label="Baixado para ler offline" />}
                  {read && <Check className="h-5 w-5 shrink-0" aria-label="Lido" />}
                </button>
              </li>
            )
          })}
        </ol>
      </section>
    </div>
  )
}

// ---------- leitor ----------

function useWide() {
  const q = '(min-width: 900px)'
  const [wide, setWide] = useState(() => typeof window !== 'undefined' && !!window.matchMedia?.(q).matches)
  useEffect(() => {
    const m = window.matchMedia?.(q)
    if (!m) return
    const on = () => setWide(m.matches)
    m.addEventListener?.('change', on)
    return () => m.removeEventListener?.('change', on)
  }, [])
  return wide
}

const ZOOM_MIN = 1
const ZOOM_MAX = 4
const clampZoom = (z: number) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(z * 100) / 100))
// Página dupla: a capa fica sozinha, depois pares (1–2, 3–4…).
const spreadStart = (p: number) => (p <= 0 ? 0 : p - ((p - 1) % 2))

function Reader({ w, chapter, startPage, downloaded, onDownloaded, onClose, onChapter, onProgress }: {
  w: MangaWork
  chapter: number
  startPage: number
  downloaded: boolean
  onDownloaded: () => void
  onClose: () => void
  onChapter: (i: number) => void
  onProgress: (page: number, pages: number, finished: boolean) => void
}) {
  const ch: MangaChapter = w.chapters[chapter]
  const [mode, setMode] = useState<Mode>(loadMode)
  const [spread, setSpread] = useState(loadSpread)
  const [page, setPage] = useState(startPage)
  const [chrome, setChrome] = useState(true)
  const [zoom, setZoom] = useState(1)
  const [dl, setDl] = useState<{ done: number; error?: string } | null>(null)
  const [local, setLocal] = useState<Record<string, string>>({})
  const wide = useWide()
  const res = useLoad(['manga-pages', ch.dir], async () => {
    try {
      return [...(await library.mangaPages(ch.dir))].sort((a, b) => natural(a.path, b.path))
    } catch (e) {
      const off = isNetworkError(e) ? await offlinePages(ch.dir) : null
      if (off) return off
      throw e
    }
  })
  const pages: LibraryItem[] = useMemo(() => res.data ?? [], [res.data])
  const scroller = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLDivElement>(null)
  const hasNext = chapter < w.chapters.length - 1
  const doubled = spread && wide && mode !== 'scroll'
  const last = Math.max(0, pages.length - 1)
  const shown = doubled ? spreadStart(Math.min(page, last)) : Math.min(page, last)
  // última página na tela (na dupla, a da direita conta para "lido")
  const shownLast = doubled && shown > 0 ? Math.min(shown + 1, last) : shown
  const src = (p: LibraryItem) => local[p.path] ?? buildStreamUrlSync(p.path)

  const pickMode = (m: Mode) => {
    setMode(m)
    setZoom(1)
    try { localStorage.setItem(MODE_KEY, m) } catch { /* sem armazenamento */ }
  }
  const toggleSpread = () => {
    setSpread((v) => {
      try { localStorage.setItem(SPREAD_KEY, v ? '0' : '1') } catch { /* sem armazenamento */ }
      return !v
    })
  }

  // Capítulo baixado: páginas saem do cache do navegador (funciona sem rede).
  useEffect(() => {
    if (!downloaded || !pages.length) return
    let alive = true
    const urls: string[] = []
    Promise.all(pages.map(async (p) => [p.path, await offlinePageUrl(p.path)] as const)).then((pairs) => {
      const m: Record<string, string> = {}
      for (const [path, url] of pairs) {
        if (url) { m[path] = url; urls.push(url) }
      }
      if (alive) setLocal(m)
      else urls.forEach((u) => URL.revokeObjectURL(u))
    })
    return () => {
      alive = false
      urls.forEach((u) => URL.revokeObjectURL(u))
    }
  }, [downloaded, pages])

  const download = async () => {
    if (!pages.length || dl) return
    setDl({ done: 0 })
    try {
      await downloadChapter(ch.dir, pages, (done) => setDl({ done }))
      setDl(null)
      onDownloaded()
    } catch (e) {
      setDl({ done: 0, error: e instanceof Error ? e.message : 'falhou' })
      window.setTimeout(() => setDl(null), 4000)
    }
  }
  const forget = async () => {
    await removeChapter(ch.dir)
    setLocal({})
    onDownloaded()
  }

  // Salva a página atual (e marca o capítulo como lido na última).
  useEffect(() => {
    if (pages.length) onProgress(shownLast, pages.length, shownLast >= pages.length - 1)
  }, [shownLast, pages.length, onProgress])

  // Rolagem: volta para a página salva e acompanha qual está na tela.
  useEffect(() => {
    if (mode !== 'scroll' || !pages.length || !scroller.current) return
    const el = scroller.current
    const target = el.querySelector<HTMLElement>(`[data-page="${page}"]`)
    target?.scrollIntoView?.({ block: 'start' })
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (e.isIntersecting) setPage(Number((e.target as HTMLElement).dataset.page))
      }
    }, { root: el, rootMargin: '-45% 0px -45% 0px' })
    el.querySelectorAll('[data-page]').forEach((p) => io.observe(p))
    return () => io.disconnect()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, pages.length])

  const step = useCallback((dir: 1 | -1) => {
    setZoom(1)
    setPage((p) => {
      let n: number
      if (doubled) {
        const s = spreadStart(p)
        n = dir > 0 ? (s === 0 ? 1 : s + 2) : s <= 1 ? (s === 0 ? -1 : 0) : s - 2
      } else {
        n = p + dir
      }
      if (n < 0) return p
      if (n >= pages.length) {
        if (hasNext) onChapter(chapter + 1)
        return p
      }
      return n
    })
  }, [pages.length, hasNext, onChapter, chapter, doubled])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      if (e.key === '+' || e.key === '=') setZoom((z) => clampZoom(z + 0.25))
      if (e.key === '-') setZoom((z) => clampZoom(z - 0.25))
      if (e.key === '0') setZoom(1)
      if (mode === 'scroll') return
      const fwd = mode === 'rtl' ? 'ArrowLeft' : 'ArrowRight'
      const back = mode === 'rtl' ? 'ArrowRight' : 'ArrowLeft'
      if (e.key === fwd || e.key === ' ') { e.preventDefault(); step(1) }
      if (e.key === back) { e.preventDefault(); step(-1) }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [mode, step, onClose])

  // Ctrl + roda do mouse (e pinça no trackpad) amplia a página, não o site.
  useEffect(() => {
    const el = root.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey) return
      e.preventDefault()
      setZoom((z) => clampZoom(z * Math.exp(-e.deltaY / 300)))
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])

  // Pinça com dois dedos.
  const pinch = useRef<{ d: number; z: number } | null>(null)
  const dist = (t: React.TouchList) => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY)
  const touch = {
    onTouchStart: (e: React.TouchEvent) => { if (e.touches.length === 2) pinch.current = { d: dist(e.touches), z: zoom } },
    onTouchMove: (e: React.TouchEvent) => {
      if (e.touches.length === 2 && pinch.current) setZoom(clampZoom(pinch.current.z * (dist(e.touches) / pinch.current.d)))
    },
    onTouchEnd: (e: React.TouchEvent) => { if (e.touches.length < 2) pinch.current = null },
  }

  const visible = doubled && shown > 0
    ? (mode === 'rtl' ? [pages[shown + 1], pages[shown]] : [pages[shown], pages[shown + 1]]).filter(Boolean)
    : pages[shown] ? [pages[shown]] : []
  const modeBtn = (m: Mode, label: string, Icon: typeof Rows3) => (
    <button type="button" role="radio" aria-checked={mode === m} onClick={() => pickMode(m)} title={label}
      className={cn('flex h-9 items-center gap-1.5 rounded-lg px-3 text-sm', mode === m ? 'bg-[#F4F0E6] font-semibold text-[#181715]' : 'text-white/70 hover:text-white')}>
      <Icon className="h-4 w-4" /><span className="hidden lg:inline">{label}</span>
    </button>
  )
  const iconBtn = 'flex h-9 min-w-9 shrink-0 items-center justify-center gap-1.5 rounded-lg px-2 text-sm text-white/80 hover:bg-white/10 hover:text-white disabled:opacity-40'

  return (
    <div ref={root} role="dialog" aria-modal="true" aria-label={`Lendo ${w.title}`} className="fixed inset-0 z-50 flex flex-col bg-[#101010] text-white">
      <div className={cn('absolute inset-x-0 top-0 z-10 flex flex-wrap items-center gap-x-3 gap-y-2 bg-gradient-to-b from-black/90 to-black/0 px-3 pb-6 pt-3 transition-opacity sm:px-5', chrome ? 'opacity-100' : 'pointer-events-none opacity-0')}>
        <button type="button" onClick={onClose} aria-label="Fechar leitor" className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-white/10 hover:bg-white/20">
          <X className="h-5 w-5" />
        </button>
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold">{w.title}</p>
          <select value={chapter} onChange={(e) => onChapter(Number(e.target.value))} aria-label="Capítulo"
            className="max-w-full truncate bg-transparent text-xs text-white/70 outline-none">
            {w.chapters.map((c, i) => <option key={c.dir} value={i} className="text-black">{c.title}</option>)}
          </select>
        </div>
        <div className="flex items-center gap-1 rounded-xl bg-white/10 p-1">
          <button type="button" className={iconBtn} aria-label="Diminuir zoom" disabled={zoom <= ZOOM_MIN} onClick={() => setZoom((z) => clampZoom(z - 0.25))}>
            <ZoomOut className="h-4 w-4" />
          </button>
          <button type="button" className={cn(iconBtn, 'w-12 tabular-nums')} aria-label="Zoom original" title="Zoom original (0)" onClick={() => setZoom(1)}>
            {Math.round(zoom * 100)}%
          </button>
          <button type="button" className={iconBtn} aria-label="Aumentar zoom" disabled={zoom >= ZOOM_MAX} onClick={() => setZoom((z) => clampZoom(z + 0.25))}>
            <ZoomIn className="h-4 w-4" />
          </button>
          {wide && mode !== 'scroll' && (
            <button type="button" className={cn(iconBtn, spread && 'bg-[#F4F0E6] text-[#181715] hover:bg-[#F4F0E6] hover:text-[#181715]')}
              aria-pressed={spread} title="Página dupla" onClick={toggleSpread}>
              <BookOpen className="h-4 w-4" /><span className="hidden xl:inline">Dupla</span>
            </button>
          )}
          {offlineSupported() && (
            downloaded ? (
              <button type="button" className={iconBtn} title="Baixado: lê sem conexão. Clique para apagar do aparelho." aria-label="Apagar capítulo baixado" onClick={() => void forget()}>
                <CloudOff className="h-4 w-4" /><span className="hidden xl:inline">Offline</span>
              </button>
            ) : (
              <button type="button" className={iconBtn} disabled={!pages.length || !!dl} onClick={() => void download()}
                title={dl?.error ?? 'Baixar capítulo para ler sem conexão'} aria-label="Baixar capítulo">
                {dl && !dl.error ? <Loader2 className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                <span className="hidden tabular-nums xl:inline">{dl ? (dl.error ? 'Falhou' : `${dl.done}/${pages.length}`) : 'Baixar'}</span>
              </button>
            )
          )}
        </div>
        <div role="radiogroup" aria-label="Modo de leitura" className="flex rounded-xl bg-white/10 p-1">
          {modeBtn('scroll', 'Rolagem', Rows3)}
          {modeBtn('ltr', 'Páginas', Columns2)}
          {modeBtn('rtl', 'Direita p/ esquerda', ArrowLeft)}
        </div>
      </div>

      {res.loading || res.error ? (
        <div className="m-auto"><Status loading={res.loading} error={res.error} /></div>
      ) : mode === 'scroll' ? (
        <div ref={scroller} className="min-h-0 flex-1 overflow-auto" style={{ touchAction: 'pan-x pan-y' }} onClick={() => setChrome((v) => !v)} {...touch}>
          <div className="mx-auto flex flex-col pt-16" style={{ maxWidth: 900 * zoom, width: zoom > 1 ? `${zoom * 100}%` : undefined }}>
            {pages.map((p, i) => (
              <img key={p.id} data-page={i} src={src(p)} alt={`Página ${i + 1}`} loading={Math.abs(i - startPage) < 3 ? 'eager' : 'lazy'}
                decoding="async" draggable={false} className="block min-h-[40vh] w-full bg-[#1a1a1a]" />
            ))}
            <ChapterEnd hasNext={hasNext} next={w.chapters[chapter + 1]} onNext={() => onChapter(chapter + 1)} onClose={onClose} />
          </div>
        </div>
      ) : (
        <div className={cn('relative min-h-0 flex-1 select-none', zoom > 1 && 'overflow-auto')} style={{ touchAction: 'pan-x pan-y' }} {...touch}
          onDoubleClick={() => setZoom((z) => (z > 1 ? 1 : 2))}>
          <div className="flex items-center justify-center" style={{ width: `${zoom * 100}%`, height: `${zoom * 100}%`, minWidth: '100%', minHeight: '100%' }}>
            {visible.map((p) => (
              <img key={p.id} src={src(p)} alt={`Página ${pages.indexOf(p) + 1}`} draggable={false}
                className={cn('h-full object-contain', visible.length > 1 ? 'w-1/2' : 'w-full', visible.length > 1 && (p === visible[0] ? 'object-right' : 'object-left'))} />
            ))}
          </div>
          {/* Pré-carrega as próximas. */}
          {!downloaded && [shownLast + 1, shownLast + 2].map((i) => pages[i] && <link key={i} rel="prefetch" href={buildStreamUrlSync(pages[i].path)} />)}
          {zoom === 1 && (
            <>
              <button type="button" aria-label={mode === 'rtl' ? 'Próxima página' : 'Página anterior'} onClick={() => step(mode === 'rtl' ? 1 : -1)} className="absolute inset-y-0 left-0 w-1/3 cursor-w-resize" />
              <button type="button" aria-label="Mostrar controles" onClick={() => setChrome((v) => !v)} className="absolute inset-y-0 left-1/3 w-1/3" />
              <button type="button" aria-label={mode === 'rtl' ? 'Página anterior' : 'Próxima página'} onClick={() => step(mode === 'rtl' ? -1 : 1)} className="absolute inset-y-0 right-0 w-1/3 cursor-e-resize" />
            </>
          )}
        </div>
      )}

      {pages.length > 0 && (
        <div className={cn('absolute inset-x-0 bottom-0 flex items-center gap-3 bg-gradient-to-t from-black/90 to-black/0 px-4 pb-4 pt-8 transition-opacity sm:px-6', chrome ? 'opacity-100' : 'pointer-events-none opacity-0')}>
          {mode !== 'scroll' && (
            <button type="button" aria-label="Voltar uma página" onClick={() => step(mode === 'rtl' ? 1 : -1)} className="flex h-9 w-9 items-center justify-center rounded-full bg-white/10 hover:bg-white/20">
              <ArrowLeft className="h-4 w-4" />
            </button>
          )}
          <input type="range" min={0} max={last} value={shown} aria-label="Página"
            onChange={(e) => {
              const n = Number(e.target.value)
              setPage(n)
              setZoom(1)
              if (mode === 'scroll') scroller.current?.querySelector<HTMLElement>(`[data-page="${n}"]`)?.scrollIntoView({ block: 'start' })
            }}
            className="min-w-0 flex-1 accent-[#F4F0E6]" style={mode === 'rtl' ? { direction: 'rtl' } : undefined} />
          {mode !== 'scroll' && (
            <button type="button" aria-label="Avançar uma página" onClick={() => step(mode === 'rtl' ? -1 : 1)} className="flex h-9 w-9 items-center justify-center rounded-full bg-white/10 hover:bg-white/20">
              <ArrowRight className="h-4 w-4" />
            </button>
          )}
          <span className="shrink-0 text-sm tabular-nums text-white/80">
            {shownLast > shown ? `${shown + 1}–${shownLast + 1}` : shown + 1} / {pages.length}
          </span>
        </div>
      )}
    </div>
  )
}

function ChapterEnd({ hasNext, next, onNext, onClose }: { hasNext: boolean; next?: MangaChapter; onNext: () => void; onClose: () => void }) {
  return (
    <div className="flex flex-col items-center gap-4 px-6 py-16 text-center">
      <p className="font-display text-2xl font-bold">{hasNext ? 'Fim do capítulo' : 'Fim da obra'}</p>
      {hasNext && next ? (
        <button type="button" onClick={(e) => { e.stopPropagation(); onNext() }}
          className="flex h-12 items-center gap-2 rounded-full bg-[#F4F0E6] px-7 font-semibold text-[#181715]">
          {next.title}<ArrowRight className="h-4 w-4" />
        </button>
      ) : (
        <button type="button" onClick={(e) => { e.stopPropagation(); onClose() }} className="h-12 rounded-full border border-white/30 px-7">Voltar para a obra</button>
      )}
    </div>
  )
}

// ---------- estante ----------

type Filter = 'all' | 'reading' | 'unread' | 'done'
type Sort = 'recent' | 'title' | 'size'

export function MangaShelf({ onCount }: { onCount?: (n: number) => void }) {
  const res = useLoad(['mangas'], () => library.mangas().then(rememberWorks).catch(async (e) => {
    const off = isNetworkError(e) ? await offlineWorks<MangaWork[]>() : null
    if (off) return off
    throw e
  }))
  const { progress, update } = useProgress()
  const [downloaded, setDownloaded] = useState<Set<string>>(new Set())
  const refreshDownloaded = useCallback(() => { downloadedChapters().then(setDownloaded).catch(() => {}) }, [])
  useEffect(refreshDownloaded, [refreshDownloaded])
  const [filter, setFilter] = useState<Filter>('all')
  const [sort, setSort] = useState<Sort>('recent')
  // obra e capítulo abertos ficam na URL (?obra=&cap=): o voltar fecha o leitor
  const [obra, setObra] = useNavParam('obra')
  const [cap, setCap] = useNavParam('cap')
  const [startPage, setStartPage] = useState(0)
  const works = useMemo(() => res.data ?? [], [res.data])
  const sel = obra ? works.find((w) => w.dir === obra) ?? null : null
  const reading = sel && cap != null && sel.chapters[cap] ? { chapter: cap, page: startPage } : null
  const setSel = (w: MangaWork | null) => setObra(w?.dir, { also: { cap: undefined } })
  const setReading = (r: { chapter: number; page: number } | null, replace = false) => {
    if (r) setStartPage(r.page)
    setCap(r?.chapter, { replace })
  }

  useEffect(() => { if (res.data) onCount?.(res.data.length) }, [res.data, onCount])

  const status = (w: MangaWork): Filter => {
    const s = progress[w.dir]
    if (!s) return 'unread'
    return s.done.length >= w.chapters.length ? 'done' : 'reading'
  }
  const shown = works
    .filter((w) => filter === 'all' || status(w) === filter)
    .sort((a, b) => (sort === 'title' ? natural(a.title, b.title) : sort === 'size' ? b.pages - a.pages : 0))
  const inProgress = works
    .filter((w) => status(w) === 'reading')
    .sort((a, b) => progress[b.dir].at - progress[a.dir].at)
    .slice(0, 6)
  const chapters = works.reduce((n, w) => n + w.chapters.length, 0)

  const onProgress = useCallback((page: number, pages: number, finished: boolean) => {
    if (!sel || !reading) return
    update(sel.dir, { chapter: sel.chapters[reading.chapter].dir, page, pages }, finished)
  }, [sel, reading, update])

  const read = (w: MangaWork, chapter: number, page: number) => { setStartPage(page); setObra(w.dir, { also: { cap: chapter } }) }
  const resume = (w: MangaWork) => {
    const s = progress[w.dir]
    read(w, Math.max(0, w.chapters.findIndex((c) => c.dir === s.chapter)), s.page)
  }

  return (
    // Papel de ponta a ponta: anula o respiro do contêiner escuro da Coleção.
    <div className="-mx-4 -mb-4 min-h-full px-4 pb-16 pt-6 lg:-mx-10 lg:-my-9 lg:px-10 lg:py-10" style={{ background: PAPER, color: INK }} data-testid="manga-shelf">
      {sel && !reading ? (
        <WorkPage w={sel} state={progress[sel.dir]} downloaded={downloaded} onBack={() => setSel(null)} onRead={(c, p) => setReading({ chapter: c, page: p })} />
      ) : (
        <div className="space-y-7">
          <header className="flex flex-wrap items-end gap-5 border-b-4 pb-5" style={{ borderColor: INK }}>
            <div className="space-y-2">
              <h2 className="font-display text-5xl font-extrabold leading-[0.95] tracking-tighter sm:text-7xl">Mangás</h2>
              <p className="text-base" style={{ color: MUTED }}>
                {res.data
                  ? [`${works.length} ${works.length === 1 ? 'obra' : 'obras'}`, `${chapters} ${chapters === 1 ? 'capítulo' : 'capítulos'}`, inProgress.length ? `${inProgress.length} em leitura` : ''].filter(Boolean).join(' · ')
                  : ' '}
              </p>
            </div>
            <div className="flex-1" />
            <div role="radiogroup" aria-label="Ordenar" className="flex rounded-xl p-1" style={{ background: PANEL }}>
              {([['recent', 'Recentes'], ['title', 'A–Z'], ['size', 'Mais páginas']] as const).map(([id, label]) => (
                <button key={id} type="button" role="radio" aria-checked={sort === id} onClick={() => setSort(id)}
                  className="h-9 rounded-[9px] px-[18px] text-sm"
                  style={sort === id ? { background: INK, color: PAPER, fontWeight: 600 } : { color: MUTED }}>
                  {label}
                </button>
              ))}
            </div>
          </header>

          <div role="radiogroup" aria-label="Filtro" className="flex flex-wrap gap-2.5">
            {([['all', 'Todas'], ['reading', 'Em leitura'], ['unread', 'Não lidas'], ['done', 'Concluídas']] as const).map(([id, label]) => (
              <PaperChip key={id} on={filter === id} onClick={() => setFilter(id)}>{label}</PaperChip>
            ))}
          </div>

          <div className="[&_p]:!text-[#4A4740]"><Status loading={res.loading} error={res.error} /></div>

          {filter === 'all' && inProgress.length > 0 && (
            <section aria-label="Continuar lendo" className="space-y-3.5">
              <h3 className="font-display text-[22px] font-bold tracking-tight">Continuar lendo</h3>
              <div className="grid grid-cols-[repeat(auto-fill,minmax(300px,1fr))] gap-4 pb-1 pr-1">
                {inProgress.map((w) => <ContinueCard key={w.dir} w={w} s={progress[w.dir]} onOpen={() => resume(w)} />)}
              </div>
            </section>
          )}

          {res.data && (
            <section aria-label="Obras" className="space-y-3.5">
              <h3 className="font-display text-[22px] font-bold tracking-tight">{filter === 'all' ? 'Todas as obras' : 'Obras'}</h3>
              {shown.length === 0 && (
                <p className="text-sm" style={{ color: MUTED }}>
                  {works.length === 0 ? 'Nenhum mangá encontrado. Coloque as obras numa pasta chamada "Mangás", uma subpasta por obra.' : 'Nenhuma obra com esse filtro.'}
                </p>
              )}
              <div className="grid grid-cols-2 gap-x-5 gap-y-7 pr-2 sm:grid-cols-[repeat(auto-fill,minmax(168px,1fr))]">
                {shown.map((w) => <WorkCard key={w.dir} w={w} state={progress[w.dir]} onOpen={() => setSel(w)} />)}
              </div>
            </section>
          )}
        </div>
      )}

      {sel && reading && (
        <Reader
          key={`${sel.dir}:${reading.chapter}`}
          w={sel}
          chapter={reading.chapter}
          startPage={reading.page}
          downloaded={downloaded.has(sel.chapters[reading.chapter].dir)}
          onDownloaded={refreshDownloaded}
          onClose={() => setReading(null)}
          onChapter={(i) => setReading({ chapter: i, page: 0 }, true)}
          onProgress={onProgress}
        />
      )}
    </div>
  )
}
