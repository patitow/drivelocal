// Gerenciar (admin): cadastro das entradas de cada estante — criar, editar
// título, apelidos, maturidade, itens e a ficha (sinopse, ano, capa...),
// que pode ser preenchida com dados de bancos públicos (AniList, MAL, IMDb...).
import { lazy, Suspense, useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useRouter } from '@tanstack/react-router'
import { toast } from 'sonner'
import { ArrowDown, ArrowLeft, ArrowUp, Check, Download, Loader2, Merge, Plus, Search, Split, Trash2, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import {
  library, type Entry, type EntryCat, type EntryData, type EntryInput, type ExtMeta, type LibraryItem, type Maturity, type MetaKind, type MetaSource,
} from '@/lib/library'
import { LIB, Status, errorMessage, useLoad } from './catalogShared'
import { SOURCE, WebImg } from './ExtMeta'
import { ArtPicker } from './ArtPicker'
import { ManageReview } from './ManageReview'
import { useNavParam } from '@/lib/nav'

const LibraryManagement = lazy(() => import('@/components/LibraryManagement').then((m) => ({ default: m.LibraryManagement })))
import { Still } from './shelves/shelfUi'

const CATS: Array<{ id: EntryCat; label: string; one: string; kind: MetaKind; sources: MetaSource[] }> = [
  { id: 'animes', label: 'Animes', one: 'anime', kind: 'anime', sources: ['anilist', 'mal', 'kitsu', 'wikipedia'] },
  { id: 'series', label: 'Séries', one: 'série', kind: 'series', sources: ['tvmaze', 'imdb', 'tmdb', 'wikipedia'] },
  { id: 'films', label: 'Filmes', one: 'filme', kind: 'movie', sources: ['imdb', 'tmdb', 'anilist', 'wikipedia'] },
  { id: 'franchises', label: 'Franquias', one: 'franquia', kind: 'movie', sources: ['tmdb', 'imdb', 'wikipedia'] },
  { id: 'mangas', label: 'Mangás', one: 'mangá', kind: 'manga', sources: ['anilist', 'mal', 'kitsu', 'wikipedia'] },
  { id: 'creators', label: 'Criadores', one: 'criador', kind: 'person', sources: ['wikipedia'] },
]
const catOf = (id: EntryCat) => CATS.find((c) => c.id === id)!

const MATURITY: Array<{ id: Maturity; label: string }> = [
  { id: 'geral', label: 'Livre' }, { id: 'sugestivo', label: 'Sugestivo' }, { id: 'explicito', label: 'Explícito' },
]
const MAT_COLOR: Record<Maturity, string> = { geral: 'bg-emerald-500/15 text-emerald-300', sugestivo: 'bg-amber-500/15 text-amber-300', explicito: 'bg-red-500/15 text-red-300' }

const UNDO_MS = 10_000
const field = 'flex flex-col gap-1.5 text-sm'
const area = 'min-h-[110px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'
const list = (s: string) => s.split(',').map((x) => x.trim()).filter(Boolean)

function useDebounced<T>(v: T, ms = 300): T {
  const [d, setD] = useState(v)
  useEffect(() => {
    const t = setTimeout(() => setD(v), ms)
    return () => clearTimeout(t)
  }, [v, ms])
  return d
}

const SECTIONS = [
  { id: 'cadastro', label: 'Cadastro', hint: 'Animes, séries, filmes, franquias, mangás e criadores: título, itens, ficha, capa e banner. Vale para todo mundo.' },
  { id: 'revisao', label: 'Revisão', hint: 'Estado do catálogo, tópicos duplicados e sugestões da IA e dos usuários.' },
  { id: 'bibliotecas', label: 'Bibliotecas', hint: 'Pastas da coleção, montagens e regras de maturidade por pasta.' },
] as const

// Tudo de administração da Coleção num lugar só, em seções (?secao=).
export function ManageTab() {
  const [secao, setSecao] = useNavParam('secao')
  const [editar] = useNavParam('editar')
  const cur = SECTIONS.find((x) => x.id === secao) ?? SECTIONS[0]
  if (editar && cur.id === 'cadastro') return <ManageEntries />
  return (
    <div className="space-y-5">
      <div>
        <h2 className="font-display text-3xl font-extrabold tracking-tight">Gerenciar</h2>
        <p className="text-sm text-muted-foreground">{cur.hint}</p>
      </div>
      <div role="tablist" aria-label="Seções do Gerenciar" className="flex gap-6 border-b">
        {SECTIONS.map((x) => (
          <button key={x.id} type="button" role="tab" aria-selected={cur.id === x.id} onClick={() => setSecao(x.id === 'cadastro' ? null : x.id)}
            className={cn('h-11 border-b-[3px] text-[15px]', cur.id === x.id ? 'border-foreground font-semibold' : 'border-transparent text-muted-foreground hover:text-foreground')}>
            {x.label}
          </button>
        ))}
      </div>
      {cur.id === 'cadastro' && <ManageEntries />}
      {cur.id === 'revisao' && <ManageReview />}
      {cur.id === 'bibliotecas' && <Suspense fallback={<Loader2 className="h-5 w-5 animate-spin" />}><LibraryManagement /></Suspense>}
    </div>
  )
}

// Cadastro: lista por estante; a entrada em edição fica no endereço (?editar=),
// então os botões "Editar" das páginas abrem direto nela e o voltar funciona.
function ManageEntries() {
  const [cat, setCat] = useState<EntryCat>('animes')
  const [q, setQ] = useState('')
  const dq = useDebounced(q)
  const [editar, setEditar] = useNavParam('editar')
  const [volta] = useNavParam('volta')
  const router = useRouter()
  const res = useLoad(['admin', 'entries', cat, dq], () => library.admin.entries(cat, dq))
  const isNew = !!editar?.startsWith('novo:')
  const target = useLoad(['admin', 'entry', editar ?? ''], () => (editar && !isNew ? library.admin.entry(editar) : Promise.resolve(null)))
  const c = catOf(cat)

  if (editar) {
    const done = () => {
      res.reload()
      if (volta) router.history.back()
      else setEditar(null)
    }
    if (isNew) {
      const nc = CATS.find((x) => x.id === editar.slice(5))?.id ?? cat
      return <EntryEditor key={editar} cat={nc} entry={null} onDone={done} />
    }
    if (!target.data) {
      return (
        <div className="space-y-3">
          <Button variant="ghost" onClick={done}><ArrowLeft className="mr-1 h-4 w-4" />Gerenciar</Button>
          <Status loading={target.loading} error={target.error} />
        </div>
      )
    }
    return <EntryEditor key={editar} cat={target.data.cat} entry={target.data} onDone={done} />
  }
  const entries = res.data ?? []
  return (
    <div className="space-y-5">
      <div role="tablist" aria-label="Categoria" className="flex flex-wrap gap-2">
        {CATS.map((t) => (
          <button key={t.id} role="tab" aria-selected={cat === t.id} onClick={() => { setCat(t.id); setQ('') }}
            className={cn('h-10 rounded-xl px-4 text-sm', cat === t.id ? 'bg-foreground font-semibold text-background' : 'bg-secondary text-foreground')}>
            {t.label}
          </button>
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[220px] flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={`Buscar em ${c.label.toLowerCase()}`} aria-label="Buscar entradas" className="pl-9" />
        </div>
        <Button onClick={() => setEditar(`novo:${cat}`)}><Plus className="mr-1 h-4 w-4" />Novo {c.one}</Button>
      </div>
      <Status loading={res.loading} error={res.error} />
      {!res.loading && entries.length === 0 && <p className="text-sm text-muted-foreground">Nenhuma entrada{dq ? ' com esse nome' : ''}.</p>}
      <ul aria-label="Entradas" className="divide-y overflow-hidden rounded-xl border">
        {entries.map((e) => (
          <li key={e.ref}>
            <button type="button" onClick={() => setEditar(e.ref)} aria-label={`Editar ${e.title}`}
              className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-secondary/60">
              <span className="h-12 w-20 shrink-0 overflow-hidden rounded-md bg-muted">
                {e.meta?.banner || e.meta?.cover ? <WebImg src={e.meta.banner || e.meta.cover} className="h-full w-full object-cover" />
                  : e.coverPath ? <Still path={e.coverPath} className="h-full w-full object-cover" /> : null}
              </span>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate font-medium">{e.title}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {[e.count ? `${e.count} ${e.count === 1 ? 'item' : 'itens'}` : '', e.meta?.year ? String(e.meta.year) : '',
                    e.meta?.synopsis || e.meta?.cover ? `ficha: ${SOURCE[e.meta.source ?? 'manual'] ?? 'manual'}` : 'sem ficha',
                    e.aliases?.length ? `também: ${e.aliases.join(', ')}` : ''].filter(Boolean).join(' · ')}
                </span>
              </span>
              <span className={cn('shrink-0 rounded-full px-2 py-0.5 text-xs', MAT_COLOR[e.maturity] ?? MAT_COLOR.geral)}>
                {MATURITY.find((m) => m.id === e.maturity)?.label ?? e.maturity}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}

interface Form {
  title: string
  aliases: string
  maturity: Maturity | 'auto'
  synopsis: string
  year: string
  genres: string
  score: string
  episodes: string
  cover: string
  banner: string
  url: string
  source: ExtMeta['source']
}

const formOf = (e: Entry | null): Form => ({
  title: e?.title ?? '',
  aliases: (e?.aliases ?? []).join(', '),
  maturity: 'auto',
  synopsis: e?.meta?.synopsis ?? '',
  year: e?.meta?.year ? String(e.meta.year) : '',
  genres: (e?.meta?.genres ?? []).join(', '),
  score: e?.meta?.score ? String(e.meta.score) : '',
  episodes: e?.meta?.episodes ? String(e.meta.episodes) : '',
  cover: e?.meta?.cover ?? '',
  banner: e?.meta?.banner ?? '',
  url: e?.meta?.url ?? '',
  source: e?.meta?.source ?? 'manual',
})

function metaOf(f: Form): EntryData {
  const n = (s: string) => (s.trim() && Number.isFinite(Number(s)) ? Math.round(Number(s)) : undefined)
  return {
    source: f.source, title: f.title.trim(), synopsis: f.synopsis.trim() || undefined, year: n(f.year), genres: list(f.genres),
    score: n(f.score), episodes: n(f.episodes), cover: f.cover.trim() || undefined, banner: f.banner.trim() || undefined, url: f.url.trim() || undefined,
  }
}
const hasMeta = (m: EntryData) => !!(m.synopsis || m.year || m.genres?.length || m.score || m.episodes || m.cover || m.banner || m.url)

function EntryEditor({ cat, entry, onDone }: { cat: EntryCat; entry: Entry | null; onDone: () => void }) {
  const c = catOf(cat)
  const qc = useQueryClient()
  const [f, setF] = useState<Form>(() => formOf(entry))
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((p) => ({ ...p, [k]: v, ...(k !== 'source' && k !== 'title' && k !== 'aliases' && k !== 'maturity' ? { source: 'manual' as const } : {}) }))
  const [adds, setAdds] = useState<LibraryItem[]>([])
  const [rems, setRems] = useState<Set<number>>(new Set())
  const [dir, setDir] = useState(entry?.dir ?? '')
  const [busy, setBusy] = useState(false)
  const [confirmDel, setConfirmDel] = useState(false)
  const isTopic = !!entry?.topicId || (!entry && (cat === 'animes' || cat === 'series' || cat === 'creators' || cat === 'franchises'))
  const ordered = cat === 'franchises'
  const [order, setOrder] = useState<number[] | null>(null)
  const own = useLoad(['admin', 'entry-art-items', entry?.ref ?? '', dir], () => {
    if (entry?.topicId) return library.items({ topicId: entry.topicId, limit: 400, sort: 'episode' }).then((p) => p.items)
    if (entry?.mediaId) return library.item(entry.mediaId).then((it) => [it])
    const d = entry?.dir || dir
    if (d) return library.items({ pathPrefix: d, kind: 'image', limit: 60 }).then((p) => p.items)
    return Promise.resolve([] as LibraryItem[])
  })
  const artItems = [...adds, ...(own.data ?? []).filter((it) => !rems.has(it.id))]

  const refresh = async () => {
    await qc.invalidateQueries({ queryKey: [LIB] })
    await qc.invalidateQueries({ queryKey: ['ext-meta'] })
    await qc.invalidateQueries({ queryKey: ['entry-art'] })
  }

  const save = async () => {
    if (!f.title.trim()) { toast.error('Dê um título.'); return }
    if (!entry && cat === 'mangas' && !dir) { toast.error('Escolha a pasta do mangá.'); return }
    if (!entry && cat === 'films' && adds.length === 0) { toast.error('Escolha o vídeo do filme.'); return }
    const meta = metaOf(f)
    const input: EntryInput = {
      title: f.title.trim(),
      aliases: list(f.aliases),
      ...(f.maturity !== 'auto' ? { maturity: f.maturity } : {}),
      ...(hasMeta(meta) ? { meta } : entry?.meta && hasMeta(entry.meta) ? { clearMeta: true } : {}),
      ...(adds.length ? { addIds: adds.map((it) => it.id) } : {}),
      ...(rems.size ? { removeIds: [...rems] } : {}),
      ...(ordered && order ? { order: order.filter((id) => !rems.has(id)) } : {}),
    }
    setBusy(true)
    try {
      if (entry) await library.admin.updateEntry({ ...input, ref: entry.ref })
      else await library.admin.createEntry({ ...input, cat, ...(cat === 'mangas' ? { dir } : {}) })
      await refresh()
      toast.success(entry ? 'Alterações salvas' : `${c.one[0].toUpperCase()}${c.one.slice(1)} cadastrado`)
      onDone()
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!entry) return
    setBusy(true)
    try {
      await library.admin.deleteEntry(entry.ref)
      await refresh()
      toast.success('Entrada excluída')
      onDone()
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setBusy(false)
    }
  }

  const clearMeta = () => setF((p) => ({ ...p, synopsis: '', year: '', genres: '', score: '', episodes: '', cover: '', banner: '', url: '', source: 'manual' }))
  const apply = (m: ExtMeta) => setF((p) => ({
    ...p,
    synopsis: m.synopsis ?? p.synopsis, year: m.year ? String(m.year) : p.year, genres: m.genres?.length ? m.genres.join(', ') : p.genres,
    score: m.score ? String(m.score) : p.score, episodes: m.episodes ? String(m.episodes) : p.episodes,
    cover: m.cover ?? p.cover, banner: m.banner ?? p.banner, url: m.url ?? p.url, source: m.source,
  }))

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" onClick={onDone} aria-label="Voltar"><ArrowLeft className="mr-1 h-4 w-4" />{c.label}</Button>
        <h2 className="min-w-0 flex-1 truncate font-display text-2xl font-extrabold tracking-tight">{entry ? entry.title : `Novo ${c.one}`}</h2>
        {entry && (confirmDel
          ? <>
            <Button variant="destructive" disabled={busy} onClick={() => void remove()}>
              <Trash2 className="mr-1 h-4 w-4" />{entry.topicId ? 'Confirmar exclusão' : 'Confirmar'}
            </Button>
            <Button variant="ghost" onClick={() => setConfirmDel(false)}>Cancelar</Button>
          </>
          : <Button variant="outline" onClick={() => setConfirmDel(true)}><Trash2 className="mr-1 h-4 w-4" />Excluir</Button>)}
        <Button disabled={busy} onClick={() => void save()}>
          {busy ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Check className="mr-1 h-4 w-4" />}Salvar
        </Button>
      </div>
      {confirmDel && entry && (
        <p className="rounded-lg bg-destructive/10 px-3 py-2 text-sm">
          {entry.topicId ? 'O agrupamento e a ficha somem; os arquivos continuam na biblioteca.'
            : entry.mediaId ? 'O vídeo deixa de ser marcado como filme e a ficha some; o arquivo continua.'
              : 'A ficha some; as páginas continuam na biblioteca.'}
        </p>
      )}

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <section aria-label="Dados" className="space-y-4">
          <label className={field}>Título
            <Input value={f.title} onChange={(e) => set('title', e.target.value)} />
          </label>
          {isTopic && (
            <label className={field}>Outros nomes (separados por vírgula)
              <Input value={f.aliases} onChange={(e) => set('aliases', e.target.value)} />
            </label>
          )}
          <label className={field}>Maturidade {entry ? '(aplica a todos os itens)' : ''}
            <select value={f.maturity} onChange={(e) => set('maturity', e.target.value as Form['maturity'])}
              className="h-10 rounded-md border border-input bg-background px-3 text-sm">
              <option value="auto">{entry ? `Manter (${MATURITY.find((m) => m.id === entry.maturity)?.label ?? entry.maturity})` : 'Manter a de cada item'}</option>
              {MATURITY.map((m) => <option key={m.id} value={m.id}>{m.label}</option>)}
            </select>
          </label>

          {cat === 'mangas' && !entry && <FolderPicker dir={dir} onPick={(d) => { setDir(d); if (!f.title) set('title', d.split('/').pop() ?? d) }} />}
          {cat === 'mangas' && entry?.dir && <p className="text-sm text-muted-foreground">Pasta: <span className="text-foreground">{entry.dir}</span></p>}
          {(isTopic || (cat === 'films' && !entry)) && (
            <ItemsEditor entry={entry} single={cat === 'films'} videosOnly={ordered} order={ordered ? order : undefined} setOrder={setOrder}
              adds={adds} setAdds={setAdds} rems={rems} setRems={setRems}
              onFirst={(it) => { if (!f.title) set('title', it.title || it.name.replace(/\.[^.]+$/, '')) }} />
          )}
          {entry?.topicId && <TopicTools entry={entry} onChanged={refresh} onGone={onDone} />}
          {entry?.mediaId && <p className="text-sm text-muted-foreground">Arquivo: <span className="text-foreground">{entry.coverPath || `#${entry.mediaId}`}</span></p>}
        </section>

        <section aria-label="Ficha" className="space-y-4">
          <MetaSearch cat={cat} title={f.title} onPick={apply} />
          <div className="flex items-center justify-between">
            <h3 className="font-semibold">Ficha <span className="text-xs font-normal text-muted-foreground">(fonte: {SOURCE[f.source] ?? f.source})</span></h3>
            <Button variant="ghost" size="sm" onClick={clearMeta}><X className="mr-1 h-4 w-4" />Limpar ficha</Button>
          </div>
          <label className={field}>Sinopse
            <textarea value={f.synopsis} onChange={(e) => set('synopsis', e.target.value)} className={area} />
          </label>
          <div className="grid grid-cols-3 gap-3">
            <label className={field}>Ano<Input inputMode="numeric" value={f.year} onChange={(e) => set('year', e.target.value)} /></label>
            <label className={field}>Nota (0–100)<Input inputMode="numeric" value={f.score} onChange={(e) => set('score', e.target.value)} /></label>
            <label className={field}>{cat === 'mangas' ? 'Capítulos' : 'Episódios'}<Input inputMode="numeric" value={f.episodes} onChange={(e) => set('episodes', e.target.value)} /></label>
          </div>
          <label className={field}>Gêneros (separados por vírgula)<Input value={f.genres} onChange={(e) => set('genres', e.target.value)} /></label>
          <label className={field}>Link da ficha<Input value={f.url} onChange={(e) => set('url', e.target.value)} /></label>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
            <ArtPicker label={cat === 'creators' ? 'Foto' : 'Capa'} round={cat === 'creators'} value={f.cover} onChange={(v) => set('cover', v)} items={artItems} />
            <ArtPicker label="Banner" wide value={f.banner} onChange={(v) => set('banner', v)} items={artItems} />
          </div>
        </section>
      </div>
    </div>
  )
}

// Busca a ficha num banco público e preenche os campos com o resultado escolhido.
function MetaSearch({ cat, title, onPick }: { cat: EntryCat; title: string; onPick: (m: ExtMeta) => void }) {
  const c = catOf(cat)
  const [source, setSource] = useState<MetaSource>(c.sources[0])
  const [q, setQ] = useState(title)
  const [res, setRes] = useState<ExtMeta[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const run = async () => {
    const term = (q || title).trim()
    if (!term) return
    setBusy(true)
    setErr(null)
    try {
      setRes(await library.admin.searchMeta(source, c.kind, term))
    } catch (e) {
      setErr(errorMessage(e))
      setRes(null)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-3 rounded-xl border p-3">
      <h3 className="font-semibold">Buscar dados</h3>
      <form className="flex flex-wrap gap-2" onSubmit={(e) => { e.preventDefault(); void run() }}>
        <select value={source} onChange={(e) => setSource(e.target.value as MetaSource)} aria-label="Fonte"
          className="h-10 rounded-md border border-input bg-background px-3 text-sm">
          {c.sources.map((s) => <option key={s} value={s}>{SOURCE[s]}</option>)}
        </select>
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={title || 'Nome para buscar'} aria-label="Termo da busca" className="min-w-[160px] flex-1" />
        <Button type="submit" variant="outline" disabled={busy}>
          {busy ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Download className="mr-1 h-4 w-4" />}Buscar
        </Button>
      </form>
      {err && <p className="text-sm text-destructive">{err}</p>}
      {res && res.length === 0 && <p className="text-sm text-muted-foreground">Nada encontrado em {SOURCE[source]}.</p>}
      {res && res.length > 0 && (
        <ul aria-label="Resultados" className="max-h-[340px] space-y-1 overflow-auto">
          {res.map((m, i) => (
            <li key={`${m.url ?? m.title}-${i}`}>
              <button type="button" onClick={() => { onPick(m); setRes(null) }} aria-label={`Usar ${m.title}`}
                className="flex w-full gap-3 rounded-lg p-2 text-left hover:bg-secondary/60">
                <span className="h-16 w-11 shrink-0 overflow-hidden rounded bg-muted">
                  {m.cover && <WebImg src={m.cover} className="h-full w-full object-cover" />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{m.title}{m.year ? ` (${m.year})` : ''}</span>
                  <span className="line-clamp-2 text-xs text-muted-foreground">{m.synopsis || m.genres?.join(', ')}</span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// Itens de um agrupamento (episódios, vídeos do criador) ou o vídeo de um filme novo.
function ItemsEditor({ entry, single, videosOnly, order, setOrder, adds, setAdds, rems, setRems, onFirst }: {
  entry: Entry | null; single: boolean; videosOnly?: boolean
  // franquia: a ordem dos itens (null = a atual); sem setOrder, sem setas
  order?: number[] | null; setOrder?: (v: number[]) => void
  adds: LibraryItem[]; setAdds: (v: LibraryItem[]) => void
  rems: Set<number>; setRems: (v: Set<number>) => void
  onFirst: (it: LibraryItem) => void
}) {
  const current = useLoad(['admin', 'entry-items', entry?.topicId ?? 0], () => (entry?.topicId
    ? library.items({ topicId: entry.topicId, limit: 400, sort: 'episode' }).then((p) => p.items)
    : Promise.resolve([] as LibraryItem[])))
  const [q, setQ] = useState('')
  const dq = useDebounced(q)
  const onlyVideo = single || !!videosOnly
  const found = useLoad(['admin', 'entry-find', dq, onlyVideo], () => (dq.trim().length > 1
    ? library.items({ q: dq, ...(onlyVideo ? { kind: 'video' as const } : {}), limit: 30, sort: 'relevance' }).then((p) => p.items)
    : Promise.resolve([] as LibraryItem[])))
  const have = new Set([...(current.data ?? []).map((it) => it.id), ...adds.map((it) => it.id)])
  const add = (it: LibraryItem) => {
    if (!adds.length) onFirst(it)
    setAdds(single ? [it] : [...adds, it])
  }
  const toggleRem = (id: number) => {
    const n = new Set(rems)
    if (n.has(id)) n.delete(id)
    else n.add(id)
    setRems(n)
  }
  const base = [...(current.data ?? []), ...adds]
  const byId = new Map(base.map((it) => [it.id, it]))
  const sortable = order !== undefined && !!setOrder
  // a ordem escolhida primeiro; o que entrou depois vai para o fim
  const rows = sortable && order
    ? [...order.map((id) => byId.get(id)).filter((it): it is LibraryItem => !!it), ...base.filter((it) => !order.includes(it.id))]
    : base
  const move = (i: number, d: number) => {
    const ids = rows.map((it) => it.id)
    const j = i + d
    if (j < 0 || j >= ids.length) return
    ;[ids[i], ids[j]] = [ids[j], ids[i]]
    setOrder?.(ids)
  }
  return (
    <div className="space-y-3">
      <h3 className="font-semibold">{single ? 'Vídeo do filme' : `Itens (${rows.length - rems.size})`}</h3>
      <Status loading={current.loading} error={current.error} />
      {rows.length > 0 && (
        <ul aria-label="Itens da entrada" className="max-h-[300px] divide-y overflow-auto rounded-lg border">
          {rows.map((it, i) => {
            const isNew = adds.some((a) => a.id === it.id)
            const off = rems.has(it.id)
            return (
              <li key={it.id} className={cn('flex items-center gap-2 px-3 py-1.5 text-sm', off && 'opacity-40 line-through')}>
                {sortable && <span className="w-6 shrink-0 text-right tabular-nums text-muted-foreground">{i + 1}</span>}
                <span className="min-w-0 flex-1 truncate" title={it.path}>{!sortable && it.episode != null ? `Ep. ${it.episode} · ` : ''}{it.name}</span>
                {sortable && (
                  <>
                    <button type="button" aria-label={`Subir ${it.title || it.name}`} disabled={i === 0} onClick={() => move(i, -1)} className="rounded p-1 hover:bg-secondary disabled:opacity-30"><ArrowUp className="h-4 w-4" /></button>
                    <button type="button" aria-label={`Descer ${it.title || it.name}`} disabled={i === rows.length - 1} onClick={() => move(i, 1)} className="rounded p-1 hover:bg-secondary disabled:opacity-30"><ArrowDown className="h-4 w-4" /></button>
                  </>
                )}
                {isNew && <span className="text-xs text-emerald-400">novo</span>}
                <button type="button" aria-label={`${off ? 'Manter' : 'Tirar'} ${it.title || it.name}`} className="rounded p-1 hover:bg-secondary"
                  onClick={() => (isNew ? setAdds(adds.filter((a) => a.id !== it.id)) : toggleRem(it.id))}>
                  {off ? <Plus className="h-4 w-4" /> : <X className="h-4 w-4" />}
                </button>
              </li>
            )
          })}
        </ul>
      )}
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={onlyVideo ? 'Procurar vídeo para adicionar' : 'Procurar vídeo ou imagem para adicionar'} aria-label="Procurar item" className="pl-9" />
      </div>
      {(found.data ?? []).length > 0 && (
        <ul aria-label="Itens encontrados" className="max-h-[240px] divide-y overflow-auto rounded-lg border">
          {(found.data ?? []).filter((it) => !have.has(it.id)).map((it) => (
            <li key={it.id}>
              <button type="button" onClick={() => add(it)} className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-secondary/60">
                <Plus className="h-4 w-4 shrink-0" />
                <span className="min-w-0 flex-1 truncate" title={it.path}>{it.path}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// Pasta de imagens que vira a obra (mangás não são enviados, só cadastrados).
function FolderPicker({ dir, onPick }: { dir: string; onPick: (d: string) => void }) {
  const [q, setQ] = useState('')
  const dq = useDebounced(q)
  const res = useLoad(['admin', 'image-folders', dq], () => library.admin.imageFolders(dq))
  return (
    <div className="space-y-2">
      <h3 className="font-semibold">Pasta das páginas</h3>
      {dir && <p className="text-sm">Escolhida: <span className="font-medium">{dir}</span></p>}
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filtrar pastas com imagens" aria-label="Filtrar pastas" />
      <Status loading={res.loading} error={res.error} />
      <ul aria-label="Pastas" className="max-h-[260px] divide-y overflow-auto rounded-lg border">
        {(res.data ?? []).map((p) => (
          <li key={p.dir}>
            <button type="button" onClick={() => onPick(p.dir)}
              className={cn('flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-secondary/60', dir === p.dir && 'bg-secondary')}>
              <span className="min-w-0 flex-1 truncate" title={p.dir}>{p.dir}</span>
              <span className="shrink-0 text-xs text-muted-foreground">{p.pages} pág.</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}

// Agrupamentos (animes, séries, criadores): juntar com outra entrada da mesma
// estante ou desfazer o agrupamento — os mesmos poderes do perfil do criador,
// com o mesmo "Desfazer" por 10s.
function TopicTools({ entry, onChanged, onGone }: { entry: Entry; onChanged: () => Promise<void>; onGone: () => void }) {
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const dq = useDebounced(q)
  const [busy, setBusy] = useState(false)
  const res = useLoad(['admin', 'entries', entry.cat, dq], () => library.admin.entries(entry.cat, dq))
  const others = (res.data ?? []).filter((e) => e.topicId && e.ref !== entry.ref).slice(0, 30)
  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    try { await fn() } catch (e) { toast.error(errorMessage(e)) } finally { setBusy(false) }
  }
  const merge = (o: Entry) => run(async () => {
    const r = await library.admin.acceptTopicDupes(entry.topicId!, [o.topicId!])
    await onChanged()
    setOpen(false)
    toast.success(`"${o.title}" juntado em "${entry.title}"`, {
      duration: UNDO_MS, action: { label: 'Desfazer', onClick: () => { library.admin.undoTopicMerge(r.batchId).then(onChanged, (e) => toast.error(errorMessage(e))) } },
    })
  })
  const dissolve = () => run(async () => {
    const b = await library.admin.dissolveTopic(entry.topicId!)
    await onChanged()
    toast.success(`Agrupamento "${b.title}" desfeito`, {
      duration: UNDO_MS, action: { label: 'Desfazer', onClick: () => { library.admin.restoreTopic(b.kind, b.slug).then(onChanged, (e) => toast.error(errorMessage(e))) } },
    })
    onGone()
  })
  return (
    <div className="space-y-2 rounded-xl border p-3">
      <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" variant={open ? 'secondary' : 'outline'} disabled={busy} onClick={() => setOpen(!open)}>
          <Merge className="mr-1 h-4 w-4" />Juntar com…
        </Button>
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void dissolve()}
          title="Os itens saem do agrupamento; os arquivos continuam na biblioteca">
          <Split className="mr-1 h-4 w-4" />Desfazer agrupamento
        </Button>
      </div>
      {open && (
        <>
          <p className="text-xs text-muted-foreground">Os itens da escolhida passam para esta; o nome dela vira apelido.</p>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Procurar entrada" aria-label="Procurar entrada para juntar" />
          <ul aria-label="Entradas para juntar" className="max-h-[220px] divide-y overflow-auto rounded-lg border">
            {others.map((o) => (
              <li key={o.ref}>
                <button type="button" disabled={busy} onClick={() => void merge(o)} className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-secondary/60">
                  <Merge className="h-4 w-4 shrink-0" />
                  <span className="min-w-0 flex-1 truncate">{o.title}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{o.count} itens</span>
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  )
}
