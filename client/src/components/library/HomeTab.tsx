// Início da Coleção: prateleiras horizontais vindas de /api/catalog/home e,
// no fim, o feed infinito "Para você".
import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { ChevronLeft, ChevronRight, ListPlus, ListVideo, Play, Tags, Video, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { buildStreamUrlSync, buildThumbnailUrlSync, cn } from '@/lib/utils'
import { library, formatDuration, type HomeShelf, type LibraryItem, type Playlist, type PlaylistCard, type TopicCard, type TopicMini } from '@/lib/library'
import { FavoriteButton, errorMessage, itemMeta, progressPct, useLoad, type OpenFn } from './catalogShared'
import { HomeFeed } from './HomeFeed'
import { PASTEL, Still, initial } from './shelves/shelfUi'

// Altura fixa do card; a largura segue o aspecto real da miniatura.
const CARD_H = 168
const MIN_W = 96
const MAX_W = 320

export function defaultAspect(it: { root?: string; path: string }): number {
  // Criadores é quase todo vertical (478x850); o resto, 16:9.
  return (it.root || it.path.split('/')[0]) === 'Criadores' ? 478 / 850 : 16 / 9
}

function useAspect(fallback: number) {
  const [aspect, setAspect] = useState(fallback)
  const onLoad = (e: React.SyntheticEvent<HTMLImageElement>) => {
    const { naturalWidth: w, naturalHeight: h } = e.currentTarget
    if (w > 0 && h > 0) setAspect(w / h)
  }
  const width = Math.round(Math.min(MAX_W, Math.max(MIN_W, CARD_H * aspect)))
  return { width, onLoad }
}

function Thumb({ path, onLoad, alt }: { path: string; onLoad: (e: React.SyntheticEvent<HTMLImageElement>) => void; alt: string }) {
  const [failed, setFailed] = useState(false)
  if (!path || failed) {
    return (
      <span className="flex h-full w-full items-center justify-center bg-muted">
        <Video className="h-8 w-8 text-muted-foreground" />
      </span>
    )
  }
  return (
    <img
      src={buildThumbnailUrlSync(path)}
      alt={alt}
      loading="lazy"
      decoding="async"
      draggable={false}
      onLoad={onLoad}
      onError={() => setFailed(true)}
      className="h-full w-full object-cover"
    />
  )
}

// Prévia muda ao parar o mouse em cima de um vídeo (só em ponteiro fino).
export function useHoverPreview(enabled: boolean) {
  const [show, setShow] = useState(false)
  const timer = useRef<number>()
  const fine = typeof window !== 'undefined' && window.matchMedia?.('(hover: hover) and (pointer: fine)').matches
  const start = () => {
    if (!enabled || !fine) return
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setShow(true), 700)
  }
  const stop = () => {
    window.clearTimeout(timer.current)
    setShow(false)
  }
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return { show, start, stop }
}

function PlaylistPicker({ item, onClose }: { item: LibraryItem; onClose: () => void }) {
  const [lists, setLists] = useState<Playlist[] | null>(null)
  useEffect(() => {
    library.playlists().then((l) => setLists(l.filter((p) => p.canEdit))).catch((e) => { toast.error(errorMessage(e)); onClose() })
  }, [onClose])
  const add = async (p: Playlist) => {
    try {
      await library.addToPlaylist(p.id, [item.id])
      toast.success(`Adicionado a "${p.name}"`)
    } catch (e) {
      toast.error(errorMessage(e))
    }
    onClose()
  }
  return (
    <div
      aria-label="Adicionar à playlist"
      className="absolute bottom-10 right-1 z-20 max-h-48 w-48 overflow-auto rounded-md border bg-popover p-1 text-sm shadow-lg"
    >
      {lists === null && <p className="p-2 text-muted-foreground">Carregando…</p>}
      {lists?.length === 0 && <p className="p-2 text-muted-foreground">Crie uma playlist na aba Playlists.</p>}
      {lists?.map((p) => (
        <button key={p.id} type="button" className="block w-full truncate rounded px-2 py-1 text-left hover:bg-accent" onClick={() => add(p)}>
          {p.name}
        </button>
      ))}
    </div>
  )
}

function ShelfItemCard({ item, onOpen, onDismiss }: { item: LibraryItem; onOpen: () => void; onDismiss?: () => void }) {
  const { width, onLoad } = useAspect(defaultAspect(item))
  const preview = useHoverPreview(item.kind === 'video')
  const [picking, setPicking] = useState(false)
  const pct = progressPct(item)
  const title = item.title || item.name
  const startAt = item.progress?.positionS || Math.min(60, (item.durationS ?? 0) * 0.2)
  return (
    <div
      className="group relative shrink-0 snap-start"
      style={{ width }}
      onMouseEnter={preview.start}
      onMouseLeave={() => { preview.stop(); setPicking(false) }}
      data-testid="shelf-card"
    >
      <button
        type="button"
        className="relative block w-full overflow-hidden rounded-xl bg-muted outline-none ring-offset-2 transition-transform duration-200 focus-visible:ring-2 focus-visible:ring-ring group-hover:scale-[1.03]"
        style={{ height: CARD_H }}
        onClick={onOpen}
        aria-label={`Abrir ${title}`}
      >
        <Thumb path={item.path} onLoad={onLoad} alt="" />
        {preview.show && (
          <video
            src={`${buildStreamUrlSync(item.path)}#t=${Math.floor(startAt)}`}
            muted
            autoPlay
            playsInline
            loop
            preload="metadata"
            className="absolute inset-0 h-full w-full object-cover"
          />
        )}
        {item.kind === 'video' && !preview.show && (
          <span className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity group-hover:opacity-100">
            <Play className="h-10 w-10 rounded-full bg-black/60 p-2 text-white" />
          </span>
        )}
        {item.durationS ? (
          <span className="absolute bottom-1.5 right-1.5 rounded bg-black/75 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-white">
            {formatDuration(item.durationS)}
          </span>
        ) : null}
        {pct > 0 && (
          <span className="absolute inset-x-0 bottom-0 h-[3px] bg-white/30">
            <span className="block h-full bg-[#FF5A5A]" style={{ width: `${pct}%` }} />
          </span>
        )}
      </button>
      <div className="absolute right-1 top-1 flex gap-1 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100">
        <FavoriteButton item={item} className="h-7 w-7 bg-black/60 text-white hover:bg-black/80 hover:text-white" />
        <Button
          variant="ghost"
          size="icon"
          aria-label="Adicionar à playlist"
          className="h-7 w-7 bg-black/60 text-white hover:bg-black/80 hover:text-white"
          onClick={(e) => { e.stopPropagation(); setPicking((v) => !v) }}
        >
          <ListPlus className="h-4 w-4" />
        </Button>
        {onDismiss && (
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Remover ${title} de continuar assistindo`}
            title="Remover de continuar assistindo"
            className="h-7 w-7 bg-black/60 text-white hover:bg-black/80 hover:text-white"
            onClick={(e) => { e.stopPropagation(); onDismiss() }}
          >
            <X className="h-4 w-4" />
          </Button>
        )}
      </div>
      {picking && <PlaylistPicker item={item} onClose={() => setPicking(false)} />}
      <p className="mt-2 line-clamp-2 text-sm font-medium leading-snug" title={item.path}>{title}</p>
      <p className="mt-0.5 truncate text-xs text-muted-foreground">{itemMeta(item)}</p>
    </div>
  )
}

function CoverCard({ cover, root, title, subtitle, icon: Icon, onClick }: {
  cover?: string
  root?: string
  title: string
  subtitle: string
  icon: typeof Tags
  onClick: () => void
}) {
  const { width, onLoad } = useAspect(cover ? defaultAspect({ root, path: cover }) : 16 / 9)
  return (
    <div className="shrink-0 snap-start" style={{ width }}>
      <button
        type="button"
        onClick={onClick}
        aria-label={`Abrir ${title}`}
        className="relative block w-full overflow-hidden rounded-xl bg-muted outline-none ring-offset-2 focus-visible:ring-2 focus-visible:ring-ring"
        style={{ height: CARD_H }}
      >
        {cover ? <Thumb path={cover} onLoad={onLoad} alt="" /> : (
          <span className="flex h-full w-full items-center justify-center"><Icon className="h-8 w-8 text-muted-foreground" /></span>
        )}
        <span className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/40 to-transparent px-3 pb-2.5 pt-8 text-left text-sm font-semibold text-white">
          {title}
        </span>
      </button>
      <p className="mt-2 truncate text-xs text-muted-foreground">{subtitle}</p>
    </div>
  )
}

// Série em destaque: pôster vertical com o progresso da temporada.
function SeriesPoster({ t, onClick }: { t: TopicCard; onClick: () => void }) {
  const pct = t.count > 0 ? Math.round((t.watched / t.count) * 100) : 0
  return (
    <div className="w-[132px] shrink-0 snap-start sm:w-[148px]">
      <button type="button" onClick={onClick} aria-label={`Abrir ${t.title}`}
        className="group relative block aspect-[2/3] w-full overflow-hidden rounded-xl bg-muted outline-none ring-offset-2 ring-offset-background focus-visible:ring-2 focus-visible:ring-ring">
        {t.coverPath ? <Still path={t.coverPath} className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.05]" />
          : <span className="flex h-full w-full items-center justify-center"><Tags className="h-8 w-8 text-muted-foreground" /></span>}
        <span className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 via-black/50 to-transparent px-2.5 pb-2.5 pt-10 text-left">
          <span className="line-clamp-3 text-sm font-semibold leading-tight text-white">{t.title}</span>
        </span>
        {pct > 0 && (
          <span className="absolute inset-x-0 bottom-0 h-[3px] bg-white/25"><span className="block h-full bg-[#7B8BFF]" style={{ width: `${pct}%` }} /></span>
        )}
      </button>
      <p className="mt-2 truncate text-xs text-muted-foreground">{t.watched ? `${t.watched} de ${t.count} vistos` : `${t.count} episódios`}</p>
    </div>
  )
}

// Criador em destaque: avatar redondo, como na estante de criadores.
function CreatorAvatar({ t, i, onClick }: { t: TopicCard; i: number; onClick: () => void }) {
  const pastel = PASTEL[i % PASTEL.length]
  return (
    <button type="button" onClick={onClick} aria-label={`Abrir ${t.title}`}
      className="group flex w-[112px] shrink-0 snap-start flex-col items-center gap-2 rounded-xl p-1 text-center outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <span className="relative flex h-[96px] w-[96px] items-center justify-center overflow-hidden rounded-full font-display text-3xl font-extrabold transition-transform duration-200 group-hover:scale-105"
        style={{ background: pastel, color: '#1A1B21', boxShadow: `0 0 0 3px hsl(var(--background)), 0 0 0 5px ${pastel}` }}>
        {initial(t.title)}
        {t.coverPath && <Still path={t.coverPath} className="absolute inset-0 h-full w-full object-cover" />}
      </span>
      <span className="w-full truncate text-sm font-semibold">{t.title}</span>
      <span className="-mt-1.5 text-xs text-muted-foreground">{t.count} {t.count === 1 ? 'item' : 'itens'}</span>
    </button>
  )
}

// Destaque no topo: o vídeo em andamento (ou o mais novo), com prévia tocando ao fundo.
function usePrefersReducedMotion() {
  return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}

function remaining(item: LibraryItem): string {
  const p = item.progress
  if (!p || p.completed || p.durationS <= 0) return ''
  const left = Math.max(0, p.durationS - p.positionS)
  const m = Math.round(left / 60)
  return m >= 60 ? `faltam ${Math.floor(m / 60)} h ${m % 60} min` : `faltam ${Math.max(1, m)} min`
}

function HeroBanner({ items, onOpen }: { items: LibraryItem[]; onOpen: OpenFn }) {
  const [idx, setIdx] = useState(0)
  const [hold, setHold] = useState(false)
  const reduced = usePrefersReducedMotion()
  useEffect(() => {
    if (reduced || hold || items.length < 2) return
    const t = window.setTimeout(() => setIdx((i) => (i + 1) % items.length), 16000)
    return () => window.clearTimeout(t)
  }, [idx, hold, items.length, reduced])
  const cur = Math.min(idx, items.length - 1)
  return <HeroSlide key={items[cur].id} items={items} idx={cur} setIdx={setIdx} setHold={setHold} reduced={reduced} onOpen={onOpen} />
}

function HeroSlide({ items, idx, setIdx, setHold, reduced, onOpen }: {
  items: LibraryItem[]; idx: number; setIdx: (i: number) => void; setHold: (v: boolean) => void; reduced: boolean; onOpen: OpenFn
}) {
  const [playing, setPlaying] = useState(false)
  const item = items[idx]
  const { width, onLoad } = useAspect(defaultAspect(item))
  const title = item.title || item.name
  const pct = progressPct(item)
  const startAt = item.progress?.positionS || Math.min(60, (item.durationS ?? 0) * 0.2)
  const left = remaining(item)

  // Prévia entra depois de um instante, para não competir com o carregamento da página.
  useEffect(() => {
    if (reduced || item.kind !== 'video') return
    const t = window.setTimeout(() => setPlaying(true), 1400)
    return () => window.clearTimeout(t)
  }, [item.kind, reduced])

  const meta = [item.episode != null ? `Episódio ${item.episode}` : '', formatDuration(item.durationS), left].filter(Boolean)
  return (
    <section
      aria-label="Destaque"
      data-testid="home-hero"
      className="relative isolate overflow-hidden rounded-3xl border bg-card"
      onMouseEnter={() => setHold(true)}
      onMouseLeave={() => setHold(false)}
    >
      <div className="absolute inset-0 -z-10 animate-in fade-in duration-700">
        <img src={buildThumbnailUrlSync(item.path)} alt="" aria-hidden draggable={false} className="h-full w-full scale-125 object-cover opacity-60 blur-3xl" />
        <div className="absolute inset-0 bg-gradient-to-r from-background via-background/70 to-background/10" />
        <div className="absolute inset-0 bg-gradient-to-t from-background/80 via-transparent to-transparent" />
      </div>
      <div className="flex min-h-[260px] items-stretch gap-6 p-5 sm:min-h-[320px] sm:p-8">
        <div className="flex min-w-0 flex-1 flex-col justify-end gap-3 sm:max-w-[48%] sm:flex-none">
          <p className="text-sm font-medium text-muted-foreground">
            {item.progress && !item.progress.completed ? 'Continue de onde parou' : 'Para assistir agora'}
          </p>
          <h1 className="line-clamp-2 font-display text-3xl font-extrabold leading-none tracking-tighter sm:text-5xl">{title}</h1>
          {meta.length > 0 && <p className="text-sm text-muted-foreground">{meta.join(' · ')}</p>}
          {pct > 0 && (
            <div className="h-1 w-full max-w-sm overflow-hidden rounded-full bg-foreground/15" role="progressbar" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
              <div className="h-full rounded-full bg-[#FF5A5A]" style={{ width: `${pct}%` }} />
            </div>
          )}
          <div className="mt-1 flex items-center gap-2">
            <Button size="lg" className="h-12 gap-2 rounded-full bg-foreground px-7 text-base font-semibold text-background hover:bg-foreground/90" onClick={() => onOpen(items, items.indexOf(item))}>
              <Play className="h-5 w-5 fill-current" />
              {pct > 0 ? 'Continuar' : 'Assistir'}
            </Button>
            <FavoriteButton item={item} className="h-11 w-11 rounded-full bg-foreground/10 hover:bg-foreground/20" />
          </div>
          {items.length > 1 && (
            <div className="mt-2 flex items-center gap-1.5" role="tablist" aria-label="Destaques">
              {items.map((it, i) => (
                <button
                  key={it.id}
                  type="button"
                  role="tab"
                  aria-selected={i === idx}
                  aria-label={`Destaque ${i + 1}`}
                  onClick={() => setIdx(i)}
                  className={cn('h-1.5 rounded-full transition-all', i === idx ? 'w-6 bg-foreground' : 'w-1.5 bg-foreground/30 hover:bg-foreground/60')}
                />
              ))}
            </div>
          )}
        </div>
        <button
          type="button"
          aria-label={`Abrir ${title}`}
          onClick={() => onOpen(items, items.indexOf(item))}
          className="relative ml-auto hidden self-center overflow-hidden rounded-xl bg-black shadow-2xl ring-1 ring-white/10 outline-none transition-transform duration-300 hover:scale-[1.02] focus-visible:ring-2 focus-visible:ring-ring sm:block"
          style={{ height: 'clamp(200px, 26vw, 280px)', aspectRatio: String(width / CARD_H), maxWidth: '48%' }}
        >
          <img src={buildThumbnailUrlSync(item.path)} alt="" draggable={false} onLoad={onLoad} className="h-full w-full object-cover" />
          {playing && (
            <video
              src={`${buildStreamUrlSync(item.path)}#t=${Math.floor(startAt)}`}
              muted
              autoPlay
              loop
              playsInline
              preload="metadata"
              className="absolute inset-0 h-full w-full animate-in fade-in object-cover duration-700"
            />
          )}
        </button>
      </div>
    </section>
  )
}

function ShelfRow({ title, children, action }: { title: string; children: React.ReactNode; action?: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [edge, setEdge] = useState({ start: true, end: false })
  const update = () => {
    const el = ref.current
    if (el) setEdge({ start: el.scrollLeft <= 2, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 2 })
  }
  useEffect(() => {
    update()
    const el = ref.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [children])
  const scroll = (dir: number) => {
    const el = ref.current
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.85, behavior: 'smooth' })
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      e.preventDefault()
      scroll(e.key === 'ArrowRight' ? 1 : -1)
    }
  }
  const arrow = 'absolute z-10 hidden h-10 w-10 items-center justify-center rounded-full border border-white/10 bg-background/80 shadow-lg backdrop-blur transition-opacity hover:bg-background sm:flex'
  const fade = (on: boolean) => (on ? '#000' : 'transparent')
  const mask = `linear-gradient(to right, ${fade(edge.start)}, #000 32px, #000 calc(100% - 32px), ${fade(edge.end)})`
  return (
    <section aria-label={title} className="group/shelf">
      <div className="mb-3 flex items-center gap-2">
        <h2 className="font-display text-[22px] font-bold tracking-tight">{title}</h2>
        <div className="ml-auto flex items-center gap-1">{action}</div>
      </div>
      <div className="relative -mx-1">
        <button type="button" aria-label={`Rolar ${title} para a esquerda`} tabIndex={-1} onClick={() => scroll(-1)}
          style={{ top: CARD_H / 2 - 12 }}
          className={cn(arrow, 'left-2', edge.start ? 'pointer-events-none opacity-0' : 'opacity-0 group-hover/shelf:opacity-100 focus-visible:opacity-100')}>
          <ChevronLeft className="h-5 w-5" />
        </button>
        <button type="button" aria-label={`Rolar ${title} para a direita`} tabIndex={-1} onClick={() => scroll(1)}
          style={{ top: CARD_H / 2 - 12 }}
          className={cn(arrow, 'right-2', edge.end ? 'pointer-events-none opacity-0' : 'opacity-0 group-hover/shelf:opacity-100 focus-visible:opacity-100')}>
          <ChevronRight className="h-5 w-5" />
        </button>
        {/* Rolagem por teclado: setas no trilho (focável) ou num card dele. */}
        {/* eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex, jsx-a11y/no-static-element-interactions */}
        <div ref={ref} tabIndex={0} onKeyDown={onKey} onScroll={update}
          aria-label={`${title}: use as setas para rolar`}
          className="shelf-rail flex snap-x gap-4 overflow-x-auto scroll-smooth px-1 pb-2 pt-1 outline-none focus-visible:ring-2 focus-visible:ring-ring"
          style={{ WebkitMaskImage: mask, maskImage: mask }}
        >
          {children}
        </div>
      </div>
    </section>
  )
}

function SkeletonShelves() {
  return (
    <div className="space-y-8" aria-label="Carregando" aria-busy="true">
      <div className="h-[260px] animate-pulse rounded-2xl bg-muted sm:h-[320px]" />
      {[0, 1, 2].map((r) => (
        <div key={r}>
          <div className="mb-2 h-5 w-48 animate-pulse rounded bg-muted" />
          <div className="flex gap-3 overflow-hidden">
            {[0, 1, 2, 3, 4, 5].map((c) => (
              <div key={c} className="shrink-0">
                <div className="animate-pulse rounded-md bg-muted" style={{ height: CARD_H, width: c % 3 === 1 ? 95 : 299 }} />
                <div className="mt-2 h-3 w-24 animate-pulse rounded bg-muted" />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

export function HomeTab({ onOpen, onOpenTopic, onOpenPlaylist }: {
  onOpen: OpenFn
  onOpenTopic: (t: TopicMini) => void
  onOpenPlaylist: (p: PlaylistCard) => void
}) {
  const home = useLoad(['home'], () => library.home())

  const dropFromContinue = (ids: number[] | 'all') => {
    home.setData((d) => {
      if (!d) return d
      const shelves = d.shelves
        .map((s) => (s.id !== 'continue' ? s : { ...s, items: ids === 'all' ? [] : (s.items ?? []).filter((it) => !ids.includes(it.id)) }))
        .filter((s) => s.id !== 'continue' || (s.items?.length ?? 0) > 0)
      return { ...d, shelves }
    })
  }
  const dismiss = async (it: LibraryItem) => {
    try {
      await library.dismissContinue(it.id)
      dropFromContinue([it.id])
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }
  const clearAll = async () => {
    if (!window.confirm('Limpar toda a lista "Continuar assistindo"? O progresso dos vídeos é mantido.')) return
    try {
      await library.clearContinue()
      dropFromContinue('all')
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }

  if (home.loading && !home.data) return <SkeletonShelves />
  if (home.error) return <p className="text-sm text-destructive">{home.error === 'Request failed with status code 503' ? 'Biblioteca indisponível no servidor.' : home.error}</p>
  const shelves = home.data?.shelves ?? []

  const renderShelf = (s: HomeShelf) => {
    if (s.kind === 'topics') {
      return (
        <ShelfRow key={s.id} title={s.title}>
          {(s.topics ?? []).map((t, i) => (t.kind === 'creator'
            ? <CreatorAvatar key={t.id} t={t} i={i} onClick={() => onOpenTopic(t)} />
            : t.kind === 'series'
              ? <SeriesPoster key={t.id} t={t} onClick={() => onOpenTopic(t)} />
              : <CoverCard key={t.id} cover={t.coverPath} title={t.title} icon={Tags}
                subtitle={`${t.count} itens${t.watched ? ` · ${t.watched} vistos` : ''}`} onClick={() => onOpenTopic(t)} />
          ))}
        </ShelfRow>
      )
    }
    if (s.kind === 'playlists') {
      return (
        <ShelfRow key={s.id} title={s.title}>
          {(s.playlists ?? []).map((p) => (
            <CoverCard key={p.id} cover={p.coverPath} title={p.name} icon={ListVideo}
              subtitle={`${p.itemCount} itens${p.ownerName ? ` · ${p.ownerName}` : ''}`} onClick={() => onOpenPlaylist(p)} />
          ))}
        </ShelfRow>
      )
    }
    const items = s.items ?? []
    const canDismiss = s.actions?.includes('dismiss')
    return (
      <ShelfRow
        key={s.id}
        title={s.title}
        action={canDismiss ? <Button variant="ghost" size="sm" onClick={clearAll}>Limpar tudo</Button> : undefined}
      >
        {items.map((it, i) => (
          <ShelfItemCard key={it.id} item={it} onOpen={() => onOpen(items, i)} onDismiss={canDismiss ? () => dismiss(it) : undefined} />
        ))}
      </ShelfRow>
    )
  }

  const heroSrc = shelves.find((x) => x.id === 'continue') ?? shelves.find((x) => x.id === 'recent') ?? shelves.find((x) => x.kind === 'items' && (x.items?.length ?? 0) > 0)
  const heroItems = (heroSrc?.items ?? []).filter((it) => it.kind === 'video').slice(0, 5)
  return (
    <div className={cn('space-y-8', home.loading && 'opacity-70')}>
      {heroItems.length > 0 && <HeroBanner items={heroItems} onOpen={onOpen} />}
      {shelves.map(renderShelf)}
      <HomeFeed onOpen={onOpen} onOpenTopic={onOpenTopic} />
    </div>
  )
}
