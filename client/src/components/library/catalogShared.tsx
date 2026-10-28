// Peças comuns das telas da Coleção (cards, grade, carregamento).
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Heart, Loader2, Play } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Thumbnail } from '@/components/Thumbnail'
import { cn } from '@/lib/utils'
import { library, formatDuration, MATURITY_LABEL, type LibraryItem } from '@/lib/library'

export type OpenFn = (list: LibraryItem[], index: number) => void

// Episódio aberto de uma lista misturada (ex.: "Continuar assistindo"): a fila do player
// vira a série dele, na ordem dos episódios, para o "próximo" ser o episódio seguinte.
export async function inSeries(list: LibraryItem[], index: number): Promise<{ list: LibraryItem[]; index: number }> {
  const it = list[index]
  const topic = it?.topics.find((t) => t.kind === 'series')
  if (!topic || list.every((x) => x.topics.some((t) => t.id === topic.id))) return { list, index }
  try {
    const r = await library.search({ topicId: topic.id, limit: 500, sort: 'added' })
    const series = r.groups.find((g) => g.series?.items.some((x) => x.id === it.id))?.series
    const i = series ? series.items.findIndex((x) => x.id === it.id) : -1
    if (series && i >= 0) return { list: series.items, index: i }
  } catch { /* sem a série, fica a lista de onde abriu */ }
  return { list, index }
}

export const asFile = (it: LibraryItem) => ({
  name: it.name,
  path: it.path,
  isVideo: it.kind === 'video',
  isImage: it.kind === 'image',
  isDirectory: false,
  size: it.size,
})

export function errorMessage(e: unknown): string {
  const err = e as { response?: { data?: { error?: string }; status?: number }; message?: string }
  return err.response?.data?.error || err.message || 'Erro'
}

export function itemMeta(item: LibraryItem): string {
  return [
    item.episode != null ? `Ep. ${item.episode}` : '',
    formatDuration(item.durationS),
    item.maturity !== 'geral' ? MATURITY_LABEL[item.maturity] : '',
  ]
    .filter(Boolean)
    .join(' · ')
}

export function progressPct(item: LibraryItem): number {
  return item.progress && item.progress.durationS > 0 ? Math.min(100, (item.progress.positionS / item.progress.durationS) * 100) : 0
}

export function FavoriteButton({ item, onChange, className }: { item: LibraryItem; onChange?: (on: boolean) => void; className?: string }) {
  const [on, setOn] = useState(item.favorite)
  const [busy, setBusy] = useState(false)
  const invalidate = useInvalidateLibrary()
  useEffect(() => setOn(item.favorite), [item.favorite])
  const toggle = async (e: React.MouseEvent) => {
    e.stopPropagation()
    setBusy(true)
    try {
      const r = await library.favorite(item.id, !on)
      setOn(r.favorite)
      onChange?.(r.favorite)
      void invalidate()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={on ? 'Remover dos favoritos' : 'Favoritar'}
      aria-pressed={on}
      disabled={busy}
      onClick={toggle}
      className={cn('h-8 w-8', className)}
    >
      <Heart className={cn('h-4 w-4', on && 'fill-red-500 text-red-500')} />
    </Button>
  )
}

export function ItemCard({ item, onOpen, extra }: { item: LibraryItem; onOpen: () => void; extra?: React.ReactNode }) {
  const pct = progressPct(item)
  const meta = [
    item.episode != null ? `Ep. ${item.episode}` : '',
    item.maturity !== 'geral' ? MATURITY_LABEL[item.maturity] : '',
  ].filter(Boolean).join(' · ')
  return (
    <div className="group flex min-w-0 flex-col gap-2.5">
      <button type="button" className="relative block w-full overflow-hidden rounded-xl bg-muted outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={onOpen} aria-label={`Abrir ${item.title || item.name}`}>
        <Thumbnail file={asFile(item)} className="aspect-video w-full object-cover transition-transform duration-200 group-hover:scale-[1.02]" size="md" />
        {item.kind === 'video' && (
          <span className="absolute inset-0 flex items-center justify-center opacity-0 transition-opacity group-hover:opacity-100">
            <Play className="h-11 w-11 rounded-full bg-black/60 p-2.5 text-white" />
          </span>
        )}
        {item.durationS ? (
          <span className="absolute bottom-2 right-2 rounded-md bg-black/75 px-1.5 py-0.5 text-xs font-semibold tabular-nums text-white">
            {formatDuration(item.durationS)}
          </span>
        ) : null}
        {pct > 0 && (
          <span className="absolute inset-x-0 bottom-0 h-1 bg-black/40">
            <span className="block h-full bg-[#FF5A5A]" style={{ width: `${pct}%` }} />
          </span>
        )}
      </button>
      <div className="flex items-start gap-1">
        <div className="min-w-0 flex-1">
          <p className="line-clamp-2 text-[15px] font-semibold leading-snug" title={item.path}>{item.title || item.name}</p>
          {meta && <p className="truncate text-[13px] text-muted-foreground">{meta}</p>}
        </div>
        <FavoriteButton item={item} className="-mr-1.5 -mt-1 shrink-0 opacity-70 hover:opacity-100" />
        {extra}
      </div>
    </div>
  )
}

export function ItemGrid({ items, onOpen, extra }: { items: LibraryItem[]; onOpen: OpenFn; extra?: (it: LibraryItem) => React.ReactNode }) {
  if (items.length === 0) return <p className="text-sm text-muted-foreground">Nada por aqui.</p>
  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-6 sm:grid-cols-[repeat(auto-fill,minmax(220px,1fr))] sm:gap-x-5">
      {items.map((it, i) => (
        <ItemCard key={it.id} item={it} onOpen={() => onOpen(items, i)} extra={extra?.(it)} />
      ))}
    </div>
  )
}

// Dados das telas da Coleção via React Query: cache por chave (voltar a uma
// tela mostra o que já veio e revalida em segundo plano). Toda chave começa
// com LIB, para mutações invalidarem tudo de uma vez.
export const LIB = 'lib'

export function useLoad<T>(key: QueryKey, fn: () => Promise<T>) {
  const qc = useQueryClient()
  const fullKey = [LIB, ...key]
  const q = useQuery({ queryKey: fullKey, queryFn: fn, staleTime: 30_000 })
  return {
    data: q.data ?? null,
    setData: (up: (d: T | null) => T | null) => qc.setQueryData<T | null>(fullKey, (d) => up(d ?? null)),
    loading: q.isPending,
    error: q.error ? errorMessage(q.error) : null,
    reload: () => { void q.refetch() },
  }
}

export function useInvalidateLibrary() {
  const qc = useQueryClient()
  return () => qc.invalidateQueries({ queryKey: [LIB] })
}

export function Status({ loading, error }: { loading: boolean; error: string | null }) {
  if (error) return <p className="text-sm text-destructive">{error === 'Request failed with status code 503' ? 'Biblioteca indisponível no servidor.' : error}</p>
  if (loading) return <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" aria-label="Carregando" />
  return null
}
