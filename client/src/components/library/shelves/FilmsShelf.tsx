// Estante Filmes e Séries: séries fora de Animes, destaque em formato cinema, franquias, filmes de anime
// (os soltos e longos da estante Animes, que aparecem nas duas) e grade A–Z.
import { useEffect, useState } from 'react'
import { useNavParam } from '@/lib/nav'
import { Check, FileWarning, Play } from 'lucide-react'
import { cn, formatFileSize } from '@/lib/utils'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Pencil } from 'lucide-react'
import { library, formatDuration, type LibraryItem, type TopicCard } from '@/lib/library'
import { useAuthRole } from '@/contexts/AuthContext'
import { isInProgress } from '@/lib/mediaRules'
import { Thumbnail } from '@/components/Thumbnail'
import { FavoriteButton, Status, asFile, errorMessage, progressPct, useLoad, type OpenFn } from '../catalogShared'
import { ArtFrame, useArt, useExtMeta } from '../ExtMeta'
import { EditEntryButton } from '../EditEntryButton'
import { FilterChips, SORT_LABEL, ShelfHeader, initial, shelfById, type Shelf, type Sort, type View } from './shelfUi'
import { SeriesDetail } from './series'
import { animeFilms as loadAnimeFilms, filmInfo, franchises, inFranchise, pickHeroes, type Franchise } from './films'

const filmTitle = (it: LibraryItem) => filmInfo(it).title
const ANIME = shelfById('animes')
const isAnime = (it: LibraryItem) => it.root === ANIME.root
const cmp = new Intl.Collator('pt-BR', { numeric: true, sensitivity: 'base' })

function sortFilms(items: LibraryItem[], by: Sort): LibraryItem[] {
  const out = [...items]
  if (by === 'added') return out.sort((a, b) => b.mtime - a.mtime)
  if (by === 'longest') return out.sort((a, b) => (b.durationS ?? 0) - (a.durationS ?? 0))
  if (by === 'largest') return out.sort((a, b) => b.size - a.size)
  return out.sort((a, b) => cmp.compare(filmTitle(a), filmTitle(b)))
}

const hours = (s: number) => {
  const h = Math.floor(s / 3600)
  const m = Math.round((s % 3600) / 60)
  return h ? `${h} h ${m} min` : `${m} min`
}

const HERO_MS = 9000
const reducedMotion = () => typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches

// Destaque girando entre os sorteados; para com o mouse em cima ou com movimento reduzido.
function FilmHeroes({ items, shelf, onPlay }: { items: LibraryItem[]; shelf: Shelf; onPlay: (it: LibraryItem) => void }) {
  const [idx, setIdx] = useState(0)
  const [paused, setPaused] = useState(false)
  useEffect(() => {
    if (paused || items.length < 2 || reducedMotion()) return
    const t = window.setTimeout(() => setIdx((i) => (i + 1) % items.length), HERO_MS)
    return () => window.clearTimeout(t)
  }, [idx, paused, items.length])
  const cur = Math.min(idx, items.length - 1)
  const it = items[cur]
  return (
    <div className="relative" onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}
      onFocusCapture={() => setPaused(true)} onBlurCapture={() => setPaused(false)}>
      <FilmHero key={it.id} it={it} shelf={shelf} onPlay={() => onPlay(it)} />
      {items.length > 1 && (
        <div className="absolute bottom-5 right-5 flex items-center gap-1.5 sm:bottom-9 sm:right-9" role="tablist" aria-label="Destaques">
          {items.map((x, i) => (
            <button key={x.id} type="button" role="tab" aria-selected={i === cur} aria-label={`Destaque ${i + 1}`}
              onClick={() => setIdx(i)}
              className={cn('h-1.5 rounded-full transition-all', i === cur ? 'w-6 bg-white' : 'w-1.5 bg-white/40 hover:bg-white/70')} />
          ))}
        </div>
      )}
    </div>
  )
}

// Destaque de Filmes: still em formato cinema, com tarjas pretas e acento dourado.
function FilmHero({ it, shelf, onPlay }: { it: LibraryItem; shelf: Shelf; onPlay: () => void }) {
  const pct = progressPct(it)
  const going = isInProgress(it)
  const info = filmInfo(it)
  const meta = useExtMeta('movie', info.title, `media:${it.id}`)
  const art = useArt(`media:${it.id}`)
  const year = meta?.year ?? info.year
  return (
    <section aria-label="Destaque" className="overflow-hidden rounded-[28px] bg-black animate-in fade-in duration-700">
      <div className="h-3 bg-black sm:h-5" />
      <button type="button" onClick={onPlay} aria-label={`${going ? 'Continuar' : 'Assistir'} ${info.title}`}
        className="group relative block aspect-[16/9] w-full overflow-hidden text-left sm:aspect-[2.39/1]">
        <ArtFrame art={art} fallback={<Thumbnail file={asFile(it)} className="h-full w-full object-cover transition-transform duration-700 group-hover:scale-[1.02]" size="md" />} />
        <span className="absolute inset-0 bg-gradient-to-t from-black via-black/40 to-transparent" />
        <span className="absolute inset-x-0 bottom-0 flex flex-col gap-3 p-5 sm:p-9">
          <span className="text-sm font-semibold" style={{ color: isAnime(it) ? ANIME.color : shelf.color }}>
            {isAnime(it) ? 'Filme de anime' : 'Em cartaz hoje'}
          </span>
          <span className="line-clamp-2 max-w-3xl font-display text-3xl font-extrabold leading-[0.95] tracking-tighter text-white sm:text-6xl">{info.title}</span>
          {meta?.synopsis && <span className="hidden max-w-2xl text-[15px] leading-relaxed text-white/75 sm:line-clamp-2">{meta.synopsis}</span>}
          <span className="flex flex-wrap items-center gap-4">
            <span className="flex h-12 items-center gap-2 rounded-full px-7 text-base font-semibold" style={{ background: shelf.color, color: shelf.ink }}>
              <Play className="h-5 w-5 fill-current" />{going ? 'Continuar' : 'Assistir'}
            </span>
            {[year ? String(year) : '', it.durationS ? formatDuration(it.durationS) : ''].filter(Boolean).map((t) => <span key={t} className="text-[15px] text-white/75">{t}</span>)}
          </span>
        </span>
        {going && <span className="absolute inset-x-0 bottom-0 h-1 bg-white/20"><span className="block h-full bg-[#FF5A5A]" style={{ width: `${pct}%` }} /></span>}
      </button>
      <div className="h-3 bg-black sm:h-5" />
    </section>
  )
}

function FilmCard({ it, shelf, onOpen, title, num, sort }: {
  it: LibraryItem; shelf: Shelf; onOpen: () => void; title?: string; num?: number; sort?: Sort
}) {
  const pct = progressPct(it)
  const info = filmInfo(it)
  const done = !!it.progress?.completed
  const art = useArt(`media:${it.id}`)
  const meta = [
    sort === 'largest' ? formatFileSize(it.size) : '',
    info.year ? String(info.year) : '',
    it.durationS ? formatDuration(it.durationS) : '',
  ].filter(Boolean).join(' · ')
  return (
    <div className="group relative flex min-w-0 flex-col gap-2">
      <button type="button" onClick={onOpen} aria-label={`Abrir ${info.title}`} data-testid="film-card"
        className="relative block aspect-video overflow-hidden rounded-xl bg-muted outline-none ring-offset-2 ring-offset-background transition-shadow focus-visible:ring-2 group-hover:ring-2"
        style={{ ['--tw-ring-color' as string]: shelf.color }}>
        <ArtFrame art={art} fallback={<Thumbnail file={asFile(it)} className="h-full w-full object-cover" size="md" />} />
        {num != null && (
          <span className="absolute bottom-1 left-2.5 font-display text-5xl font-extrabold leading-none tracking-tighter text-white/90 drop-shadow-[0_2px_8px_rgba(0,0,0,0.7)]">{num}</span>
        )}
        {isAnime(it) && (
          <span className="absolute left-2 top-2 rounded-md px-1.5 py-0.5 text-[11px] font-bold" style={{ background: ANIME.color, color: ANIME.ink }}>Anime</span>
        )}
        {done && (
          <span className="absolute bottom-2 right-2 flex items-center gap-1 rounded-md bg-black/75 px-1.5 py-0.5 text-xs text-white"><Check className="h-3 w-3" />Visto</span>
        )}
        {pct > 0 && !done && <span className="absolute inset-x-0 bottom-0 h-[3px] bg-white/25"><span className="block h-full bg-[#FF5A5A]" style={{ width: `${pct}%` }} /></span>}
        {it.broken && (
          <span className="absolute right-2 top-2 inline-flex items-center gap-1 rounded-md bg-amber-500 px-1.5 py-0.5 text-[11px] font-bold text-black" title={it.broken}>
            <FileWarning className="h-3 w-3" />Corrompido
          </span>
        )}
      </button>
      <FavoriteButton item={it} className="absolute right-2 top-2 opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100" />
      <EditEntryButton compact entryRef={`media:${it.id}`} className="absolute right-12 top-2 opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100" />
      <span className="line-clamp-2 font-display text-[17px] font-bold leading-tight tracking-tight" title={it.path}>{title ?? info.title}</span>
      {meta && <span className="-mt-1 text-[13px] tabular-nums text-muted-foreground">{meta}</span>}
    </div>
  )
}

// Editar a franquia no Gerenciar. A achada pelo nome vira cadastrada antes,
// com os filmes na ordem em que aparecem.
function EditFranchise({ f }: { f: Franchise }) {
  if (useAuthRole() !== 'admin') return null
  return <EditFranchiseButton f={f} />
}

function EditFranchiseButton({ f }: { f: Franchise }) {
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true)
    try {
      const ref = f.id ? `topic:${f.id}` : (await library.admin.createEntry({
        cat: 'franchises', title: f.name, addIds: f.items.map((it) => it.id),
      })).ref
      if (!f.id) await library.admin.updateEntry({ ref, order: f.items.map((it) => it.id) })
      void navigate({ to: '/colecao/$tab', params: { tab: 'manage' }, search: { editar: ref, volta: 1 } as never })
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <button type="button" disabled={busy} onClick={() => void go()} aria-label={`Editar franquia ${f.name}`}
      className="flex h-11 items-center gap-2 rounded-full border px-5 text-sm font-medium hover:bg-secondary disabled:opacity-50">
      <Pencil className="h-4 w-4" />Editar
    </button>
  )
}

// Franquia: faixa com o nome, o andamento e os filmes na ordem.
function FranchiseRow({ f, shelf, onOpen }: { f: Franchise; shelf: Shelf; onOpen: OpenFn }) {
  const n = f.items.length
  const seen = f.items.filter((it) => it.progress?.completed).length
  const next = Math.max(0, f.items.findIndex((it) => !it.progress?.completed))
  const total = f.items.reduce((s, it) => s + (it.durationS ?? 0), 0)
  return (
    <section aria-label={`Franquia ${f.name}`} className="space-y-5 rounded-[24px] border p-4 sm:p-6"
      style={{ background: `${shelf.color}0F`, borderColor: `${shelf.color}33` }}>
      <div className="flex flex-wrap items-end gap-x-6 gap-y-3">
        <div className="min-w-0">
          <p className="text-xs font-bold uppercase tracking-[0.18em]" style={{ color: shelf.color }}>Franquia</p>
          <h3 className="font-display text-3xl font-extrabold leading-none tracking-tighter sm:text-4xl">{f.name}</h3>
          <p className="mt-1.5 text-sm text-muted-foreground">
            {[`${n} filmes`, total ? hours(total) : '', seen ? `${seen} de ${n} vistos` : ''].filter(Boolean).join(' · ')}
          </p>
        </div>
        <div className="flex-1" />
        <EditFranchise f={f} />
        <button type="button" onClick={() => onOpen(f.items, seen >= n ? 0 : next)}
          className="flex h-11 items-center gap-2 rounded-full px-6 text-sm font-semibold" style={{ background: shelf.color, color: shelf.ink }}>
          <Play className="h-4 w-4 fill-current" />{seen === 0 ? 'Assistir em ordem' : seen >= n ? 'Rever do começo' : `Continuar no ${next + 1}º`}
        </button>
      </div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-5 sm:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]">
        {f.items.map((it, i) => (
          <FilmCard key={it.id} it={it} shelf={shelf} num={i + 1} title={inFranchise(filmInfo(it).title, f.name)} onOpen={() => onOpen(f.items, i)} />
        ))}
      </div>
    </section>
  )
}

function TVSeriesCard({ t, shelf, onClick }: { t: TopicCard; shelf: Shelf; onClick: () => void }) {
  const art = useArt(`topic:${t.id}`)
  return (
    <button type="button" onClick={onClick} aria-label={`Abrir ${t.title}`}
      className="group flex min-w-0 flex-col gap-3 rounded-xl text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
      <div className="relative aspect-video overflow-hidden rounded-xl bg-muted ring-1 ring-white/5 transition-transform duration-200 group-hover:-translate-y-0.5">
        <ArtFrame art={art} fallback={t.coverPath && <Thumbnail file={asFile({ id: t.id, path: t.coverPath, name: t.title, kind: 'video' } as LibraryItem)} className="h-full w-full object-cover" size="md" />} />
        <span className="absolute left-2 top-2 rounded-full px-2.5 py-0.5 text-xs font-semibold" style={{ background: shelf.color, color: shelf.ink }}>
          {t.count} {t.count === 1 ? 'episódio' : 'episódios'}
        </span>
      </div>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="line-clamp-2 text-[15px] font-semibold leading-snug">{t.title}</span>
        <span className="text-[13px] text-muted-foreground">{t.watched > 0 ? `${Math.min(t.watched, t.count)} de ${t.count} vistos` : 'Não vista'}</span>
      </div>
    </button>
  )
}

// Série aberta (?serie=t<id>): episódios do tópico, como na estante Animes.
function TVSeriesOpen({ serie, shelf, onOpen, onBack }: { serie: string; shelf: Shelf; onOpen: OpenFn; onBack: () => void }) {
  const id = Number(serie.slice(1))
  const res = useLoad(['tv-serie', id], () => library.search({ topicId: id, limit: 400, sort: 'added' })
    .then((r) => r.groups.find((g) => g.series?.key === serie)?.series ?? r.groups.find((g) => g.series)?.series ?? null))
  if (res.loading || res.error) return <Status loading={res.loading} error={res.error} />
  if (!res.data) return <p className="text-sm text-muted-foreground">Série não encontrada.</p>
  return <SeriesDetail s={res.data} shelf={shelf} onOpen={onOpen} onBack={onBack} backLabel="Filmes e Séries" />
}

export function FilmsShelf({ shelf, onOpen }: { shelf: Shelf; onOpen: OpenFn; count?: number }) {
  const [serie, setSerie] = useNavParam('serie')
  if (serie && /^t\d+$/.test(serie)) return <TVSeriesOpen serie={serie} shelf={shelf} onOpen={onOpen} onBack={() => setSerie(null)} />
  return <FilmsAndSeries shelf={shelf} onOpen={onOpen} onSerie={(id) => setSerie(`t${id}`)} />
}

function FilmsAndSeries({ shelf, onOpen, onSerie }: { shelf: Shelf; onOpen: OpenFn; onSerie: (id: number) => void }) {
  const [view, setView] = useState<View>('all')
  const [sort, setSort] = useState<Sort>('title')
  const flags = { unwatched: view === 'unwatched', favorites: view === 'favorites' }
  const res = useLoad(['films', shelf.root, view], () => library.items({ root: shelf.root, ...flags, limit: 400, sort: 'title' }))
  const anime = useLoad(['films-anime', view], () => loadAnimeFilms(flags))
  const tv = useLoad(['tv-series'], () => library.tvSeries())
  const saved = useLoad(['franchises'], () => library.franchises())
  const tvAll = tv.data ?? []
  const tvList = view === 'unwatched' ? tvAll.filter((t) => t.watched < t.count) : view === 'favorites' ? [] : tvAll
  const own = res.data?.items ?? []
  const animeFilms = anime.data ?? []
  const items = sortFilms([...own, ...animeFilms], sort)
  const going = items.filter(isInProgress)
  // Sorteio uma vez por visita, só depois de as duas listas (filmes e filmes de anime) chegarem;
  // guarda os ids para o progresso atualizado aparecer sem sortear de novo.
  const [heroIds, setHeroIds] = useState<number[] | null>(null)
  const settled = view === 'all' && !!res.data && !res.loading && !anime.loading
  useEffect(() => {
    if (settled && heroIds === null && items.length > 0) setHeroIds(pickHeroes(items).map((it) => it.id))
  }, [settled, heroIds, items])
  const current = new Map(items.map((it) => [it.id, it]))
  const heroes = (heroIds ?? []).map((id) => current.get(id)).filter((it): it is LibraryItem => !!it)
  const play = (it: LibraryItem) => onOpen(items, Math.max(0, items.indexOf(it)))
  // A–Z: franquias e filmes de anime em destaque; o resto em seções por letra.
  const byTitle = sort === 'title'
  // as cadastradas no Gerenciar valem; o resto se agrupa pelo começo do nome
  const byId = new Map([...own, ...animeFilms].map((it) => [it.id, it]))
  const manual: Franchise[] = (saved.data ?? []).map((s) => ({
    id: s.id, name: s.title, items: s.items.map((id) => byId.get(id)).filter((it): it is LibraryItem => !!it),
  })).filter((f) => f.items.length > 0)
  const taken = new Set(manual.flatMap((f) => f.items.map((it) => it.id)))
  const fr = byTitle ? [...manual, ...franchises(own.filter((it) => !taken.has(it.id)))].sort((a, b) => cmp.compare(a.name, b.name)) : []
  const inFr = new Set(fr.flatMap((f) => f.items.map((it) => it.id)))
  const rest = byTitle ? items.filter((it) => !inFr.has(it.id) && !isAnime(it)) : items
  const animeLoose = animeFilms.filter((it) => !inFr.has(it.id))
  const sections = byTitle
    ? rest.reduce<Array<{ letter: string; items: LibraryItem[] }>>((acc, it) => {
      const c = initial(filmTitle(it)).normalize('NFD').replace(/[̀-ͯ]/g, '')
      const letter = /[A-Z]/.test(c) ? c : '#'
      const last = acc[acc.length - 1]
      if (last?.letter === letter) last.items.push(it)
      else acc.push({ letter, items: [it] })
      return acc
    }, [])
    : [{ letter: '', items }]
  const grid = 'grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-[repeat(auto-fill,minmax(240px,1fr))] sm:gap-x-5'
  const loading = res.loading || anime.loading
  const total = own.length + animeFilms.length
  const ready = !!res.data
  return (
    <div className="space-y-7">
      <ShelfHeader shelf={shelf} count={ready ? total : undefined}
        sub={ready ? [
          `${total.toLocaleString('pt-BR')} ${total === 1 ? 'filme' : 'filmes'}`,
          animeFilms.length ? `${animeFilms.length} de anime` : '',
          fr.length ? `${fr.length} ${fr.length === 1 ? 'franquia' : 'franquias'}` : '',
          tvAll.length ? `${tvAll.length} ${tvAll.length === 1 ? 'série' : 'séries'}` : '',
          going.length ? `${going.length} pela metade` : '',
        ].filter(Boolean).join(' · ') : undefined} />
      <div className="flex flex-wrap items-center gap-2.5">
        <FilterChips value={view} onChange={setView} shelf={shelf} />
        <div className="flex-1" />
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          Ordenar
          <select value={sort} onChange={(e) => setSort(e.target.value as Sort)} className="h-[38px] rounded-[10px] border bg-card px-3 text-foreground">
            {(Object.keys(SORT_LABEL) as Sort[]).map((k) => <option key={k} value={k}>{SORT_LABEL[k]}</option>)}
          </select>
        </label>
      </div>
      <Status loading={loading} error={res.error ?? anime.error} />
      {view === 'all' && heroes.length > 0 && <FilmHeroes items={heroes} shelf={shelf} onPlay={play} />}
      {tvList.length > 0 && (
        <section aria-label="Séries" className="space-y-4">
          <h3 className="font-display text-[26px] font-bold tracking-tight">Séries</h3>
          <div className={grid}>
            {tvList.map((t) => <TVSeriesCard key={t.id} t={t} shelf={shelf} onClick={() => onSerie(t.id)} />)}
          </div>
        </section>
      )}
      {!loading && items.length === 0 && tvList.length === 0 && <p className="text-sm text-muted-foreground">Nada por aqui.</p>}
      {fr.map((f) => <FranchiseRow key={f.name} f={f} shelf={shelf} onOpen={onOpen} />)}
      {byTitle && animeLoose.length > 0 && (
        <section aria-label="Filmes de anime" className="space-y-4">
          <div>
            <h3 className="font-display text-[26px] font-bold tracking-tight">Filmes de anime</h3>
            <p className="text-sm text-muted-foreground">Também aparecem na estante Animes.</p>
          </div>
          <div className={grid}>
            {animeLoose.map((it) => <FilmCard key={it.id} it={it} shelf={shelf} onOpen={() => play(it)} />)}
          </div>
        </section>
      )}
      {byTitle && rest.length > 0 && (fr.length > 0 || animeFilms.length > 0 || tvList.length > 0) && (
        <h3 className="font-display text-[26px] font-bold tracking-tight">{fr.length || animeFilms.length ? 'Todos os outros' : 'Filmes'}</h3>
      )}
      {sections.filter((sec) => sec.items.length > 0).map((sec) => (
        <section key={sec.letter || 'all'} aria-label={sec.letter ? `Letra ${sec.letter}` : 'Filmes'}
          className={cn(sec.letter && 'grid gap-4 border-t pt-6 lg:grid-cols-[88px_minmax(0,1fr)]')}>
          {sec.letter && (
            <h3 className="font-display text-6xl font-extrabold leading-none tracking-tighter lg:sticky lg:top-0 lg:self-start" style={{ color: shelf.color }}>
              {sec.letter}
            </h3>
          )}
          <div className={grid}>
            {sec.items.map((it) => <FilmCard key={it.id} it={it} shelf={shelf} sort={sort} onOpen={() => play(it)} />)}
          </div>
        </section>
      ))}
    </div>
  )
}
