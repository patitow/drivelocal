import { useCallback, useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Play, Plus, Trash2, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { MobileMenuButton } from '@/components/MobileMenuButton'
import { VideoPlayer } from '@/components/VideoPlayer'
import { ImageViewer } from '@/components/ImageViewer'
import {
  library,
  formatDuration,
  type LibraryItem,
  type Playlist,
} from '@/lib/library'
import { ItemGrid, Status, asFile, errorMessage, inSeries, useInvalidateLibrary, useLoad, type OpenFn } from './catalogShared'
import { HomeTab } from './HomeTab'
import { SearchTab } from './SearchTab'
import { WebImg, useArt } from './ExtMeta'
import { SHELVES, ShelfPage, useCreatorNews, useShelfCounts } from './shelves'
import { ManageTab } from './ManageTab'
import { CATALOG_MORE, isShelfTab, type CatalogTab } from './CatalogNav'
import { useNavParam } from '@/lib/nav'

export { FavoriteButton } from './catalogShared'

type Tab = CatalogTab
const isShelf = isShelfTab

interface PlayerState { list: LibraryItem[]; index: number }
// O mínimo de um tópico para abrir sua página (vem da lista ou de um card da home).
interface TopicRef { id: number; title: string; count: number; watched: number }

// Tópicos abertos de um card (a URL só leva o id): o nome aparece na hora.
const topicRefs = new Map<number, TopicRef>()

// Abre uma seção da Coleção, com o que estiver aberto nela (?topico=, ?playlist=).
export function useGoCatalog() {
  const navigate = useNavigate()
  return useCallback((tab: Tab, search: Record<string, unknown> = {}) => {
    void (tab === 'home' ? navigate({ to: '/colecao', search }) : navigate({ to: '/colecao/$tab', params: { tab }, search }))
  }, [navigate])
}

// Coleção: catálogo da biblioteca (início, busca, tópicos, playlists e revisão).
// A seção vem do endereço (/colecao/:secao) e o que está aberto nela, dos
// parâmetros (?topico=, ?serie=...), então voltar/avançar e links funcionam.
export function CatalogView({ tab }: { tab: Tab }) {
  const { role } = useAuth()
  const isAdmin = role === 'admin'
  const [player, setPlayer] = useState<PlayerState | null>(null)
  const goCatalog = useGoCatalog()

  const open = useCallback<OpenFn>((list, index) => {
    setPlayer({ list, index })
    // a fila troca pela série quando ela chega, se o player ainda estiver no mesmo episódio
    void inSeries(list, index).then((next) => {
      if (next.list !== list) setPlayer((p) => (p && p.list === list && p.index === index ? next : p))
    })
  }, [])
  const invalidate = useInvalidateLibrary()
  // ao fechar o player, progresso e "continuar" mudaram: revalida as telas
  const closePlayer = () => { setPlayer(null); void invalidate() }

  const counts = useShelfCounts()
  const creatorNews = useCreatorNews()
  const go = (id: Tab) => goCatalog(id)

  const main: Array<{ id: Tab; label: string }> = [{ id: 'home', label: 'Início' }]
  const more = CATALOG_MORE

  const current = player ? player.list[player.index] : null
  const files = useMemo(() => (player ? player.list.map(asFile) : []), [player])

  return (
    // A Coleção é sempre escura, como no design aprovado.
    <div className="dark flex h-full min-h-0 flex-col bg-background text-foreground">
      {/* Mangás é papel de ponta a ponta: o fundo cobre também a sobra abaixo do conteúdo. */}
      <div className={cn('min-h-0 min-w-0 flex-1 overflow-auto p-4 lg:px-10 lg:py-9', tab === 'mangas' && 'lg:bg-[#F4F0E6]')}>
      <div className="mb-4 flex items-center gap-2 lg:hidden">
        <MobileMenuButton />
        <h1 className="font-display text-2xl font-extrabold tracking-tight">Coleção</h1>
      </div>
      {/* Pílulas de estante (celular) */}
      <div aria-label="Estantes" className="shelf-rail -mx-4 mb-5 flex gap-2 overflow-x-auto px-4 lg:hidden">
        {[...main, ...SHELVES, ...more.filter((t) => !t.admin || isAdmin)].map((t) => (
          <button
            key={t.id}
            aria-current={tab === t.id ? 'page' : undefined}
            onClick={() => go(t.id)}
            className={cn('h-10 shrink-0 rounded-xl px-4 text-sm', tab === t.id ? 'bg-foreground font-semibold text-background' : 'bg-secondary text-foreground')}
          >
            {t.label}
            {t.id === 'criadores' && creatorNews > 0 && tab !== t.id && (
              <span className="ml-1.5 inline-block h-2 w-2 rounded-full align-middle" style={{ background: '#FF6B9A' }} aria-label="com novidades" />
            )}
          </button>
        ))}
      </div>

      {isShelf(tab) && (
        <ShelfPage
          id={tab}
          onOpen={open}
          counts={counts}
        />
      )}
      {tab === 'favorites' && <FavoritesTab onOpen={open} />}
      {tab === 'home' && (
        <HomeTab
          onOpen={open}
          onOpenTopic={(t) => (t.kind === 'creator'
            ? goCatalog('criadores', { criador: t.id })
            : t.kind === 'series'
              ? goCatalog('animes', { serie: `t${t.id}` })
              : goCatalog('topics', { topico: t.id }))}
          onOpenPlaylist={(p) => goCatalog('playlists', { playlist: p.id })}
        />
      )}
      {tab === 'search' && <SearchTab onOpen={open} />}
      {tab === 'topics' && <TopicsTab onOpen={open} />}
      {tab === 'playlists' && <PlaylistsTab onOpen={open} />}
      {tab === 'manage' && isAdmin && <ManageTab />}

      {player && current && current.kind === 'video' && (
        <VideoPlayer
          file={asFile(current)}
          files={files}
          currentIndex={player.index}
          onClose={closePlayer}
          onNavigate={(i) => setPlayer((p) => (p ? { ...p, index: i } : p))}
        />
      )}
      {player && current && current.kind !== 'video' && (
        <ImageViewer
          file={asFile(current)}
          files={files}
          currentIndex={player.index}
          onClose={closePlayer}
          onNavigate={(i) => setPlayer((p) => (p ? { ...p, index: i } : p))}
        />
      )}
      </div>
    </div>
  )
}

function FavoritesTab({ onOpen }: { onOpen: OpenFn }) {
  const res = useLoad(['favorites'], () => library.items({ favorites: true, sort: 'recent', limit: 120 }))
  return (
    <div className="space-y-6">
      <h2 className="font-display text-5xl font-extrabold leading-[0.95] tracking-tighter sm:text-7xl">Favoritos</h2>
      <Status loading={res.loading} error={res.error} />
      {res.data && <ItemGrid items={res.data.items} onOpen={onOpen} />}
    </div>
  )
}

function TopicsTab({ onOpen }: { onOpen: OpenFn }) {
  const [kind, setKind] = useState('')
  const [q, setQ] = useState('')
  const [selId, setSelId] = useNavParam('topico')
  const list = useLoad(['topics', kind, q], () => library.topics({ kind: kind || undefined, q: q || undefined, limit: 200 }))
  const items = useLoad(['topic-items', selId ?? null], () => (selId ? library.topicItems(selId) : Promise.resolve(null)))
  const setSel = (t: TopicRef | null) => { if (t) topicRefs.set(t.id, t); setSelId(t?.id) }
  // aberto por link: o nome vem da lista (ou de quem abriu)
  const sel = selId ? (list.data?.topics.find((t) => t.id === selId) ?? topicRefs.get(selId)) : undefined

  if (selId) {
    return (
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setSel(null)}>Voltar</Button>
          {sel && <h2 className="font-semibold">{sel.title}</h2>}
          {sel && <Badge variant="secondary">{sel.watched}/{sel.count} vistos</Badge>}
        </div>
        <Status loading={items.loading} error={items.error} />
        {items.data && <ItemGrid items={items.data.items} onOpen={onOpen} />}
      </div>
    )
  }
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filtrar tópicos" className="max-w-xs" aria-label="Filtrar tópicos" />
        <select className="h-9 rounded-md border bg-background px-2 text-sm" value={kind} onChange={(e) => setKind(e.target.value)} aria-label="Tipo de tópico">
          <option value="">Todos</option>
          <option value="series">Séries</option>
          <option value="creator">Criadores</option>
          <option value="collection">Coleções</option>
        </select>
      </div>
      <Status loading={list.loading} error={list.error} />
      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {list.data?.topics.map((t) => (
          <li key={t.id}>
            <button type="button" onClick={() => setSel(t)} className="flex w-full items-center gap-3 rounded-md border p-3 text-left hover:bg-accent">
              <TopicArt id={t.id} />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{t.title}</span>
                <span className="text-xs text-muted-foreground">{t.kind} · {t.count} itens</span>
              </span>
              {t.watched > 0 && <Badge variant="secondary">{t.watched}/{t.count}</Badge>}
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}

function PlaylistsTab({ onOpen }: { onOpen: OpenFn }) {
  const [name, setName] = useState('')
  const [selId, setSelId] = useNavParam('playlist')
  const list = useLoad(['playlists'], () => library.playlists())
  const detail = useLoad(['playlist', selId ?? null], () => (selId ? library.playlist(selId) : Promise.resolve(null)))
  const setSel = (p: Playlist | null) => setSelId(p?.id)
  const sel = selId ? (detail.data?.playlist ?? list.data?.find((p) => p.id === selId)) : undefined

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    try {
      await library.createPlaylist(name.trim())
      setName('')
      list.reload()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  if (sel) {
    const items = detail.data?.items ?? []
    return (
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setSel(null)}>Voltar</Button>
          <h2 className="font-semibold">{sel.name}</h2>
          {items.length > 0 && (
            <Button size="sm" onClick={() => onOpen(items, 0)}><Play className="mr-1 h-4 w-4" />Tocar tudo</Button>
          )}
        </div>
        <Status loading={detail.loading} error={detail.error} />
        {detail.data && (
          <ItemGrid
            items={items}
            onOpen={onOpen}
            extra={sel.canEdit ? (it) => (
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                aria-label="Remover da playlist"
                onClick={async () => {
                  await library.removeFromPlaylist(sel.id, it.id).catch((e) => toast.error(errorMessage(e)))
                  detail.reload()
                }}
              >
                <X className="h-4 w-4" />
              </Button>
            ) : undefined}
          />
        )}
      </div>
    )
  }
  return (
    <div className="space-y-3">
      <form onSubmit={create} className="flex gap-2">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Nova playlist" className="max-w-xs" aria-label="Nome da nova playlist" />
        <Button type="submit" size="sm"><Plus className="mr-1 h-4 w-4" />Criar</Button>
      </form>
      <Status loading={list.loading} error={list.error} />
      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {list.data?.map((p) => (
          <li key={p.id} className="flex items-center gap-2 rounded-md border p-3">
            <button type="button" className="min-w-0 flex-1 text-left" onClick={() => setSel(p)}>
              <span className="block truncate font-medium">{p.name}</span>
              <span className="text-xs text-muted-foreground">
                {p.itemCount} itens{p.durationS ? ` · ${formatDuration(p.durationS)}` : ''}{p.ownerName ? ` · ${p.ownerName}` : ''}
              </span>
            </button>
            {p.canEdit && (
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Excluir ${p.name}`}
                onClick={async () => {
                  if (!window.confirm(`Excluir a playlist "${p.name}"?`)) return
                  await library.deletePlaylist(p.id).catch((e) => toast.error(errorMessage(e)))
                  list.reload()
                }}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}


// Capa/banner escolhidos no Gerenciar, também na lista de tópicos.
function TopicArt({ id }: { id: number }) {
  const art = useArt(`topic:${id}`)
  const src = art?.cover || art?.banner
  if (!src) return null
  return <WebImg src={src} className={cn('h-12 shrink-0 rounded object-cover', art?.cover ? 'w-8' : 'w-20')} />
}
