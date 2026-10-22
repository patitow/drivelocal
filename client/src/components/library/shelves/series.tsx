// Séries: card em pilha, linha "Próximo episódio" e página da série.
import { ArtFrame, MetaBlurb, WebImg, useArt, useExtMeta } from '../ExtMeta'
import { useEffect, useRef, useState } from 'react'
import { Check, ChevronLeft, FileWarning, Play } from 'lucide-react'
import { cn } from '@/lib/utils'
import { formatDuration, type LibraryItem, type SeriesGroup } from '@/lib/library'
import { Thumbnail } from '@/components/Thumbnail'
import { asFile, progressPct, type OpenFn } from '../catalogShared'
import { Segmented, Still, totalTime, type Shelf } from './shelfUi'
import { EditEntryButton } from '../EditEntryButton'

export const epLabel = (it: LibraryItem) => (it.episode != null ? `${it.part ? `${it.part} · ` : ''}Ep. ${it.episode}` : it.title || it.name)

// Série como pilha de capas (o "volume" da pilha mostra que há vários episódios).
export function SeriesStackCard({ s, shelf, onClick }: { s: SeriesGroup; shelf: Shelf; onClick: () => void }) {
  const cover = s.coverPath || s.best.path
  const art = useArt(s.topicId ? `topic:${s.topicId}` : undefined)
  const meta = [s.watched > 0 ? `${s.watched} de ${s.total} vistos` : 'Não visto', s.year ? String(s.year) : ''].filter(Boolean).join(' · ')
  return (
    <button type="button" onClick={onClick} aria-label={`Abrir ${s.title}`} data-testid="series-card"
      className="group flex min-w-0 flex-col gap-3 rounded-xl text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <div className="relative px-2.5 pt-2">
        <div className="absolute inset-x-5 top-0 h-[calc(100%-8px)] rounded-xl" style={{ background: shelf.color, opacity: 0.18 }} />
        <div className="absolute inset-x-2.5 top-1 h-[calc(100%-8px)] rounded-xl" style={{ background: shelf.color, opacity: 0.32 }} />
        <div className="relative aspect-video overflow-hidden rounded-xl bg-muted transition-transform duration-200 group-hover:-translate-y-0.5">
          <ArtFrame art={art} fallback={<Thumbnail file={asFile({ ...s.best, path: cover })} className="h-full w-full object-cover" size="md" />} />
          <span className="absolute left-2 top-2 rounded-full bg-black/70 px-2.5 py-0.5 text-xs font-semibold text-white">
            {s.total} {s.total === 1 ? 'episódio' : 'episódios'}
          </span>
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-0.5 px-2.5">
        <span className="line-clamp-2 text-[15px] font-semibold leading-snug" title={s.folder || s.title}>{s.title}</span>
        <span className="text-[13px] text-muted-foreground">{meta}</span>
      </div>
    </button>
  )
}

// Linha "Próximo episódio": séries já começadas, com o episódio seguinte.
export function NextUp({ series, onOpen }: { series: SeriesGroup[]; onOpen: OpenFn }) {
  const list = series.filter((s) => s.next && s.watched > 0).slice(0, 6)
  if (list.length === 0) return null
  return (
    <section aria-label="Próximo episódio" className="space-y-3.5">
      <h3 className="font-display text-[22px] font-bold tracking-tight">Próximo episódio</h3>
      <div className="grid gap-4 sm:grid-cols-2 2xl:grid-cols-3">
        {list.map((s) => {
          const n = s.next!
          const idx = s.items.findIndex((it) => it.id === n.id)
          return (
            <button key={s.key} type="button" onClick={() => (idx >= 0 ? onOpen(s.items, idx) : onOpen([n], 0))}
              className="flex min-w-0 items-center gap-3.5 rounded-2xl bg-card p-3 text-left transition-colors hover:bg-secondary">
              <Thumbnail file={asFile(n)} className="aspect-video w-[132px] shrink-0 rounded-[10px] object-cover" size="sm" />
              <span className="min-w-0">
                <span className="block truncate text-[15px] font-semibold">{s.title}</span>
                <span className="text-[13px] text-muted-foreground">{epLabel(n)}{n.durationS ? ` · ${formatDuration(n.durationS)}` : ''}</span>
              </span>
            </button>
          )
        })}
      </div>
    </section>
  )
}

export const seen = (it: LibraryItem) => !!it.progress?.completed || progressPct(it) >= 90
// Nome do episódio quando ele diz mais do que só o número.
export const epName = (it: LibraryItem, series: string) => {
  const t = (it.title || it.name.replace(/\.[^.]+$/, '')).trim()
  return t && !t.toLowerCase().startsWith(series.toLowerCase()) && !/^(ep\.?|epis[oó]dio)?\s*\d+$/i.test(t) ? t : ''
}

export interface SeriesPart { id: string; label: string; items: LibraryItem[] }

// Divide os episódios em temporadas (pelo número) ou, sem isso, pela subpasta.
export function seriesParts(s: SeriesGroup): SeriesPart[] {
  const by = (key: (it: LibraryItem) => string, label: (k: string) => string): SeriesPart[] => {
    const m = new Map<string, LibraryItem[]>()
    for (const it of s.items) m.set(key(it), [...(m.get(key(it)) ?? []), it])
    return [...m].map(([id, items]) => ({ id, label: label(id), items }))
  }
  // o servidor já dividiu pelas pastas (anos, OVAs, extras), na ordem certa
  if (s.items.some((it) => it.part)) {
    const parts = by((it) => it.part ?? '', (k) => k || 'Episódios')
    if (parts.length > 1) return parts
  }
  const seasons = by((it) => String(it.season ?? 0), (k) => (k === '0' ? 'Extras' : `Temporada ${k}`))
  const order = (p: SeriesPart) => (p.id === '0' ? 999 : Number(p.id)) // extras por último
  if (seasons.length > 1) return seasons.sort((a, b) => order(a) - order(b))
  const dir = (it: LibraryItem) => it.path.slice(0, it.path.lastIndexOf('/'))
  const folders = by(dir, (k) => k.slice(k.lastIndexOf('/') + 1))
  if (folders.length < 2) return []
  // A pasta mais rasa é a série em si ("Episódios"); as demais levam o próprio nome
  // sem repetir o título da série ("Série Exemplo Specials" → "Specials").
  const depth = (p: SeriesPart) => p.id.split('/').length
  folders.sort((a, b) => depth(a) - depth(b) || a.id.localeCompare(b.id, 'pt-BR', { numeric: true }))
  return folders.map((p, i) => {
    const short = p.label.replace(new RegExp(`^${s.title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}[\\s:._-]*`, 'i'), '').trim()
    return { ...p, label: i === 0 && depth(p) < depth(folders[folders.length - 1]) ? 'Episódios' : short || p.label }
  })
}

// Página da série, no mesmo molde da página de obra dos Mangás: capa com camada
// deslocada, título, ações e a lista de episódios (por temporada quando houver).
export function SeriesDetail({ s, shelf, onOpen, onBack, backLabel }: { s: SeriesGroup; shelf: Shelf; onOpen: OpenFn; onBack: () => void; backLabel?: string }) {
  const target = s.next ?? s.items[0] ?? s.best
  const idx = Math.max(0, s.items.findIndex((it) => it.id === target.id))
  const cover = s.coverPath || s.best.path
  // Abas: por temporada quando os arquivos dizem; senão pela subpasta (ex.: "Specials").
  const parts = seriesParts(s)
  const [part, setPart] = useState(parts.find((p) => p.items.includes(target))?.id ?? parts[0]?.id ?? '')
  const shown = parts.length > 1 ? parts.find((p) => p.id === part)?.items ?? s.items : s.items
  const top = useRef<HTMLDivElement>(null)
  useEffect(() => { top.current?.scrollIntoView?.({ block: 'start' }) }, [s.key])
  const done = Math.max(s.watched, s.items.filter(seen).length)
  const pct = s.total > 0 ? Math.round((done / s.total) * 100) : 0
  const runtime = totalTime(s.items)
  const ref = s.topicId ? `topic:${s.topicId}` : undefined
  const meta = useExtMeta(shelf.id === 'animes' ? 'anime' : null, s.title, ref)
  const art = useArt(ref)
  return (
    <div ref={top} className="scroll-mt-9 space-y-8">
      <button type="button" onClick={onBack} className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ChevronLeft className="h-4 w-4" />{backLabel ?? shelf.label}
      </button>
      <header className="relative isolate overflow-hidden px-4 pb-8 pt-2 sm:mx-0 sm:rounded-[28px] sm:px-8 sm:pt-8 lg:px-10">
        {art?.banner
          ? <WebImg src={art.banner} className="absolute inset-0 -z-10 h-full w-full object-cover opacity-45" />
          : <Still path={cover} className="absolute inset-0 -z-10 h-full w-full scale-110 object-cover opacity-35 blur-3xl" />}
        <div className="absolute inset-0 -z-10" style={{ background: `linear-gradient(180deg, transparent 0%, hsl(var(--background)) 92%)` }} />
        <div className="flex flex-col gap-7 lg:flex-row lg:items-end">
          {/* pôster da ficha quando houver; senão o still do vídeo */}
          <span className={cn('relative block shrink-0', art?.cover ? 'aspect-[2/3] w-44 sm:w-56' : 'aspect-video w-[calc(100%-12px)] max-w-[460px]')}>
            <span className="absolute inset-0 translate-x-2.5 -translate-y-2 rounded-2xl" style={{ background: shelf.color, opacity: 0.35 }} />
            <span className="absolute inset-0 overflow-hidden rounded-2xl bg-muted shadow-2xl">
              <ArtFrame art={art?.cover ? { cover: art.cover } : undefined}
                fallback={<Thumbnail file={asFile({ ...s.best, path: cover })} className="h-full w-full object-cover" size="md" />} />
            </span>
          </span>
          <div className="min-w-0 flex-1 space-y-3">
            <h2 className="break-words font-display text-4xl font-extrabold leading-[0.95] tracking-tighter sm:text-6xl" style={{ color: shelf.color }}>{s.title}</h2>
            <p className="text-base text-muted-foreground">
              {[`${s.total} ${s.total === 1 ? 'episódio' : 'episódios'}`, parts.length > 1 ? parts.map((p) => p.label).join(', ') : '', s.year ? String(s.year) : meta?.year ? String(meta.year) : '', runtime].filter(Boolean).join(' · ')}
            </p>
            {meta && <MetaBlurb meta={meta} accent={shelf.color} />}
            {done > 0 && (
              <div className="flex max-w-md items-center gap-3">
                <span className="h-1.5 flex-1 rounded-full bg-white/10">
                  <span className="block h-full rounded-full" style={{ width: `${pct}%`, background: shelf.color }} />
                </span>
                <span className="shrink-0 text-sm text-muted-foreground">{done} de {s.total} vistos</span>
              </div>
            )}
            <div className="flex flex-wrap gap-2.5 pt-2">
              <button type="button" onClick={() => onOpen(s.items, idx)}
                className="flex h-12 items-center gap-2 rounded-full px-7 text-base font-semibold" style={{ background: shelf.color, color: shelf.ink }}>
                <Play className="h-5 w-5 fill-current" />{done > 0 || progressPct(target) > 0 ? `Continuar · ${epLabel(target)}` : 'Assistir do início'}
              </button>
              {(done > 0 || idx > 0) && (
                <button type="button" onClick={() => onOpen(s.items, 0)} className="h-12 rounded-full border px-6 text-base font-medium hover:bg-secondary">
                  Do primeiro episódio
                </button>
              )}
              <EditEntryButton entryRef={ref} />
            </div>
          </div>
        </div>
      </header>

      <section aria-label="Episódios" className="space-y-3.5">
        <div className="flex flex-wrap items-center gap-4">
          <h3 className="font-display text-[22px] font-bold tracking-tight">Episódios</h3>
          {parts.length > 1 && (
            <div className="max-w-full overflow-x-auto">
              <Segmented value={part} onChange={setPart} shelf={shelf} label="Temporada"
                options={parts.map((p) => ({ id: p.id, label: `${p.label} (${p.items.length})` }))} />
            </div>
          )}
        </div>
        <ol className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
          {shown.map((it) => {
            const i = s.items.indexOf(it)
            const current = it.id === target.id
            const p = progressPct(it)
            const name = epName(it, s.title)
            return (
              <li key={it.id}>
                <button type="button" onClick={() => onOpen(s.items, i)} aria-label={`Tocar ${epLabel(it)}`} data-testid="episode-row"
                  className={cn('group flex w-full min-w-0 items-center gap-3.5 rounded-2xl border p-2.5 text-left transition-colors hover:bg-secondary', current ? 'bg-card' : 'border-transparent bg-card/60')}
                  style={current ? { borderColor: shelf.color } : undefined}>
                  <span className="relative block aspect-video w-[136px] shrink-0 overflow-hidden rounded-[10px] bg-muted">
                    <Thumbnail file={asFile(it)} className={cn('h-full w-full object-cover', seen(it) && 'opacity-60')} size="sm" />
                    <span className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
                      <Play className="h-6 w-6 fill-white text-white" />
                    </span>
                    {p > 0 && !seen(it) && (
                      <span className="absolute inset-x-0 bottom-0 h-[3px] bg-white/25"><span className="block h-full bg-[#FF5A5A]" style={{ width: `${p}%` }} /></span>
                    )}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="text-[15px] font-semibold">{it.episode != null ? `Episódio ${it.episode}` : epLabel(it)}</span>
                      {current && <span className="rounded-full px-2 py-0.5 text-[11px] font-bold" style={{ background: shelf.color, color: shelf.ink }}>{done > 0 || p > 0 ? 'Próximo' : 'Comece aqui'}</span>}
                      {it.broken && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-amber-500 px-2 py-0.5 text-[11px] font-bold text-black" title={it.broken} data-testid="broken-badge">
                          <FileWarning className="h-3 w-3" />Corrompido
                        </span>
                      )}
                    </span>
                    {name && <span className="block truncate text-[13px] text-muted-foreground" title={name}>{name}</span>}
                    <span className="block text-[13px] text-muted-foreground">
                      {[it.durationS ? formatDuration(it.durationS) : '', p > 0 && !seen(it) ? `parou em ${Math.round(p)}%` : ''].filter(Boolean).join(' · ')}
                    </span>
                  </span>
                  {seen(it) && <Check className="h-5 w-5 shrink-0" style={{ color: shelf.color }} aria-label="Visto" />}
                </button>
              </li>
            )
          })}
        </ol>
      </section>
    </div>
  )
}
