// Estante de Criadores: cartões com mosaico do que chegou e perfil do criador.
import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { ArrowDownUp, ChevronLeft, Heart, Image as ImageIcon, Play } from 'lucide-react'
import { cn, formatFileSize } from '@/lib/utils'
import { library, formatDuration, type CreatorNews, type LibraryItem, type TopicInfo } from '@/lib/library'
import { Thumbnail } from '@/components/Thumbnail'
import { LIB, Status, asFile, errorMessage, progressPct, useLoad, type OpenFn } from '../catalogShared'
import { PASTEL, Segmented, ShelfHeader, Still, initial, totalTime, type Shelf } from './shelfUi'
import { CreatorActions, CreatorItemMenu } from './CreatorActions'
import { useNavParam } from '@/lib/nav'
import { useAuth } from '@/contexts/AuthContext'
import { MetaBlurb, WebImg, useArt, useExtMeta } from '../ExtMeta'
import { EditEntryButton } from '../EditEntryButton'

const WEEK = 7 * 24 * 3600

// Avatar do criador: a foto da ficha; sem ela (ou se falhar), a inicial no pastel.
function CreatorAvatar({ title, photo, className, style }: { title: string; photo?: string; className: string; style: React.CSSProperties }) {
  const [bad, setBad] = useState('')
  return (
    <span className={cn('flex shrink-0 items-center justify-center overflow-hidden rounded-full font-display font-extrabold', className)} style={style}>
      {photo && bad !== photo
        ? <WebImg src={photo} alt="" onFail={() => setBad(photo)} className="h-full w-full object-cover" />
        : initial(title)}
    </span>
  )
}

// Ordem dos itens do perfil (lembrada neste navegador).
type ItemSort = 'recent' | 'old' | 'name' | 'longest' | 'largest'
const SORT_KEY = 'drivelocal:creator-sort'
const SORTS: Array<{ id: ItemSort; label: string }> = [
  { id: 'recent', label: 'Mais recentes' }, { id: 'old', label: 'Mais antigos' }, { id: 'name', label: 'Nome' },
  { id: 'longest', label: 'Mais longos' }, { id: 'largest', label: 'Maiores arquivos' },
]
const loadSort = (): ItemSort => {
  try {
    const v = localStorage.getItem(SORT_KEY)
    if (SORTS.some((o) => o.id === v)) return v as ItemSort
  } catch { /* sem armazenamento */ }
  return 'recent'
}
const nameOf = (it: LibraryItem) => it.title || it.name
const byName = new Intl.Collator('pt-BR', { numeric: true, sensitivity: 'base' })
function sortItems(list: LibraryItem[], by: ItemSort): LibraryItem[] {
  const out = [...list]
  if (by === 'name') return out.sort((a, b) => byName.compare(nameOf(a), nameOf(b)))
  // fotos não têm duração: vão para o fim, as maiores primeiro
  if (by === 'longest') return out.sort((a, b) => (b.durationS ?? 0) - (a.durationS ?? 0) || b.size - a.size)
  if (by === 'largest') return out.sort((a, b) => b.size - a.size)
  return out.sort((a, b) => (by === 'recent' ? b.mtime - a.mtime : a.mtime - b.mtime) || byName.compare(nameOf(a), nameOf(b)))
}
// rodapé do card: o dado que está ordenando a lista
function footOf(it: LibraryItem, by: ItemSort): string {
  if (by === 'name') return nameOf(it)
  if (by === 'largest') return formatFileSize(it.size)
  if (by === 'longest' && it.durationS) return formatDuration(it.durationS)
  return new Date(it.mtime * 1000).toLocaleDateString('pt-BR')
}
// since: última visita à estante (servidor); sem ela, os últimos 7 dias.
const isFresh = (t: TopicInfo, since = Date.now() / 1000 - WEEK) => !!t.latest && t.latest > since

function ago(sec: number): string {
  const d = Math.max(0, Date.now() / 1000 - sec)
  if (d < 3600) return `há ${Math.max(1, Math.round(d / 60))} min`
  if (d < 86400) return `há ${Math.round(d / 3600)} h`
  const days = Math.round(d / 86400)
  if (days < 30) return days === 1 ? 'ontem' : `há ${days} dias`
  const months = Math.round(days / 30)
  if (months < 12) return months === 1 ? 'há 1 mês' : `há ${months} meses`
  const years = Math.round(days / 365)
  return years === 1 ? 'há 1 ano' : `há ${years} anos`
}

// Cartão do criador: mosaico com o que chegou por último e avatar sobreposto.
function CreatorCard({ t, i, active, fresh, onOpen }: { t: TopicInfo; i: number; active?: boolean; fresh: boolean; onOpen: () => void }) {
  const tiles = (t.recent?.length ? t.recent : t.coverPath ? [t.coverPath] : []).slice(0, 3)
  const pastel = PASTEL[i % PASTEL.length]
  const art = useArt(`topic:${t.id}`)
  const [badBanner, setBadBanner] = useState('')
  const banner = art?.banner && badBanner !== art.banner ? art.banner : ''
  return (
    <button type="button" onClick={onOpen} aria-pressed={active} data-testid="creator-card"
      className={cn('group flex min-w-0 flex-col overflow-hidden rounded-[20px] border bg-card text-left transition-colors hover:border-[#FF6B9A]/60', active && 'border-[#FF6B9A]')}>
      <span className={cn('relative grid aspect-[16/10] gap-[3px] overflow-hidden', tiles.length >= 3 ? 'grid-cols-[2fr_1fr] grid-rows-2' : tiles.length === 2 ? 'grid-cols-2' : 'grid-cols-1')}
        style={{ background: `${pastel}33` }}>
        {banner
          ? <span className="relative col-span-full row-span-full min-h-0 overflow-hidden bg-muted">
            <WebImg src={banner} onFail={() => setBadBanner(banner)} className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.04]" />
          </span>
          : tiles.map((p, k) => (
            <span key={p} className={cn('relative min-h-0 overflow-hidden bg-muted', tiles.length >= 3 && k === 0 && 'row-span-2')}>
              <Still path={p} className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.04]" />
            </span>
          ))}
        {fresh && (
          <span className="absolute right-2.5 top-2.5 rounded-full px-2.5 py-0.5 text-xs font-bold" style={{ background: '#FF6B9A', color: '#3A0A1D' }}>Novo</span>
        )}
        {t.favorite && <Heart className="absolute left-2.5 top-2.5 h-5 w-5 fill-[#FF6B9A] text-[#FF6B9A] drop-shadow" aria-label="Favorito" />}
      </span>
      <span className="relative flex items-end gap-3 px-4 pb-4">
        <CreatorAvatar title={t.title} photo={art?.cover} className="-mt-7 h-[60px] w-[60px] text-2xl"
          style={{ background: pastel, color: '#1A1B21', boxShadow: '0 0 0 4px hsl(var(--card))' }} />
        <span className="min-w-0 pt-2.5">
          <span className="block truncate text-[15px] font-semibold">{t.title}</span>
          <span className="block truncate text-[13px] text-muted-foreground">
            {[`${t.count} ${t.count === 1 ? 'item' : 'itens'}`, t.latest ? `último ${ago(t.latest)}` : ''].filter(Boolean).join(' · ')}
          </span>
        </span>
      </span>
    </button>
  )
}

// Perfil do criador: faixa na cor da estante, avatar, ações e abas Vídeos/Fotos.
function CreatorProfile({ t, i, shelf, all: creators, onOpen, onClose, onChanged, onRefresh }: { t: TopicInfo; i: number; shelf: Shelf; all: TopicInfo[]; onOpen: OpenFn; onClose: () => void; onChanged: () => void; onRefresh: () => void }) {
  const { role } = useAuth()
  const isAdmin = role === 'admin'
  const items = useLoad(['topic-items', t.id], () => library.topicItems(t.id))
  const [fav, setFav] = useState(t.favorite)
  // fotos e vídeos juntos por padrão; o filtro restringe a um só tipo
  const [tab, setTab] = useState<'all' | 'video' | 'image'>('all')
  const [sort, setSortState] = useState<ItemSort>(loadSort)
  const setSort = (v: ItemSort) => {
    setSortState(v)
    try { localStorage.setItem(SORT_KEY, v) } catch { /* sem armazenamento */ }
  }
  const all = items.data?.items ?? []
  const videos = all.filter((it) => it.kind === 'video')
  const photos = all.filter((it) => it.kind !== 'video')
  const shown = sortItems(tab === 'video' ? videos : tab === 'image' ? photos : all, sort)
  const pastel = PASTEL[i % PASTEL.length]
  const art = useArt(`topic:${t.id}`)
  const [badBanner, setBadBanner] = useState('')
  const banner = art?.banner && badBanner !== art.banner ? art.banner : ''
  // ficha curada no Gerenciar (bio, link)
  const bio = useExtMeta(null, t.title, `topic:${t.id}`)
  const toggleFav = async () => {
    try {
      const r = await library.favoriteTopic(t.id, !fav)
      setFav(r.favorite)
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }
  const meta = [`${videos.length} ${videos.length === 1 ? 'vídeo' : 'vídeos'}`, photos.length ? `${photos.length} fotos` : '', totalTime(videos)].filter(Boolean).join(' · ')
  // "Tudo" só faz sentido com os dois tipos
  const tabs = videos.length && photos.length
    ? [{ id: 'all' as const, label: 'Tudo', n: all.length }, { id: 'video' as const, label: 'Vídeos', n: videos.length }, { id: 'image' as const, label: 'Fotos', n: photos.length }]
    : []
  return (
    <section aria-label={`Perfil de ${t.title}`} className="overflow-hidden rounded-3xl border bg-card">
      <div className="relative h-[110px] sm:h-[140px]"
        style={{ background: shelf.color, backgroundImage: `radial-gradient(circle at 12% 120%, #FFB3C9 0 120px, transparent 121px), radial-gradient(circle at 92% -20%, #E0457A 0 150px, transparent 151px)` }}>
        {banner && (
          <>
            <WebImg src={banner} onFail={() => setBadBanner(banner)} className="absolute inset-0 h-full w-full object-cover" />
            <span className="absolute inset-0 bg-gradient-to-t from-black/50 to-transparent" />
          </>
        )}
        {/* à esquerda, junto do resto da navegação */}
        <button type="button" onClick={onClose} aria-label="Fechar perfil"
          className="absolute left-3 top-3 flex h-9 items-center gap-1 rounded-full bg-black/30 pl-2 pr-3.5 text-sm font-medium text-white hover:bg-black/50">
          <ChevronLeft className="h-5 w-5" />Criadores
        </button>
      </div>
      <div className="relative -mt-11 flex flex-wrap items-start gap-x-6 gap-y-4 px-5 pb-7 sm:px-8">
        <CreatorAvatar title={t.title} photo={art?.cover} className="h-[112px] w-[112px] text-[44px]"
          style={{ background: pastel, color: shelf.ink, boxShadow: '0 0 0 6px hsl(var(--card))' }} />
        <div className="flex min-w-0 flex-[1_1_260px] flex-col gap-1.5 sm:pt-[60px]">
          <h3 className="break-words font-display text-[34px] font-extrabold leading-tight tracking-tight">{t.title}</h3>
          <p className="text-[15px] text-muted-foreground">{items.data ? meta : ' '}</p>
          {bio?.synopsis && <MetaBlurb meta={bio} className="pt-1 [&_p]:text-muted-foreground" />}
        </div>
        <div className="flex gap-2.5 sm:pt-[64px]">
          <button type="button" disabled={videos.length === 0} onClick={() => onOpen(videos, 0)}
            className="flex h-11 items-center gap-2 rounded-full bg-foreground px-5 text-[15px] font-semibold text-background disabled:opacity-50">
            <Play className="h-4 w-4 fill-current" />Tocar tudo
          </button>
          <button type="button" onClick={toggleFav} aria-pressed={fav}
            className="flex h-11 items-center gap-2 rounded-full border px-5 text-[15px] hover:bg-secondary">
            <Heart className={cn('h-4 w-4', fav && 'fill-current')} style={fav ? { color: shelf.color } : undefined} />
            {fav ? 'Favoritado' : 'Favoritar'}
          </button>
          <CreatorActions t={t} all={creators} isAdmin={isAdmin} onChanged={onChanged} onRefresh={onRefresh} />
          <EditEntryButton entryRef={`topic:${t.id}`} className="h-11 px-5 text-[15px]" />
        </div>
      </div>
      {all.length > 0 && (
        <div className="flex flex-wrap items-center justify-between gap-x-6 border-t px-5 sm:px-8">
          <div role="tablist" aria-label="Seções do criador" className="flex gap-6 sm:gap-7">
            {tabs.map((x) => (
              <button key={x.id} type="button" role="tab" aria-selected={tab === x.id} onClick={() => setTab(x.id)}
                className={cn('h-[52px] border-b-[3px] text-[15px]', tab === x.id ? 'font-semibold' : 'border-transparent text-muted-foreground hover:text-foreground')}
                style={tab === x.id ? { borderColor: shelf.color } : undefined}>
                {x.label} <span className="text-sm text-muted-foreground">{x.n}</span>
              </button>
            ))}
          </div>
          <label className="flex h-[52px] items-center gap-2 text-sm text-muted-foreground">
            <ArrowDownUp className="h-4 w-4" />
            <span className="sr-only">Ordenar por</span>
            <select value={sort} onChange={(e) => setSort(e.target.value as ItemSort)} aria-label="Ordenar por"
              className="rounded-md bg-transparent py-1 pr-1 text-[15px] text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring">
              {SORTS.map((o) => <option key={o.id} value={o.id} className="bg-popover">{o.label}</option>)}
            </select>
          </label>
        </div>
      )}
      <div className="bg-background/40 px-5 pb-8 pt-6 sm:px-8">
        <Status loading={items.loading} error={items.error} />
        {items.data && shown.length === 0 && <p className="text-sm text-muted-foreground">Nada deste criador ainda.</p>}
        <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-4 sm:grid-cols-[repeat(auto-fill,minmax(170px,1fr))]">
          {shown.map((it, idx) => (
            <div key={it.id} className="group/item relative min-w-0">
            <button type="button" onClick={() => onOpen(shown, idx)} aria-label={`Abrir ${it.title || it.name}`}
              className="group flex w-full min-w-0 flex-col gap-2 text-left">
              <span className="relative block aspect-[9/16] overflow-hidden rounded-xl bg-muted">
                <Thumbnail file={asFile(it)} className="h-full w-full object-cover transition-transform duration-200 group-hover:scale-[1.03]" size="md" />
                {it.kind !== 'video' && (
                  <span className="absolute bottom-2 right-2 rounded-md bg-black/70 p-1 text-white" aria-label="Foto"><ImageIcon className="h-3.5 w-3.5" /></span>
                )}
                {it.durationS ? (
                  <span className="absolute bottom-2 right-2 rounded-md bg-black/70 px-1.5 py-0.5 text-xs tabular-nums text-white">{formatDuration(it.durationS)}</span>
                ) : null}
                {progressPct(it) > 0 && (
                  <span className="absolute inset-x-0 bottom-0 h-[3px] bg-white/30"><span className="block h-full bg-[#FF5A5A]" style={{ width: `${progressPct(it)}%` }} /></span>
                )}
              </span>
              <span className="truncate text-[13px] text-muted-foreground">{footOf(it, sort)}</span>
            </button>
            {isAdmin && <CreatorItemMenu item={it} t={t} all={creators} onChanged={onRefresh} />}
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

export function CreatorsShelf({ shelf, onOpen }: { shelf: Shelf; onOpen: OpenFn }) {
  const [view, setView] = useState<'all' | 'favorites' | 'fresh'>('all')
  // perfil aberto na URL (?criador=id)
  const [criador, setCriador] = useNavParam('criador')
  const setSel = (s: { t: TopicInfo; i: number } | null) => setCriador(s?.t.id)
  const top = useRef<HTMLDivElement>(null)
  const list = useLoad(['creators'], () => library.topics({ kind: 'creator', limit: 300 }))
  // Marca a estante como vista ao abrir; o "Novo" continua valendo para o que
  // chegou desde a visita anterior até sair da estante.
  const qc = useQueryClient()
  const [since, setSince] = useState<number | undefined>()
  const [visited, setVisited] = useState(false)
  useEffect(() => {
    library.markCreatorsSeen().then((prev) => {
      setSince(prev.since)
      setVisited(prev.seenAt > 0)
      qc.setQueryData<CreatorNews>(['creator-news'], (c) => (c ? { ...c, count: 0, favorites: 0, ids: [] } : c))
    }).catch(() => {})
  }, [qc])
  const all = list.data?.topics ?? []
  const selIdx = criador ? all.findIndex((t) => t.id === criador) : -1
  const sel = selIdx >= 0 ? { t: all[selIdx], i: selIdx } : null
  const fresh = all.filter((t) => isFresh(t, since))
  // A cor do avatar segue a posição na lista completa, para não mudar ao filtrar.
  const shown = (view === 'favorites' ? all.filter((t) => t.favorite) : view === 'fresh' ? [...fresh].sort((a, b) => (b.latest ?? 0) - (a.latest ?? 0)) : all)
  const pick = (t: TopicInfo, i: number) => {
    setSel({ t, i })
    top.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
  const total = list.data?.total
  const videos = all.reduce((n, t) => n + t.count, 0)
  return (
    <div ref={top} className="scroll-mt-9 space-y-7">
      <ShelfHeader shelf={shelf} count={total}
        sub={total != null ? [`${total} ${total === 1 ? 'criador' : 'criadores'}`, `${videos.toLocaleString('pt-BR')} itens`, fresh.length ? `${fresh.length} com novidades${visited ? ' desde a última visita' : ' esta semana'}` : ''].filter(Boolean).join(' · ') : undefined}>
        <Segmented value={view} onChange={setView} shelf={shelf} label="Filtro"
          options={[{ id: 'all', label: 'Todos' }, { id: 'favorites', label: 'Favoritos' }, { id: 'fresh', label: 'Com novidades' }]} />
      </ShelfHeader>
      {sel && (
        <CreatorProfile key={sel.t.id} t={sel.t} i={sel.i} shelf={shelf} all={all} onOpen={onOpen} onClose={() => setSel(null)}
          onChanged={() => { setSel(null); list.reload(); void qc.invalidateQueries() }}
          onRefresh={() => { list.reload(); void qc.invalidateQueries({ queryKey: [LIB, 'topic-items'] }) }} />
      )}
      <Status loading={list.loading} error={list.error} />
      <section aria-label="Todos os criadores" className="space-y-3.5">
        {sel && <h3 className="font-display text-[22px] font-bold tracking-tight">Todos os criadores</h3>}
        {list.data && shown.length === 0 && (
          <p className="text-sm text-muted-foreground">{view === 'fresh' ? (visited ? 'Nada novo desde a última visita.' : 'Nenhum criador recebeu itens nos últimos 7 dias.') : 'Nenhum criador favoritado ainda.'}</p>
        )}
        <div className="grid grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-4 sm:gap-5">
          {shown.map((t) => {
            const i = all.indexOf(t)
            return <CreatorCard key={t.id} t={t} i={i} active={sel?.t.id === t.id} fresh={isFresh(t, since)} onOpen={() => pick(t, i)} />
          })}
        </div>
      </section>
    </div>
  )
}
