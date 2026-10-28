// Cliente da biblioteca (catálogo, favoritos, progresso, playlists e admin).
// Tipos espelham os DTOs de server-go/library_*.go.
import { api } from '@/lib/api'

export type Maturity = 'geral' | 'sugestivo' | 'explicito'
export const MATURITY_LABEL: Record<Maturity, string> = {
  geral: 'Geral',
  sugestivo: 'Sugestivo (16+)',
  explicito: 'Explícito',
}

export interface TagRef { id: number; slug: string; label: string; facet: string; source?: string }
export interface TopicMini { id: number; kind: string; title: string }
export interface Progress {
  positionS: number
  durationS: number
  completed: boolean
  audioTrack?: string
  subtitleTrack?: string
  updatedAt: number
}
export interface LibraryItem {
  id: number
  // parte da série pela pasta ("2009", "2011 · OVAs"), quando a série tem várias
  part?: string
  path: string
  name: string
  title: string
  kind: 'video' | 'image' | string
  root: string
  size: number
  mtime: number
  durationS?: number
  season?: number
  episode?: number
  category?: string
  maturity: Maturity
  missing?: boolean
  // motivo quando o ffprobe não abre o arquivo (corrompido/incompleto)
  broken?: string
  favorite: boolean
  hidden?: boolean
  progress?: Progress
  tags: TagRef[]
  topics: TopicMini[]
}
export interface ItemPage { items: LibraryItem[]; total: number; limit: number; offset: number }
export interface ItemQuery {
  q?: string
  kind?: string
  root?: string
  pathPrefix?: string
  tags?: string[]
  topicId?: number
  maturity?: string
  category?: string
  favorites?: boolean
  unwatched?: boolean
  inProgress?: boolean
  since?: number // mtime mínimo (unix s)
  minDurationS?: number
  maxDurationS?: number
  sort?: 'relevance' | 'recent' | 'title' | 'size' | 'episode' | 'random' | 'added'
  limit?: number
  offset?: number
}
export interface TagInfo extends TagRef { status: string; count: number; aliases: string[] }
export interface TopicInfo {
  id: number
  kind: string
  slug: string
  title: string
  description: string
  source: string
  count: number
  watched: number
  favorite: boolean
  coverPath?: string
  aliases: string[]
  recent?: string[] // até 3 itens mais novos
  latest?: number // mtime do item mais novo
  maturity?: Maturity // a maior entre capa e recentes
}
export interface MangaChapter { dir: string; title: string; pages: number; cover: string }
export interface MangaWork { dir: string; title: string; author?: string; cover: string; pages: number; mtime: number; chapters: MangaChapter[] }
// restrictedMode: o próprio usuário liga/desliga o explícito; só existe se restrictedAllowed.
export interface Prefs { maxMaturity: Maturity; autoResume: boolean; autoplay: boolean; restrictedMode?: boolean; restrictedAllowed?: boolean }
export interface Playlist {
  id: number
  ownerId: string
  ownerName?: string
  name: string
  description: string
  visibility: 'private' | 'shared' | 'global'
  smart?: ItemQuery
  itemCount: number
  durationS: number
  canEdit: boolean
  createdAt: number
  updatedAt: number
}
export interface SeriesGroup {
  key: string
  topicId?: number
  folder?: string
  title: string
  coverPath: string
  matched: number
  total: number
  year?: number
  maturity: Maturity
  next?: LibraryItem
  best: LibraryItem
  items: LibraryItem[]
  watched: number
}
export interface SearchGroup { type: 'series' | 'item'; item?: LibraryItem; series?: SeriesGroup }
export interface GroupedSearch { total: number; groups: SearchGroup[]; truncated: boolean }
export interface TopicCard { id: number; kind: string; title: string; count: number; watched: number; coverPath: string; maturity?: Maturity }
export interface PlaylistCard extends Playlist { coverPath?: string }
export interface HomeShelf {
  id: string
  title: string
  kind: 'items' | 'topics' | 'playlists'
  items?: LibraryItem[]
  topics?: TopicCard[]
  playlists?: PlaylistCard[]
  actions?: string[]
  more?: Record<string, string>
}
export interface HomeData { shelves: HomeShelf[]; generatedAt: number }
// "Para você": recomendação paginada; a mesma semente mantém a ordem entre páginas.
export type FeedScope = 'all' | 'series' | 'creators' | 'films'
export interface FeedItem extends LibraryItem {
  reason: string
  reasonKind: 'favorite' | 'next' | 'affinity' | 'new' | 'discover'
  reasonTopic?: TopicMini
}
export interface FeedPage { items: FeedItem[]; next: number }
export interface TopicDupMember { id: number; title: string; count: number; score: number; examples: string[] }
export interface TopicDupGroup { kind: string; canonical: TopicDupMember; members: TopicDupMember[]; score: number }
export interface TopicMergeBatch { batchId: string; into: string; merged: string[]; actor: string; createdAt: number; undone: boolean }
// tracks: última faixa de áudio ("idioma|título") e legenda (idioma ou 'off') usada na série
export interface PathProgress { mediaId: number; progress?: Progress | null; favorite: boolean; prefs: Prefs; tracks?: { audio?: string; sub?: string } }
export interface TrackChoice { audioTrack?: string; subtitleTrack?: string }

// Monta a query string no formato que o servidor lê (tags separadas por vírgula).
export function itemQueryParams(q: ItemQuery): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === null || v === '' || v === false) continue
    if (Array.isArray(v)) {
      if (v.length) out[k] = v.join(',')
    } else {
      out[k] = String(v)
    }
  }
  return out
}

async function call<T>(method: string, url: string, data?: unknown, params?: Record<string, string>): Promise<T> {
  const res = await api.request<T>({ method, url, data, params })
  return res.data
}

// Segunda barreira do conteúdo explícito: o servidor já filtra, mas uma resposta velha (cache, corrida com
// o switch) não pode mostrá-lo com o modo desligado. Sem saber o modo, esconde.
let restrictedGate: Promise<boolean> | null = null
const restrictedOf = (p: Prefs) => !!p.restrictedAllowed && !!p.restrictedMode
function restrictedOn(): Promise<boolean> {
  restrictedGate ??= call<Prefs>('GET', '/api/me/prefs').then(restrictedOf, () => {
    restrictedGate = null
    return false
  })
  return restrictedGate
}

// tira de toda lista, em qualquer profundidade, o que vier marcado como explícito
export function dropMature<T>(data: T): T {
  if (Array.isArray(data)) {
    return data
      .filter((v) => !(v && typeof v === 'object' && (v as { maturity?: unknown }).maturity === 'explicito'))
      .map(dropMature) as T
  }
  if (data && typeof data === 'object') {
    return Object.fromEntries(Object.entries(data).map(([k, v]) => [k, dropMature(v)])) as T
  }
  return data
}

async function view<T>(req: Promise<T>): Promise<T> {
  const [data, on] = await Promise.all([req, restrictedOn()])
  return on ? data : dropMature(data)
}

export type MetaSource = 'anilist' | 'mal' | 'kitsu' | 'imdb' | 'tmdb' | 'tvmaze' | 'wikipedia'
export type MetaKind = 'anime' | 'manga' | 'movie' | 'series' | 'person'
// Cadastro: entradas de cada estante e a ficha curada (vence a busca automática).
export interface EntryArt { cover?: string; banner?: string }
export type EntryCat = 'animes' | 'films' | 'franchises' | 'series' | 'mangas' | 'creators'
// franquia cadastrada: os filmes na ordem escolhida
export interface FranchiseInfo { id: number; title: string; items: number[] }
export interface EntryData extends Partial<Omit<ExtMeta, 'source'>> { source?: ExtMeta['source']; cat?: string; registered?: boolean }
export interface Entry {
  ref: string
  cat: EntryCat
  title: string
  count: number
  coverPath?: string
  topicId?: number
  mediaId?: number
  dir?: string
  aliases: string[]
  maturity: Maturity
  meta?: EntryData
}
export interface EntryInput {
  cat?: EntryCat
  ref?: string
  title?: string
  aliases?: string[]
  dir?: string
  addIds?: number[]
  removeIds?: number[]
  order?: number[] // agrupamento: os itens na ordem escolhida
  maturity?: Maturity | 'auto'
  meta?: EntryData
  clearMeta?: boolean
}

export interface MangaReadState { chapter: string; page: number; pages: number; done: string[]; at: number }
export interface CreatorNews { seenAt: number; since: number; count: number; favorites: number; ids: number[] }
export interface ExtMeta {
  source: MetaSource | 'manual'
  url?: string
  title: string
  year?: number
  synopsis?: string
  cover?: string
  banner?: string
  genres: string[]
  score?: number
  episodes?: number
}

export const library = {
  items: (q: ItemQuery) => view(call<ItemPage>('GET', '/api/catalog/items', undefined, itemQueryParams(q))),
  item: (id: number) => call<LibraryItem>('GET', `/api/catalog/items/${id}`),
  lookup: (path: string) => call<LibraryItem>('GET', '/api/catalog/lookup', undefined, { path }),
  setTags: (id: number, add: string[], remove: string[] = []) =>
    call<{ item: LibraryItem }>('POST', `/api/catalog/items/${id}/tags`, { add, remove }),
  tags: (q = '') => call<{ tags: TagInfo[] }>('GET', '/api/tags', undefined, q ? { q } : undefined).then((r) => r.tags ?? []),
  topics: (params: { kind?: string; q?: string; favorites?: boolean; limit?: number } = {}) =>
    view(call<{ topics: TopicInfo[]; total: number }>('GET', '/api/topics', undefined, itemQueryParams(params as ItemQuery))),
  topicItems: (id: number) => view(call<ItemPage>('GET', `/api/topics/${id}/items`, undefined, { limit: '500' })),
  favorite: (id: number, on: boolean) => call<{ favorite: boolean }>(on ? 'PUT' : 'DELETE', `/api/me/favorites/${id}`),
  // quem não é admin: sugere juntar (into) ou tirar dos criadores; vai para a Revisão
  suggestTopic: (id: number, action: 'merge' | 'invalid', into?: number) =>
    call<unknown>('POST', `/api/topics/${id}/suggest`, { action, into }),
  favoriteTopic: (id: number, on: boolean) => call<{ favorite: boolean }>(on ? 'PUT' : 'DELETE', `/api/me/favorite-topics/${id}`),
  hide: (id: number, on: boolean) => call<{ hidden: boolean }>(on ? 'PUT' : 'DELETE', `/api/me/hidden/${id}`),
  search: (q: ItemQuery) =>
    view(call<GroupedSearch>('GET', '/api/catalog/search', undefined, { ...itemQueryParams(q), group: 'series' })),
  home: () =>
    view(call<HomeData>('GET', '/api/catalog/home')).then((d) => ({
      ...d,
      // estante que ficou vazia pelo filtro sai junto
      shelves: d.shelves.filter((s) => s.kind === 'playlists' || (s.kind === 'items' ? s.items : s.topics)?.length !== 0),
    })),
  tvSeries: () => view(call<{ series: TopicCard[] }>('GET', '/api/catalog/tv-series')).then((r) => r.series),
  franchises: () => call<{ franchises: FranchiseInfo[] }>('GET', '/api/catalog/franchises').then((r) => r.franchises ?? []),
  feed: (scope: FeedScope, seed: number, offset: number, limit = 24) =>
    view(call<FeedPage>('GET', '/api/catalog/feed', undefined, { scope, seed: String(seed), offset: String(offset), limit: String(limit) })),
  mangas: () => call<{ works: MangaWork[] }>('GET', '/api/catalog/mangas').then((r) => r.works ?? []),
  // Páginas de um capítulo, na ordem de leitura.
  mangaPages: (dir: string) =>
    call<ItemPage>('GET', '/api/catalog/items', undefined, itemQueryParams({ pathPrefix: dir, kind: 'image', sort: 'episode', limit: 500 }))
      .then((p) => p.items.filter((it) => it.path.slice(0, it.path.lastIndexOf('/')) === dir)),
  // Tira do "continuar assistindo" sem apagar progresso; volta com progresso novo.
  dismissContinue: (id: number) => call<unknown>('DELETE', `/api/me/continue/${id}`),
  clearContinue: () => call<{ dismissed: number }>('DELETE', '/api/me/continue'),
  continueWatching: () => view(call<{ items: LibraryItem[] }>('GET', '/api/me/continue')).then((r) => r.items ?? []),
  prefs: () => call<Prefs>('GET', '/api/me/prefs').then((p) => {
    restrictedGate = Promise.resolve(restrictedOf(p))
    return p
  }),
  savePrefs: (p: Pick<Prefs, 'autoResume' | 'autoplay'>) => call<Prefs>('PUT', '/api/me/prefs', p),
  mangaProgress: () => call<Record<string, MangaReadState>>('GET', '/api/me/manga-progress'),
  saveMangaProgress: (p: Record<string, MangaReadState>) => call<Record<string, MangaReadState>>('PUT', '/api/me/manga-progress', p),
  creatorNews: () => call<CreatorNews>('GET', '/api/me/creator-news'),
  // marca a estante como vista; devolve o estado de antes
  markCreatorsSeen: () => call<CreatorNews>('POST', '/api/me/creator-news'),
  // ref: ficha curada no cadastro (topic:<id>, media:<id>, manga:<pasta>)
  meta: (kind: 'anime' | 'manga' | 'movie' | null, q: string, ref?: string) =>
    call<{ meta: ExtMeta | null }>('GET', '/api/meta', undefined, { q, ...(kind ? { kind } : {}), ...(ref ? { ref } : {}) }).then((r) => r.meta),
  // capa e banner das fichas do cadastro, por ref (para os cards)
  art: () => call<{ art: Record<string, EntryArt> }>('GET', '/api/meta/art').then((r) => r.art ?? {}),
  setRestrictedMode: (on: boolean) => call<Prefs>('PUT', '/api/me/prefs', { restrictedMode: on }).then((p) => {
    restrictedGate = Promise.resolve(restrictedOf(p))
    return p
  }),
  progressByPath: (path: string) => call<PathProgress>('GET', '/api/me/progress', undefined, { path }),
  saveProgressByPath: (path: string, positionS: number, durationS: number, completed?: boolean, tracks?: TrackChoice) =>
    call<unknown>('PUT', '/api/me/progress', { positionS, durationS, completed, ...tracks }, { path }),
  clearProgress: (id: number) => call<unknown>('DELETE', `/api/me/progress/${id}`),
  // Marca (no fim, concluído) ou desmarca (sem progresso) vários de uma vez, como uma temporada.
  setWatched: (ids: number[], watched: boolean) => call<{ updated: number }>('POST', '/api/me/watched', { ids, watched }),
  playlists: () => call<{ playlists: Playlist[] }>('GET', '/api/playlists').then((r) => r.playlists ?? []),
  playlist: (id: number) => view(call<{ playlist: Playlist; items: LibraryItem[] }>('GET', `/api/playlists/${id}`)),
  createPlaylist: (name: string, visibility: Playlist['visibility'] = 'private') =>
    call<Playlist>('POST', '/api/playlists', { name, visibility }),
  deletePlaylist: (id: number) => call<unknown>('DELETE', `/api/playlists/${id}`),
  addToPlaylist: (id: number, ids: number[]) => call<{ added: number }>('POST', `/api/playlists/${id}/items`, { ids }),
  removeFromPlaylist: (id: number, mediaId: number) => call<unknown>('DELETE', `/api/playlists/${id}/items/${mediaId}`),
  admin: {
    stats: () => call<Record<string, unknown>>('GET', '/api/admin/library/stats'),
    scan: () => call<unknown>('POST', '/api/admin/library/scan'),
    aiStatus: () => call<Record<string, unknown>>('GET', '/api/admin/library/ai/status'),
    aiPause: (paused: boolean) => call<unknown>('POST', '/api/admin/library/ai/pause', { paused }),
    review: (limit = 50) =>
      call<{ suggestions: Suggestion[]; total: number }>('GET', '/api/admin/library/review', undefined, { status: 'pending', limit: String(limit) }),
    decide: (ids: number[], accept: boolean) => call<unknown>('POST', '/api/admin/library/review', { ids, accept }),
    topicDupes: () => call<{ groups: TopicDupGroup[]; threshold: number }>('GET', '/api/admin/library/topic-dupes'),
    scanTopicDupes: () => call<{ topics: number; pairs: number; suggested: number }>('POST', '/api/admin/library/topic-dupes/scan'),
    acceptTopicDupes: (canonicalId: number, memberIds: number[]) =>
      call<{ batchId: string; merged: number }>('POST', '/api/admin/library/topic-dupes/accept', { canonicalId, memberIds }),
    rejectTopicDupes: (ids: number[]) => call<{ rejected: number }>('POST', '/api/admin/library/topic-dupes/reject', { ids }),
    topicMerges: () => call<{ merges: TopicMergeBatch[] }>('GET', '/api/admin/library/topic-merges').then((r) => r.merges ?? []),
    undoTopicMerge: (batchId: string) => call<{ restored: number }>('POST', `/api/admin/library/topic-merges/${encodeURIComponent(batchId)}/undo`),
    dissolveTopic: (id: number) => call<TopicBlock>('POST', `/api/admin/library/topics/${id}/dissolve`),
    topicBlocks: () => call<{ blocks: TopicBlock[] }>('GET', '/api/admin/library/topic-blocks').then((r) => r.blocks ?? []),
    restoreTopic: (kind: string, slug: string) => call<{ id: number }>('POST', '/api/admin/library/topic-blocks/restore', { kind, slug }),
    topicFlags: () => call<{ flags: TopicFlag[] }>('GET', '/api/admin/library/topic-flags').then((r) => r.flags ?? []),
    dismissTopicFlag: (topicId: number) => call<unknown>('POST', `/api/admin/library/topic-flags/${topicId}/dismiss`),
    setItem: (id: number, actions: Record<string, unknown>) =>
      call<{ result: { batchId: string } }>('PATCH', `/api/catalog/items/${id}`, actions),
    undoChanges: (batchId: string) => call<unknown>('POST', `/api/admin/library/changes/${encodeURIComponent(batchId)}/undo`),
    // revert: desfazer um renomear (sem guardar o nome desfeito como apelido)
    renameTopic: (id: number, title: string, revert = false) => call<unknown>('PATCH', `/api/admin/library/topics/${id}`, { title, revert }),
    entries: (cat: EntryCat, q = '') =>
      call<{ entries: Entry[] }>('GET', '/api/admin/library/entries', undefined, q ? { cat, q } : { cat }).then((r) => r.entries ?? []),
    entry: (ref: string) => call<{ entry: Entry }>('GET', '/api/admin/library/entries', undefined, { ref }).then((r) => r.entry),
    createEntry: (input: EntryInput) => call<{ ref: string }>('POST', '/api/admin/library/entries', input),
    updateEntry: (input: EntryInput) => call<{ ref: string }>('PUT', '/api/admin/library/entries', input),
    // guarda uma imagem no acervo local (de um link ou de um item) e devolve o endereço
    storeArt: (src: { url?: string; mediaId?: number; at?: number; crop?: { x: number; y: number; w: number; h: number } }) => call<{ ref: string; url: string }>('POST', '/api/admin/library/art', src),
    uploadArt: (file: File) => {
      const fd = new FormData()
      fd.append('file', file)
      return api.request<{ ref: string; url: string }>({ method: 'POST', url: '/api/admin/library/art', data: fd, headers: { 'Content-Type': 'multipart/form-data' } })
        .then((r) => r.data)
    },
    deleteEntry: (ref: string) => call<unknown>('DELETE', '/api/admin/library/entries', undefined, { ref }),
    imageFolders: (q: string) =>
      call<{ folders: { dir: string; pages: number }[] }>('GET', '/api/admin/library/image-folders', undefined, { q }).then((r) => r.folders ?? []),
    searchMeta: (source: MetaSource, kind: MetaKind, q: string) =>
      call<{ results: ExtMeta[] }>('GET', '/api/admin/library/meta/search', undefined, { source, kind, q }).then((r) => r.results ?? []),
  },
}

// Tópico extinto ("não é um criador"): não volta no scan; dá para desfazer.
export interface TopicBlock { kind: string; slug: string; title: string; count: number; actor?: string; createdAt: number }
// Sugestão de usuário: este tópico não é válido.
export interface TopicFlag { topicId: number; kind: string; title: string; count: number; by: string[] }

export interface Suggestion {
  id: number
  mediaId: number
  path: string
  title: string
  maturity: Maturity
  kind: string
  value: unknown
  confidence?: number
  source: string
  model?: string
  status: string
  createdAt: number
}

// ---- Regras do player (puras, testadas) ----

// Abaixo disso não vale a pena retomar; perto do fim, recomeça do zero.
export const RESUME_MIN_S = 15
export const RESUME_END_FRAC = 0.95

// Posição de onde retomar, ou null para começar do início.
export function resumePoint(p: Progress | null | undefined, durationS = 0): number | null {
  if (!p || p.completed) return null
  const pos = p.positionS
  if (!Number.isFinite(pos) || pos < RESUME_MIN_S) return null
  const dur = durationS > 0 ? durationS : p.durationS
  if (dur > 0 && pos >= dur * RESUME_END_FRAC) return null
  return pos
}

// Próximo índice tocável para o autoplay (pula imagens), ou null.
export function nextPlayableIndex(files: Array<{ isVideo?: boolean; isImage?: boolean; name: string }>, current: number): number | null {
  for (let i = current + 1; i < files.length; i++) {
    const f = files[i]
    if (f.isVideo || (!f.isImage && /\.(mp4|mkv|webm|avi|mov|m4v|wmv|flv|ts|m2ts)$/i.test(f.name))) return i
  }
  return null
}

// Grava só quando andou o suficiente desde a última gravação (ou sempre, se forçado).
export function shouldSaveProgress(last: number | null, now: number, force = false): boolean {
  if (force) return true
  if (last === null) return now >= RESUME_MIN_S
  return Math.abs(now - last) >= 10
}

export function formatDuration(s?: number): string {
  if (!s || !Number.isFinite(s)) return ''
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}
