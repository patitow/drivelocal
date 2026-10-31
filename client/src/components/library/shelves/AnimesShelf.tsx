// Estante de Animes: destaque, próximos episódios e séries em pilha.
import { useState } from 'react'
import { useNavParam } from '@/lib/nav'
import { Play } from 'lucide-react'
import { library, type LibraryItem, type SearchGroup, type SeriesGroup } from '@/lib/library'
import { Thumbnail } from '@/components/Thumbnail'
import { ItemGrid, Status, asFile, progressPct, useLoad, type OpenFn } from '../catalogShared'
import { FilterChips, SORT_LABEL, Segmented, ShelfHeader, Still, type Shelf, type Sort, type View } from './shelfUi'
import { NextUp, SeriesDetail, SeriesStackCard, epLabel } from './series'
import { filmInfo } from './films'
import { WebImg, useArt } from '../ExtMeta'
import { ANIME_FILM_MIN_S } from '@/lib/mediaRules'

// Destaque de Animes: a série em andamento (ou a mais nova) com régua de episódios.
function Spotlight({ s, shelf, onOpen, onDetail }: { s: SeriesGroup; shelf: Shelf; onOpen: OpenFn; onDetail: () => void }) {
  const target = s.next ?? s.items[0] ?? s.best
  const idx = Math.max(0, s.items.findIndex((it) => it.id === target.id))
  const cover = s.coverPath || s.best.path
  const art = useArt(s.topicId ? `topic:${s.topicId}` : undefined)
  const going = s.watched > 0
  return (
    <section aria-label="Destaque" className="relative isolate overflow-hidden rounded-[28px]" style={{ background: shelf.ink }}>
      {art?.banner
        ? <WebImg src={art.banner} className="absolute inset-0 -z-10 h-full w-full object-cover opacity-60" />
        : <Still path={cover} className="absolute inset-0 -z-10 h-full w-full scale-110 object-cover opacity-50 blur-2xl" />}
      <div className="absolute inset-0 -z-10" style={{ background: `linear-gradient(100deg, ${shelf.ink} 18%, ${shelf.ink}cc 48%, ${shelf.ink}33 100%)` }} />
      <div className="grid grid-cols-1 items-center gap-6 p-5 sm:p-8 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] lg:gap-10 lg:p-10">
        <div className="order-2 min-w-0 space-y-4 lg:order-1">
          <p className="text-sm font-semibold" style={{ color: shelf.color }}>{going ? 'Você está assistindo' : 'Chegou agora'}</p>
          <h3 className="line-clamp-3 font-display text-4xl font-extrabold leading-[0.95] tracking-tighter text-white sm:text-5xl">{s.title}</h3>
          <p className="text-[15px] text-white/70">
            {going ? `${s.watched} de ${s.total} episódios vistos` : `${s.total} ${s.total === 1 ? 'episódio' : 'episódios'}`}
            {s.year ? `, ${s.year}` : ''}
          </p>
          {/* Régua: um traço por episódio, cheio quando visto, alto no próximo. */}
          {s.items.length > 1 && s.items.length <= 160 && (
            <div aria-hidden className="flex h-7 items-end gap-[3px]">
              {s.items.map((it, i) => {
                const seen = it.progress?.completed || progressPct(it) >= 90
                return (
                  <span key={it.id} className="w-full max-w-[10px] flex-1 rounded-full"
                    style={{ height: i === idx ? 28 : 14, background: i === idx ? '#FFFFFF' : seen ? shelf.color : 'rgba(255,255,255,0.18)' }} />
                )
              })}
            </div>
          )}
          <div className="flex flex-wrap gap-2.5 pt-1">
            <button type="button" onClick={() => onOpen(s.items, idx)}
              className="flex h-12 items-center gap-2 rounded-full px-7 text-base font-semibold" style={{ background: shelf.color, color: shelf.ink }}>
              <Play className="h-5 w-5 fill-current" />{going ? `Continuar · ${epLabel(target)}` : 'Assistir'}
            </button>
            <button type="button" onClick={onDetail} className="h-12 rounded-full border border-white/30 px-6 text-base font-medium text-white hover:bg-white/10">
              Ver série
            </button>
          </div>
        </div>
        <button type="button" onClick={() => onOpen(s.items, idx)} aria-label={`Tocar ${epLabel(target)}`}
          className="group relative order-1 block aspect-video overflow-hidden rounded-2xl bg-black/40 shadow-2xl lg:order-2">
          <Thumbnail file={asFile(target)} className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-[1.03]" size="md" />
          {progressPct(target) > 0 && (
            <span className="absolute inset-x-0 bottom-0 h-1 bg-white/25"><span className="block h-full bg-[#FF5A5A]" style={{ width: `${progressPct(target)}%` }} /></span>
          )}
        </button>
      </div>
    </section>
  )
}

// Animes (pilhas por série) vêm da pasta raiz da estante.
export function RootShelf({ shelf, onOpen, count }: { shelf: Shelf; onOpen: OpenFn; count?: number }) {
  const [view, setView] = useState<View>('all')
  const [mode, setMode] = useState<'series' | 'episodes'>('series')
  const [sort, setSort] = useState<Sort>('added')
  // série aberta na URL (?serie=); picked guarda o card clicado até a lista chegar
  const [serie, setSerie] = useNavParam('serie')
  const [picked, setPicked] = useState<SeriesGroup | null>(null)
  const setSel = (s: SeriesGroup | null) => { setPicked(s); setSerie(s?.key) }
  const grouped = shelf.id === 'animes' && mode === 'series'
  const q = { root: shelf.root, unwatched: view === 'unwatched', favorites: view === 'favorites', limit: grouped ? 400 : 120 }
  const res = useLoad<{ groups: SearchGroup[]; items: LibraryItem[] }>(
    ['shelf', shelf.id, view, grouped],
    () => (grouped
      ? library.search({ ...q, sort: 'added' }).then((r) => ({ groups: r.groups, items: [] }))
      : library.items({ ...q, sort: shelf.id === 'animes' ? 'recent' : 'title' }).then((p) => ({ groups: [], items: p.items }))),
  )
  const series = (res.data?.groups ?? []).flatMap((g) => (g.type === 'series' && g.series ? [g.series] : []))
  // série vinda de fora da estante (home, link): pode morar em outra pasta raiz
  const listed = serie ? (series.find((s) => s.key === serie) ?? (picked?.key === serie ? picked : null)) : null
  const outsideId = serie && !listed && !res.loading && /^t\d+$/.test(serie) ? Number(serie.slice(1)) : 0
  const outside = useLoad(['serie-topic', outsideId], () => (outsideId
    ? library.search({ topicId: outsideId, limit: 400, sort: 'added' }).then((r) => r.groups.find((g) => g.series?.key === serie)?.series ?? null)
    : Promise.resolve(null)))
  const sel = listed ?? (outsideId ? outside.data ?? null : null)
  if (sel) return <SeriesDetail s={sel} shelf={shelf} onOpen={onOpen} onBack={() => setSel(null)} />
  if (serie && (res.loading || (outsideId && outside.loading))) return <Status loading error={null} />
  const singles = (res.data?.groups ?? []).flatMap((g) => (g.type === 'item' && g.item ? [g.item] : []))
  // Soltos com mais de 40 min são filmes: vêm antes das séries, não no fim.
  // filmes (também na estante Filmes), com o título limpo do nome do arquivo
  const films = singles.filter((it) => it.kind === 'video' && (it.durationS ?? 0) >= ANIME_FILM_MIN_S)
  const filmCards = films.map((it) => ({ ...it, title: filmInfo(it).title }))
  const loose = singles.filter((it) => !films.includes(it))
  // duração e tamanho da série: soma dos episódios
  const sum = (s: SeriesGroup, f: (it: LibraryItem) => number) => s.items.reduce((n, it) => n + f(it), 0)
  const sorted = sort === 'title' ? [...series].sort((a, b) => a.title.localeCompare(b.title, 'pt-BR'))
    : sort === 'longest' ? [...series].sort((a, b) => sum(b, (it) => it.durationS ?? 0) - sum(a, (it) => it.durationS ?? 0))
      : sort === 'largest' ? [...series].sort((a, b) => sum(b, (it) => it.size) - sum(a, (it) => it.size))
        : series
  const going = series.filter((s) => s.watched > 0 && s.next)
  const spot = going[0] ?? series[0]
  return (
    <div className="space-y-7">
      <ShelfHeader shelf={shelf} count={count}
        sub={grouped && res.data && view === 'all' && count != null
          ? [`${series.length.toLocaleString('pt-BR')} séries`, `${count.toLocaleString('pt-BR')} episódios`, going.length ? `${going.length} em andamento` : ''].filter(Boolean).join(' · ')
          : undefined}>
        {shelf.id === 'animes' && (
          <Segmented value={mode} onChange={setMode} options={[{ id: 'series', label: 'Séries' }, { id: 'episodes', label: 'Episódios' }]} shelf={shelf} label="Visualização" />
        )}
      </ShelfHeader>
      <div className="flex flex-wrap items-center gap-2.5">
        <FilterChips value={view} onChange={setView} shelf={shelf} />
        <div className="flex-1" />
        {grouped && (
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            Ordenar
            <select value={sort} onChange={(e) => setSort(e.target.value as Sort)}
              className="h-[38px] rounded-[10px] border bg-card px-3 text-foreground">
              {(Object.keys(SORT_LABEL) as Sort[]).map((k) => <option key={k} value={k}>{SORT_LABEL[k]}</option>)}
            </select>
          </label>
        )}
      </div>
      <Status loading={res.loading} error={res.error} />
      {res.data && grouped && (
        <>
          {view === 'all' && spot && <Spotlight s={spot} shelf={shelf} onOpen={onOpen} onDetail={() => setSel(spot)} />}
          {view === 'all' && <NextUp series={going.filter((s) => s !== spot)} onOpen={onOpen} />}
          {films.length > 0 && (
            <section aria-label="Filmes" className="space-y-3.5">
              <h3 className="font-display text-[22px] font-bold tracking-tight">Filmes</h3>
              <ItemGrid items={filmCards} onOpen={onOpen} />
            </section>
          )}
          <section aria-label="Séries" className="space-y-3.5">
            <h3 className="font-display text-[22px] font-bold tracking-tight">Todas as séries</h3>
            {sorted.length === 0 && <p className="text-sm text-muted-foreground">Nada por aqui.</p>}
            <div className="grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-x-5 gap-y-7">
              {sorted.map((s) => <SeriesStackCard key={s.key} s={s} shelf={shelf} onClick={() => setSel(s)} />)}
            </div>
          </section>
          {loose.length > 0 && (
            <section aria-label="Avulsos" className="space-y-3.5">
              <h3 className="font-display text-[22px] font-bold tracking-tight">Avulsos</h3>
              <ItemGrid items={loose} onOpen={onOpen} />
            </section>
          )}
        </>
      )}
      {res.data && !grouped && <ItemGrid items={res.data.items} onOpen={onOpen} />}
    </div>
  )
}
