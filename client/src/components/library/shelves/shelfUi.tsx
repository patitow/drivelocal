// Peças comuns às estantes: definição das estantes, cabeçalho, seletores e helpers.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { buildThumbnailUrlSync, cn } from '@/lib/utils'
import { library, type LibraryItem } from '@/lib/library'
import { useLoad } from '../catalogShared'
import { animeFilms } from './films'

export type ShelfId = 'animes' | 'mangas' | 'filmes' | 'criadores'

export interface Shelf {
  id: ShelfId
  label: string
  unit: string
  color: string // acento da estante
  ink: string // texto sobre o acento
  root?: string // pasta raiz que alimenta a estante
}

export const SHELVES: Shelf[] = [
  { id: 'animes', label: 'Animes', unit: 'títulos', color: '#7B8BFF', ink: '#12143A', root: 'Animes' },
  { id: 'mangas', label: 'Mangás', unit: 'obras', color: '#F4F0E6', ink: '#181715' },
  { id: 'filmes', label: 'Filmes e Séries', unit: 'títulos', color: '#E8B93C', ink: '#2A1F00', root: 'Filmes' },
  { id: 'criadores', label: 'Criadores', unit: 'criadores', color: '#FF6B9A', ink: '#3A0A1D' },
]

export const shelfById = (id: ShelfId) => SHELVES.find((s) => s.id === id)!

export const PASTEL = ['#FFB3C9', '#B8C2FF', '#F2D48A', '#A8E3D0', '#FFCFA3', '#D5B8FF', '#C9E3A0', '#9FD0F5']

// Contagem de cada estante para a barra lateral (falha em silêncio: mostra só o nome).
export function useShelfCounts(): Partial<Record<ShelfId, number>> {
  const r = useLoad(['shelf-counts'], async () => {
    const out: Partial<Record<ShelfId, number>> = {}
    await Promise.all([
      library.mangas().then((w) => { out.mangas = w.length }).catch(() => {}),
      library.items({ root: 'Animes', limit: 1 }).then((p) => { out.animes = p.total }).catch(() => {}),
      Promise.all([library.items({ root: 'Filmes', limit: 1 }), animeFilms().catch(() => [])])
        .then(([p, a]) => { out.filmes = p.total + a.length }).catch(() => {}),
      library.topics({ kind: 'creator', limit: 1 }).then((p) => { out.criadores = p.total }).catch(() => {}),
    ])
    return out
  })
  return r.data ?? {}
}

// Criadores com itens novos desde a última visita à estante (badge na lateral).
export function useCreatorNews(): number {
  const q = useQuery({ queryKey: ['creator-news'], queryFn: () => library.creatorNews(), staleTime: 5 * 60_000, refetchInterval: 15 * 60_000 })
  return q.data?.count ?? 0
}

export function ShelfHeader({ shelf, count, sub, children }: { shelf: Shelf; count?: number; sub?: string; children?: React.ReactNode }) {
  return (
    <header className="flex flex-wrap items-end gap-5">
      <div className="space-y-2">
        <h2
          className="font-display text-5xl font-extrabold leading-[0.95] tracking-tighter sm:text-7xl"
          style={{ color: shelf.color }}
        >
          {shelf.label}
        </h2>
        <p className="text-base text-muted-foreground">
          {sub ?? (count == null ? ' ' : `${count.toLocaleString('pt-BR')} ${shelf.unit}`)}
        </p>
      </div>
      <div className="flex-1" />
      {children}
    </header>
  )
}

// Seletor segmentado (Todos / Não vistos / Favoritos) na cor da estante.
export function Segmented<T extends string>({ value, onChange, options, shelf, label }: {
  value: T
  onChange: (v: T) => void
  options: Array<{ id: T; label: string }>
  shelf: Shelf
  label: string
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex rounded-xl bg-card p-1">
      {options.map((o) => {
        const on = o.id === value
        return (
          <button
            key={o.id}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(o.id)}
            className={cn('h-9 whitespace-nowrap rounded-[9px] px-3 text-sm transition-colors sm:px-4', on ? 'font-semibold' : 'text-muted-foreground hover:text-foreground')}
            style={on ? { background: shelf.color, color: shelf.ink } : undefined}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

export type View = 'all' | 'unwatched' | 'favorites'
export const VIEWS: Array<{ id: View; label: string }> = [
  { id: 'all', label: 'Todos' },
  { id: 'unwatched', label: 'Não vistos' },
  { id: 'favorites', label: 'Favoritos' },
]

// Chips de filtro (Todos / Não vistos / Favoritos) com contorno na cor da estante.
export function FilterChips({ value, onChange, shelf }: { value: View; onChange: (v: View) => void; shelf: Shelf }) {
  return (
    <div role="radiogroup" aria-label="Filtro" className="flex flex-wrap gap-2.5">
      {VIEWS.map((o) => {
        const on = o.id === value
        return (
          <button
            key={o.id}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(o.id)}
            className="h-[38px] rounded-[10px] border px-4 text-sm transition-colors hover:bg-secondary"
            style={on ? { borderColor: shelf.color, background: `${shelf.color}24` } : undefined}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}

export type Sort = 'added' | 'title' | 'longest' | 'largest'
export const SORT_LABEL: Record<Sort, string> = { added: 'Adicionados recentemente', title: 'A–Z', longest: 'Mais longos', largest: 'Maiores arquivos' }

// Imagem de fundo que some sozinha se a miniatura não existir.
export function Still({ path, className, alt = '' }: { path: string; className?: string; alt?: string }) {
  const [failed, setFailed] = useState(false)
  if (failed) return null
  return <img src={buildThumbnailUrlSync(path)} alt={alt} draggable={false} decoding="async" onError={() => setFailed(true)} className={className} />
}

export const initial = (title: string) => title.replace(/^[^\p{L}\p{N}]+/u, '').charAt(0).toUpperCase()

export function totalTime(items: LibraryItem[]): string {
  const s = items.reduce((n, it) => n + (it.durationS ?? 0), 0)
  if (s <= 0) return ''
  const h = Math.floor(s / 3600)
  return h > 0 ? `${h} h` : `${Math.max(1, Math.round(s / 60))} min`
}
