// "Para você": feed infinito de recomendações no fim da home (/api/catalog/feed).
// Grade em colunas (masonry) com o aspecto real de cada miniatura; cada card diz
// por que foi recomendado e leva à série/criador de onde veio.
import { useEffect, useMemo, useRef, useState } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Compass, Heart, Loader2, Play, Shuffle, Sparkles, Star, Video } from 'lucide-react'
import { buildStreamUrlSync, buildThumbnailUrlSync, cn } from '@/lib/utils'
import { library, formatDuration, type FeedItem, type FeedScope, type TopicMini } from '@/lib/library'
import { errorMessage, progressPct, type OpenFn } from './catalogShared'
import { defaultAspect, useHoverPreview } from './HomeTab'
import { useReleasingMediaRef } from '@/hooks/useReleasingMediaRef'

const SCOPES: Array<{ id: FeedScope; label: string }> = [
  { id: 'all', label: 'Tudo' }, { id: 'series', label: 'Animes' }, { id: 'creators', label: 'Criadores' }, { id: 'films', label: 'Filmes e Séries' },
]
const SCOPE_KEY = 'drivelocal:feed-scope'
const SEED_KEY = 'drivelocal:feed-seed'

// Semente da sessão: voltar para a home mantém a mesma ordem; "Embaralhar" troca.
function sessionSeed(): number {
  try {
    const v = Number(sessionStorage.getItem(SEED_KEY))
    if (Number.isFinite(v) && v > 0) return v
  } catch { /* sem armazenamento */ }
  return newSeed()
}
function newSeed(): number {
  const v = 1 + Math.floor(Math.random() * 2_000_000_000)
  try { sessionStorage.setItem(SEED_KEY, String(v)) } catch { /* sem armazenamento */ }
  return v
}
function savedScope(): FeedScope {
  try {
    const v = localStorage.getItem(SCOPE_KEY)
    if (SCOPES.some((s) => s.id === v)) return v as FeedScope
  } catch { /* sem armazenamento */ }
  return 'all'
}

// Cor da estante de onde o item vem (mesmas cores da barra lateral).
function accentOf(it: FeedItem): string {
  if (it.reasonTopic?.kind === 'creator') return '#FF6B9A'
  if (it.root === 'Filmes') return '#E8B93C'
  if (it.root === 'Animes' || it.reasonTopic?.kind === 'series') return '#7B8BFF'
  return '#A8E3D0'
}

const REASON: Record<FeedItem['reasonKind'], { icon: typeof Heart; label: string }> = {
  favorite: { icon: Heart, label: 'Favorito' },
  next: { icon: Play, label: 'Próximo episódio' },
  affinity: { icon: Sparkles, label: 'Pra você' },
  new: { icon: Star, label: 'Novo' },
  discover: { icon: Compass, label: 'Descubra' },
}

// Ancestral que rola (overflow auto/scroll); null = a janela.
function scrollParent(el: HTMLElement): HTMLElement | null {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const o = getComputedStyle(p).overflowY
    if ((o === 'auto' || o === 'scroll') && p.scrollHeight > p.clientHeight) return p
  }
  return null
}

// Aspecto das miniaturas, já medido (compartilhado com os cards: sem pulo ao montar).
const aspects = new Map<number, number>()
function measure(items: FeedItem[], ms = 1500): Promise<void> {
  const todo = items.filter((it) => !aspects.has(it.id))
  if (!todo.length || typeof Image === 'undefined') return Promise.resolve()
  const one = (it: FeedItem) => new Promise<void>((done) => {
    const img = new Image()
    img.onload = () => { if (img.naturalWidth && img.naturalHeight) aspects.set(it.id, img.naturalWidth / img.naturalHeight); done() }
    img.onerror = () => done()
    img.src = buildThumbnailUrlSync(it.path)
  })
  return Promise.race([Promise.all(todo.map(one)).then(() => undefined), new Promise<void>((r) => setTimeout(r, ms))])
}
const aspectOf = (it: FeedItem) => Math.max(0.56, Math.min(2, aspects.get(it.id) ?? defaultAspect(it)))

// Masonry estável: cada item fica na coluna onde entrou; os novos vão para a
// coluna mais baixa, medida no DOM (corrige o erro das estimativas) e com o
// aspecto real das miniaturas, carregadas antes de entrar na grade.
function useMasonry(items: FeedItem[], cols: number, key: string, grid: React.RefObject<HTMLDivElement>) {
  const [layout, setLayout] = useState<{ key: string; cols: FeedItem[][] }>({ key: '', cols: [] })
  const run = useRef(0)
  useEffect(() => {
    const k = `${key}:${cols}`
    const fresh = layout.key !== k
    const have = new Set(fresh ? [] : layout.cols.flat().map((it) => it.id))
    const fresh2 = items.filter((it) => !have.has(it.id))
    if (!fresh && !fresh2.length) return
    const id = ++run.current
    void measure(fresh2).then(() => {
      if (id !== run.current) return
      const el = grid.current
      const colEls = el ? Array.from(el.children) as HTMLElement[] : []
      const w = colEls[0]?.clientWidth || 240
      const gap = 20 / w // gap-5 entre cards
      const text = 62 / w // título + linha do motivo
      const out: FeedItem[][] = fresh ? Array.from({ length: cols }, () => []) : layout.cols.map((c) => [...c])
      const h = out.map((c, i) => (fresh || !colEls[i] ? c.reduce((n, it) => n + 1 / aspectOf(it) + text + gap, 0) : colEls[i].offsetHeight / w + gap))
      for (const it of fresh2) {
        const c = h.indexOf(Math.min(...h))
        out[c].push(it)
        h[c] += 1 / aspectOf(it) + text + gap
      }
      setLayout({ key: k, cols: out })
    })
  }, [items, cols, key, layout, grid])
  return layout.key === `${key}:${cols}` ? layout.cols : []
}

function useColumns(ref: React.RefObject<HTMLElement>) {
  const [cols, setCols] = useState(4)
  useEffect(() => {
    const el = ref.current
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(([e]) => {
      const w = e.contentRect.width
      setCols(w < 520 ? 2 : w < 760 ? 3 : w < 1100 ? 4 : 5)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [ref])
  return cols
}

export function HomeFeed({ onOpen, onOpenTopic }: { onOpen: OpenFn; onOpenTopic: (t: TopicMini) => void }) {
  const [scope, setScopeState] = useState<FeedScope>(savedScope)
  const [seed, setSeed] = useState(sessionSeed)
  const setScope = (v: FeedScope) => {
    setScopeState(v)
    try { localStorage.setItem(SCOPE_KEY, v) } catch { /* sem armazenamento */ }
  }
  // fora da chave da biblioteca: invalidar a coleção não recarrega todas as páginas
  const q = useInfiniteQuery({
    queryKey: ['feed', scope, seed],
    queryFn: ({ pageParam }) => library.feed(scope, seed, pageParam),
    initialPageParam: 0,
    getNextPageParam: (last) => (last.next >= 0 ? last.next : undefined),
    staleTime: Infinity,
  })
  const items = useMemo(() => {
    const seen = new Set<number>()
    return (q.data?.pages ?? []).flatMap((p) => p.items).filter((it) => !seen.has(it.id) && !!seen.add(it.id))
  }, [q.data])

  const grid = useRef<HTMLDivElement>(null)
  const cols = useColumns(grid)
  const columns = useMasonry(items, cols, `${scope}:${seed}`, grid)
  const indexOf = useMemo(() => new Map(items.map((it, i) => [it.id, i])), [items])

  // carrega a próxima página bem antes do fim: a margem vale no contêiner que
  // rola de verdade (o observador sem root mede contra a janela, que não rola)
  const sentinel = useRef<HTMLDivElement>(null)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q
  const placed = columns.reduce((n, c) => n + c.length, 0)
  useEffect(() => {
    const el = sentinel.current
    if (!el || !hasNextPage || isFetchingNextPage || placed < items.length || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(([e]) => {
      if (e.isIntersecting) void fetchNextPage()
    }, { root: scrollParent(el), rootMargin: '1800px 0px' })
    io.observe(el)
    return () => io.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage, placed, items.length])

  const shuffle = () => setSeed(newSeed())

  return (
    <section aria-label="Para você" className="space-y-5 pt-2">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
        <div>
          <h2 className="font-display text-3xl font-extrabold tracking-tighter sm:text-4xl">Para você</h2>
          <p className="mt-1 text-sm text-muted-foreground">Pelo que você assiste e favorita, com novidades e umas surpresas.</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div role="tablist" aria-label="Filtrar recomendações" className="flex max-w-full gap-1 overflow-x-auto rounded-full bg-secondary p-1">
            {SCOPES.map((s) => (
              <button key={s.id} type="button" role="tab" aria-selected={scope === s.id} onClick={() => setScope(s.id)}
                className={cn('h-8 shrink-0 rounded-full px-3 text-sm transition-colors sm:px-3.5', scope === s.id ? 'bg-foreground font-semibold text-background' : 'text-muted-foreground hover:text-foreground')}>
                {s.label}
              </button>
            ))}
          </div>
          <button type="button" onClick={shuffle} title="Sortear outra ordem"
            className="flex h-10 items-center gap-2 rounded-full border px-4 text-sm hover:bg-secondary">
            <Shuffle className="h-4 w-4" />Embaralhar
          </button>
        </div>
      </div>

      {q.error && <p className="text-sm text-destructive">{errorMessage(q.error)}</p>}
      <div ref={grid} className="flex items-start gap-3 sm:gap-4" data-testid="feed-grid">
        {q.isPending || (items.length > 0 && columns.length === 0)
          ? Array.from({ length: cols }, (_, c) => (
            <div key={c} className="flex min-w-0 flex-1 flex-col gap-5">
              {[0, 1, 2].map((k) => <div key={k} className="animate-pulse rounded-2xl bg-muted" style={{ aspectRatio: (c + k) % 3 === 1 ? '9 / 14' : '16 / 9' }} />)}
            </div>
          ))
          : columns.map((col, c) => (
            <div key={c} className="flex min-w-0 flex-1 flex-col gap-5">
              {col.map((it) => (
                <FeedCard key={it.id} item={it} onOpen={() => onOpen(items, indexOf.get(it.id) ?? 0)} onTopic={onOpenTopic} />
              ))}
            </div>
          ))}
      </div>
      {!q.isPending && items.length === 0 && !q.error && (
        <p className="py-10 text-center text-sm text-muted-foreground">Nada para recomendar aqui ainda.</p>
      )}
      <div ref={sentinel} className="flex justify-center py-6">
        {isFetchingNextPage && <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" aria-label="Carregando mais" />}
        {!hasNextPage && items.length > 0 && (
          <div className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
            Você viu tudo por aqui.
            <button type="button" onClick={shuffle} className="flex h-10 items-center gap-2 rounded-full border px-4 text-foreground hover:bg-secondary">
              <Shuffle className="h-4 w-4" />Embaralhar de novo
            </button>
          </div>
        )}
      </div>
    </section>
  )
}

function FeedCard({ item, onOpen, onTopic }: { item: FeedItem; onOpen: () => void; onTopic: (t: TopicMini) => void }) {
  const [aspect, setAspect] = useState(() => aspects.get(item.id) ?? defaultAspect(item))
  const [failed, setFailed] = useState(false)
  const preview = useHoverPreview(item.kind === 'video')
  const previewRef = useReleasingMediaRef()
  const title = item.title || item.name
  const accent = accentOf(item)
  const r = REASON[item.reasonKind] ?? REASON.discover
  const pct = progressPct(item)
  const topic = item.reasonTopic
  const startAt = Math.min(60, (item.durationS ?? 0) * 0.2)
  return (
    <article className="group min-w-0" onMouseEnter={preview.start} onMouseLeave={preview.stop}>
      <button type="button" onClick={onOpen} aria-label={`Abrir ${title}`}
        className="relative block w-full overflow-hidden rounded-2xl bg-muted outline-none ring-offset-2 ring-offset-background transition-transform duration-200 focus-visible:ring-2 focus-visible:ring-ring group-hover:-translate-y-0.5"
        style={{ aspectRatio: String(Math.max(0.56, Math.min(2, aspect))) }}>
        {failed ? (
          <span className="flex h-full w-full items-center justify-center"><Video className="h-8 w-8 text-muted-foreground" /></span>
        ) : (
          <img src={buildThumbnailUrlSync(item.path)} alt="" loading="lazy" decoding="async" draggable={false}
            onLoad={(e) => { const { naturalWidth: w, naturalHeight: h } = e.currentTarget; if (w > 0 && h > 0) { aspects.set(item.id, w / h); setAspect(w / h) } }}
            onError={() => setFailed(true)}
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-[1.04]" />
        )}
        {preview.show && (
          <video ref={previewRef} src={`${buildStreamUrlSync(item.path)}#t=${Math.floor(startAt)}`} muted autoPlay playsInline loop preload="metadata"
            className="absolute inset-0 h-full w-full object-cover" />
        )}
        <span className="pointer-events-none absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-black/60 to-transparent" />
        <span title={item.reason}
          className="absolute left-2 top-2 flex items-center gap-1 rounded-full bg-black/65 py-1 pl-1.5 pr-2.5 text-[11px] font-semibold text-white backdrop-blur-sm">
          <r.icon className="h-3.5 w-3.5" style={{ color: accent }} />{r.label}
        </span>
        {item.durationS ? (
          <span className="absolute bottom-2 right-2 rounded-md bg-black/70 px-1.5 py-0.5 text-[11px] font-medium tabular-nums text-white">
            {formatDuration(item.durationS)}
          </span>
        ) : null}
        {pct > 0 && (
          <span className="absolute inset-x-0 bottom-0 h-[3px] bg-white/25"><span className="block h-full bg-[#FF5A5A]" style={{ width: `${pct}%` }} /></span>
        )}
      </button>
      <p className="mt-2.5 line-clamp-2 text-[15px] font-semibold leading-snug" title={item.path}>{title}</p>
      <p className="mt-1 flex min-w-0 items-center gap-1.5 text-[13px] text-muted-foreground">
        <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: accent }} />
        {topic ? (
          <button type="button" onClick={() => onTopic(topic)} title={item.reason}
            className="min-w-0 truncate hover:text-foreground hover:underline">
            {item.reasonKind === 'next' ? `Próximo de ${topic.title}` : topic.title}
          </button>
        ) : (
          <span className="truncate">{item.reason}</span>
        )}
      </p>
    </article>
  )
}
