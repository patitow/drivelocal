// Busca da Coleção agrupada por série (/api/catalog/search?group=series).
import { useEffect, useState } from 'react'
import { useNavParam } from '@/lib/nav'
import { toast } from 'sonner'
import { Search, Shuffle, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { library, type ItemQuery, type SearchGroup, type SeriesGroup } from '@/lib/library'
import { ItemGrid, Status, errorMessage, useLoad, type OpenFn } from './catalogShared'
import { SHELVES, SeriesDetail, SeriesStackCard, shelfById, type Shelf } from './shelves'

// Estante pela pasta raiz do item (a cor da pilha acompanha a estante).
const shelfOf = (root: string): Shelf => SHELVES.find((s) => s.root === root) ?? shelfById('animes')

export function SearchResults({ groups, onOpen, onSeries }: { groups: SearchGroup[]; onOpen: OpenFn; onSeries: (s: SeriesGroup) => void }) {
  if (groups.length === 0) return <p className="text-sm text-muted-foreground">Nada encontrado. Tente outro termo ou tire um filtro.</p>
  const series = groups.flatMap((g) => (g.type === 'series' && g.series ? [g.series] : []))
  // Itens soltos tocam em sequência entre si, na ordem da busca.
  const loose = groups.flatMap((g) => (g.type === 'item' && g.item ? [g.item] : []))
  return (
    <div className="space-y-9">
      {series.length > 0 && (
        <section aria-label="Séries" className="space-y-3.5">
          <h3 className="font-display text-[22px] font-bold tracking-tight">Séries</h3>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-x-5 gap-y-7">
            {series.map((s) => <SeriesStackCard key={s.key} s={s} shelf={shelfOf(s.best.root)} onClick={() => onSeries(s)} />)}
          </div>
        </section>
      )}
      {loose.length > 0 && (
        <section aria-label="Vídeos" className="space-y-3.5">
          {series.length > 0 && <h3 className="font-display text-[22px] font-bold tracking-tight">Vídeos</h3>}
          <ItemGrid items={loose} onOpen={onOpen} />
        </section>
      )}
    </div>
  )
}

type Filter = 'all' | 'unwatched' | 'week' | 'progress' | 'short' | 'long'
const FILTERS: Array<{ id: Filter; label: string }> = [
  { id: 'all', label: 'Tudo' },
  { id: 'unwatched', label: 'Não assistidos' },
  { id: 'week', label: 'Adicionados esta semana' },
  { id: 'progress', label: 'Em andamento' },
  { id: 'short', label: 'Curtos' },
  { id: 'long', label: 'Longos' },
]

const WEEK_S = 7 * 24 * 3600

function filterQuery(f: Filter): ItemQuery {
  switch (f) {
    case 'unwatched': return { unwatched: true }
    case 'week': return { since: Math.floor(Date.now() / 1000) - WEEK_S }
    case 'progress': return { inProgress: true }
    case 'short': return { kind: 'video', maxDurationS: 15 * 60 }
    case 'long': return { kind: 'video', minDurationS: 60 * 60 }
    default: return {}
  }
}

// Estantes com pasta própria viram escopo da busca; "Todas" não restringe.
const SCOPES = [{ id: 'all', label: 'Todas', root: '', color: '' }, ...SHELVES.filter((s) => s.root).map((s) => ({ id: s.id, label: s.label, root: s.root!, color: s.color }))]

export function SearchTab({ onOpen }: { onOpen: OpenFn }) {
  // termo e série aberta ficam na URL (?q=&serie=): voltar da série refaz a busca
  const [urlQ, setUrlQ] = useNavParam('q')
  const [serie, setSerie] = useNavParam('serie')
  const [q, setQ] = useState(urlQ ?? '')
  const [term, setTerm] = useState(urlQ ?? '')
  const [filter, setFilter] = useState<Filter>('all')
  const [scope, setScope] = useState('all')
  const [picked, setPicked] = useState<SeriesGroup | null>(null)
  const setSel = (s: SeriesGroup | null) => { setPicked(s); setSerie(s?.key) }
  useEffect(() => { if ((urlQ ?? '') !== term) setUrlQ(term || undefined, { replace: true }) }, [term]) // eslint-disable-line react-hooks/exhaustive-deps
  const root = SCOPES.find((s) => s.id === scope)?.root ?? ''

  // Busca enquanto digita, com uma pausa curta para não disparar a cada tecla.
  useEffect(() => {
    const t = window.setTimeout(() => setTerm(q.trim()), 300)
    return () => window.clearTimeout(t)
  }, [q])

  // Sem termo, a vitrine é de vídeos recentes (as páginas de mangá soltas poluiriam a lista).
  const query: ItemQuery = { q: term, root, ...(term ? {} : { kind: 'video' }), ...filterQuery(filter) }
  const res = useLoad(
    ['search', term, root, filter],
    () => library.search({ ...query, sort: term ? 'relevance' : 'added', limit: 60 }),
  )

  const surprise = async () => {
    try {
      const p = await library.items({ ...query, kind: query.kind ?? 'video', sort: 'random', limit: 1 })
      if (p.items.length) onOpen(p.items, 0)
      else toast.info('Nada para sortear com esses filtros')
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }

  const found = (res.data?.groups ?? []).flatMap((g) => (g.type === 'series' && g.series?.key === serie ? [g.series] : []))[0]
  const sel = serie ? (found ?? (picked?.key === serie ? picked : null)) : null
  if (sel) return <SeriesDetail s={sel} shelf={shelfOf(sel.best.root)} onOpen={onOpen} onBack={() => setSel(null)} backLabel="Busca" />

  const chip = 'h-9 shrink-0 rounded-[10px] px-4 text-sm transition-colors'
  return (
    <div className="space-y-7">
      <div className="flex items-center gap-3">
        <form role="search" className="relative min-w-0 max-w-[560px] flex-1" onSubmit={(e) => { e.preventDefault(); setTerm(q.trim()) }}>
          <Search className="pointer-events-none absolute left-[18px] top-1/2 h-[18px] w-[18px] -translate-y-1/2 text-muted-foreground" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Buscar séries, filmes, criadores"
            aria-label="Buscar"
            className="h-[46px] w-full rounded-full border border-[#33343D] bg-[#23242B] pl-12 pr-11 text-[15px] text-foreground outline-none placeholder:text-muted-foreground focus-visible:border-foreground/40"
          />
          {q && (
            <button type="button" aria-label="Limpar busca" onClick={() => { setQ(''); setTerm('') }}
              className="absolute right-3 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-full text-muted-foreground hover:text-foreground">
              <X className="h-4 w-4" />
            </button>
          )}
        </form>
        <div className="hidden flex-1 sm:block" />
        <button type="button" onClick={surprise} aria-label="Sortear algo"
          className="flex h-[46px] shrink-0 items-center gap-2 rounded-full border border-[#33343D] px-4 text-[15px] font-semibold transition-colors hover:bg-secondary sm:px-5">
          <Shuffle className="h-4 w-4" /><span className="hidden sm:inline">Sortear algo</span>
        </button>
      </div>

      <div role="tablist" aria-label="Filtros" className="shelf-rail -mx-4 flex gap-2 overflow-x-auto px-4 lg:mx-0 lg:flex-wrap lg:px-0">
        {FILTERS.map((f) => (
          <button key={f.id} type="button" role="tab" aria-selected={filter === f.id} onClick={() => setFilter(f.id)}
            className={cn(chip, filter === f.id ? 'bg-[#ECE8E1] font-semibold text-[#1A1B21]' : 'bg-[#2A2B33] text-foreground hover:bg-[#33343D]')}>
            {f.label}
          </button>
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
        <div role="radiogroup" aria-label="Estante" className="flex gap-1">
          {SCOPES.map((s) => (
            <button key={s.id} type="button" role="radio" aria-checked={scope === s.id} onClick={() => setScope(s.id)}
              className={cn('flex h-8 items-center gap-2 rounded-full px-3 transition-colors', scope === s.id ? 'bg-secondary font-semibold text-foreground' : 'text-muted-foreground hover:text-foreground')}>
              {s.color && <span className="h-2 w-2 rounded-full" style={{ background: s.color }} />}
              {s.label}
            </button>
          ))}
        </div>
        <div className="flex-1" />
        {res.data && (
          <span className="text-muted-foreground">
            {term
              ? `${res.data.groups.length.toLocaleString('pt-BR')} ${res.data.groups.length === 1 ? 'resultado' : 'resultados'} para “${term}” · ${res.data.total.toLocaleString('pt-BR')} ${res.data.total === 1 ? 'item' : 'itens'}${res.data.truncated ? ' · mostrando os primeiros' : ''}`
              : 'Adicionados recentemente'}
          </span>
        )}
      </div>

      <Status loading={res.loading} error={res.error} />
      {res.data && <SearchResults groups={res.data.groups} onOpen={onOpen} onSeries={setSel} />}
    </div>
  )
}
