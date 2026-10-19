// Itens da Coleção na barra lateral única do app (início, estantes e seções).
import { Search, ListVideo, Tags, Home, ShieldCheck, SlidersHorizontal, Heart } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SHELVES, useCreatorNews, useShelfCounts, type ShelfId } from './shelves/shelfUi'

export type CatalogTab = 'home' | 'search' | 'topics' | 'playlists' | 'review' | 'manage' | 'favorites' | ShelfId

export const isShelfTab = (t: CatalogTab): t is ShelfId => SHELVES.some((s) => s.id === t)

export const CATALOG_MORE: Array<{ id: CatalogTab; label: string; icon: typeof Home; admin?: boolean }> = [
  { id: 'search', label: 'Busca', icon: Search },
  { id: 'topics', label: 'Tópicos', icon: Tags },
  { id: 'playlists', label: 'Playlists', icon: ListVideo },
  { id: 'favorites', label: 'Favoritos', icon: Heart },
  { id: 'review', label: 'Revisão', icon: ShieldCheck, admin: true },
  { id: 'manage', label: 'Gerenciar', icon: SlidersHorizontal, admin: true },
]

interface Props {
  // aba ativa, ou null quando a tela atual não é a Coleção
  active: CatalogTab | null
  onPick: (t: CatalogTab) => void
  showLabels: boolean
  isAdmin: boolean
}

export function CatalogNav({ active, onPick, showLabels, isAdmin }: Props) {
  const counts = useShelfCounts()
  const creatorNews = useCreatorNews()
  const item = (on: boolean) => cn(
    'flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-left text-sm transition-colors',
    showLabels ? 'justify-start' : 'justify-center px-0',
    on ? 'bg-secondary font-semibold text-foreground' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
  )
  return (
    <div role="tablist" aria-label="Coleção" aria-orientation="vertical" className="space-y-1">
      {showLabels && <div className="px-3 pb-1 pt-2 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">Coleção</div>}
      <button role="tab" aria-selected={active === 'home'} className={item(active === 'home')} onClick={() => onPick('home')} title={showLabels ? undefined : 'Início'}>
        <Home className="h-5 w-5 shrink-0" />
        {showLabels && <span className="flex-1">Início</span>}
      </button>
      {SHELVES.map((s) => (
        <button key={s.id} role="tab" aria-selected={active === s.id} className={item(active === s.id)} onClick={() => onPick(s.id)} title={showLabels ? undefined : s.label}>
          <span className="relative flex h-5 w-5 shrink-0 items-center justify-center">
            <span className="h-2.5 w-2.5 rounded-full" style={{ background: s.color }} />
            {!showLabels && s.id === 'criadores' && creatorNews > 0 && active !== s.id && (
              <span className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-[#FF6B9A]" aria-label="com novidades" />
            )}
          </span>
          {showLabels && <span className="flex-1">{s.label}</span>}
          {showLabels && s.id === 'criadores' && creatorNews > 0 && active !== s.id && (
            <span className="rounded-full px-1.5 text-[11px] font-bold" style={{ background: s.color, color: s.ink }}
              title={`${creatorNews} ${creatorNews === 1 ? 'criador' : 'criadores'} com novidades`} data-testid="creator-news">{creatorNews} {creatorNews === 1 ? 'novo' : 'novos'}</span>
          )}
          {showLabels && counts[s.id] != null && <span className="text-xs text-muted-foreground">{counts[s.id]}</span>}
        </button>
      ))}
      <div className="mx-3 my-2 h-px bg-border" />
      {CATALOG_MORE.filter((t) => !t.admin || isAdmin).map((t) => (
        <button key={t.id} role="tab" aria-selected={active === t.id} className={item(active === t.id)} onClick={() => onPick(t.id)} title={showLabels ? undefined : t.label}>
          <t.icon className="h-5 w-5 shrink-0" />
          {showLabels && <span className="flex-1">{t.label}</span>}
        </button>
      ))}
    </div>
  )
}
