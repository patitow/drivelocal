import '@testing-library/jest-dom/vitest'
import { screen, waitFor, within } from '@testing-library/react'
import { render } from '@/__tests__/render'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { library } from '@/lib/library'
import { useRouter, useRouterState } from '@tanstack/react-router'
import { CatalogView } from './CatalogView'
import { CatalogNav, type CatalogTab } from './CatalogNav'

// A Coleção como no app: seções na barra lateral (CatalogNav) + conteúdo, com
// a seção no endereço (/colecao/:secao).
function Colecao() {
  const router = useRouter()
  const tab = (useRouterState({ select: (s) => s.location.pathname }).split('/')[2] || 'home') as CatalogTab
  return (
    <>
      <CatalogNav active={tab} onPick={(t) => router.history.push(t === 'home' ? '/colecao' : `/colecao/${t}`)} showLabels isAdmin={role === 'admin'} />
      <CatalogView tab={tab} />
    </>
  )
}

let role = 'user'
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ role }) }))
vi.mock('@/components/Thumbnail', () => ({ Thumbnail: () => <div data-testid="thumb" /> }))
vi.mock('@/components/VideoPlayer', () => ({
  VideoPlayer: ({ file, files }: { file: { name: string }; files: unknown[] }) => <div data-testid="player">{file.name} de {files.length}</div>,
}))
vi.mock('@/components/ImageViewer', () => ({ ImageViewer: () => <div data-testid="viewer" /> }))
vi.mock('@/components/MobileMenuButton', () => ({ MobileMenuButton: () => null }))
vi.mock('sonner', () => ({ toast: Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn() }) }))
vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return {
    ...real,
    library: {
      ...real.library,
      continueWatching: vi.fn(),
      items: vi.fn(),
      favorite: vi.fn(),
      playlists: vi.fn(),
      playlist: vi.fn(),
      createPlaylist: vi.fn(),
      home: vi.fn(),
      feed: vi.fn(),
      search: vi.fn(),
      dismissContinue: vi.fn(),
      clearContinue: vi.fn(),
      addToPlaylist: vi.fn(),
      topicItems: vi.fn(),
      topics: vi.fn(),
      admin: {
        ...real.library.admin,
        stats: vi.fn(),
        aiStatus: vi.fn(),
        review: vi.fn(),
        topicDupes: vi.fn(),
        topicMerges: vi.fn(),
        acceptTopicDupes: vi.fn(),
        rejectTopicDupes: vi.fn(),
        undoTopicMerge: vi.fn(),
        topicFlags: vi.fn(),
        topicBlocks: vi.fn(),
        dissolveTopic: vi.fn(),
        dismissTopicFlag: vi.fn(),
        restoreTopic: vi.fn(),
      },
    },
  }
})

const lib = library as unknown as Record<string, ReturnType<typeof vi.fn>>
const adm = library.admin as unknown as Record<string, ReturnType<typeof vi.fn>>

const item = (id: number, name: string, extra: Record<string, unknown> = {}) => ({
  id, path: `Animes/Serie Exemplo/${name}`, name, title: name, kind: 'video', root: 'Animes', size: 1, mtime: 0,
  maturity: 'geral', favorite: false, tags: [], topics: [], ...extra,
})

const homeData = () => ({
  generatedAt: 0,
  shelves: [
    { id: 'continue', title: 'Continuar assistindo', kind: 'items', actions: ['dismiss'], items: [
      item(1, 'ep 01.mkv', { progress: { positionS: 300, durationS: 1400, completed: false, updatedAt: 0 } }),
      item(3, 'ep 03.mkv', { progress: { positionS: 10, durationS: 1400, completed: false, updatedAt: 0 } }),
    ] },
    { id: 'favorites', title: 'Favoritos', kind: 'items', items: [item(2, 'ep 02.mkv', { favorite: true })] },
    { id: 'top-series', title: 'Séries em destaque', kind: 'topics', topics: [{ id: 7, kind: 'series', title: 'Serie Exemplo', count: 3, watched: 1, coverPath: 'Animes/Serie Exemplo/ep 01.mkv' }] },
  ],
})

describe('CatalogView', () => {
  beforeEach(() => {
    role = 'user'
    Object.values(lib).forEach((f) => f.mockReset?.())
    Object.values(adm).forEach((f) => f.mockReset?.())
    lib.home.mockResolvedValue(homeData())
    lib.feed.mockResolvedValue({ items: [], next: -1 })
    lib.items.mockResolvedValue({ items: [], total: 0, limit: 48, offset: 0 })
    adm.topicFlags.mockResolvedValue([])
    adm.topicBlocks.mockResolvedValue([])
  })

  it('Início mostra as prateleiras; esconde Revisão para não-admin', async () => {
    render(<Colecao />)
    expect(await screen.findByTestId('home-hero')).toHaveTextContent('ep 01.mkv')
    const cont = screen.getByRole('region', { name: 'Continuar assistindo' })
    const favs = screen.getByRole('region', { name: 'Favoritos' })
    expect(within(cont).getByText('ep 01.mkv')).toBeInTheDocument()
    expect(within(favs).getByText('ep 02.mkv')).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: /Revisão/ })).not.toBeInTheDocument()
    expect(lib.home).toHaveBeenCalledTimes(1)
    // Abrir um card toca a fila da prateleira.
    await userEvent.click(screen.getByRole('button', { name: 'Abrir ep 03.mkv' }))
    expect(screen.getByTestId('player')).toHaveTextContent('ep 03.mkv de 2')
  })

  it('remover de continuar assistindo tira só aquele item', async () => {
    lib.dismissContinue.mockResolvedValue({ ok: true })
    render(<Colecao />)
    await userEvent.click(await screen.findByRole('button', { name: 'Remover ep 01.mkv de continuar assistindo' }))
    expect(lib.dismissContinue).toHaveBeenCalledWith(1)
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Remover ep 01.mkv de continuar assistindo' })).not.toBeInTheDocument())
    expect(screen.getAllByText('ep 03.mkv').length).toBeGreaterThan(0)
    expect(lib.favorite).not.toHaveBeenCalled()
  })

  it('limpar tudo pede confirmação', async () => {
    lib.clearContinue.mockResolvedValue({ dismissed: 2 })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    render(<Colecao />)
    const btn = await screen.findByRole('button', { name: 'Limpar tudo' })
    await userEvent.click(btn)
    expect(lib.clearContinue).not.toHaveBeenCalled()
    await userEvent.click(btn)
    expect(lib.clearContinue).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Continuar assistindo' })).not.toBeInTheDocument())
    expect(within(screen.getByRole('region', { name: 'Favoritos' })).getByText('ep 02.mkv')).toBeInTheDocument()
    confirm.mockRestore()
  })

  it('série da home abre a série na estante de animes, mesmo fora da pasta Animes', async () => {
    const eps = [item(1, 'ep 01.mkv', { episode: 1 })]
    const group = { type: 'series', series: { key: 't7', topicId: 7, title: 'Serie Exemplo', coverPath: eps[0].path, matched: 1, total: 1, maturity: 'geral', next: eps[0], best: eps[0], items: eps, watched: 0 } }
    // a estante (raiz Animes) não tem a série; a busca pelo tópico tem
    lib.search.mockImplementation((q: { topicId?: number }) => Promise.resolve({ total: 1, truncated: false, groups: q.topicId === 7 ? [group] : [] }))
    render(<Colecao />)
    await userEvent.click(await screen.findByRole('button', { name: 'Abrir Serie Exemplo' }))
    await waitFor(() => expect(lib.search).toHaveBeenCalledWith(expect.objectContaining({ topicId: 7 })))
    expect(await screen.findByRole('heading', { name: 'Serie Exemplo' })).toBeInTheDocument()
    expect(lib.topicItems).not.toHaveBeenCalled()
  })

  it('feed "Para você" mostra o motivo e leva à série de onde veio', async () => {
    // jsdom não carrega imagens: o feed mede o aspecto antes de montar a grade
    vi.stubGlobal('Image', class { onload?: () => void; onerror?: () => void; set src(_: string) { queueMicrotask(() => this.onerror?.()) } })
    lib.feed.mockResolvedValue({
      items: [item(5, 'ep 05.mkv', { reason: 'Próximo de Serie Exemplo', reasonKind: 'next', reasonTopic: { id: 7, kind: 'series', title: 'Serie Exemplo' } })],
      next: -1,
    })
    lib.search.mockResolvedValue({ total: 0, truncated: false, groups: [] })
    render(<Colecao />)
    const feed = await screen.findByRole('region', { name: 'Para você' })
    expect(await within(feed).findByText('ep 05.mkv')).toBeInTheDocument()
    expect(within(feed).getByText('Próximo episódio')).toBeInTheDocument()
    await userEvent.click(within(feed).getByRole('tab', { name: 'Filmes e Séries' }))
    await waitFor(() => expect(lib.feed).toHaveBeenLastCalledWith('films', expect.any(Number), 0))
    await userEvent.click(within(feed).getByRole('tab', { name: 'Tudo' }))
    await userEvent.click(await within(feed).findByRole('button', { name: 'Próximo de Serie Exemplo' }))
    await waitFor(() => expect(lib.search).toHaveBeenCalledWith(expect.objectContaining({ topicId: 7 })))
  })

  it('admin vê a aba Revisão', async () => {
    role = 'admin'
    render(<Colecao />)
    expect(await screen.findByRole('tab', { name: /Revisão/ })).toBeInTheDocument()
  })

  it('favoritar alterna o coração', async () => {
    lib.favorite.mockResolvedValue({ favorite: true })
    render(<Colecao />)
    const btn = (await screen.findAllByRole('button', { name: 'Favoritar' }))[0]
    await userEvent.click(btn)
    expect(lib.favorite).toHaveBeenCalledWith(1, true)
    await waitFor(() => expect(btn).toHaveAttribute('aria-pressed', 'true'))
  })

  it('busca agrupa episódios em série e deixa itens soltos', async () => {
    const eps = [item(1, 'ep 01.mkv', { episode: 1 }), item(2, 'ep 02.mkv', { episode: 2 })]
    lib.search.mockResolvedValue({
      total: 3,
      truncated: false,
      groups: [
        { type: 'series', series: { key: 't7', topicId: 7, title: 'Serie Exemplo', coverPath: eps[0].path, matched: 2, total: 12, year: 2019, maturity: 'geral', next: eps[1], best: eps[0], items: eps, watched: 1 } },
        { type: 'item', item: item(9, 'Filme Teste.mp4', { path: 'Filmes/Filme Teste.mp4', root: 'Filmes' }) },
      ],
    })
    render(<Colecao />)
    await userEvent.click(await screen.findByRole('tab', { name: /Busca/ }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Buscar' }), 'exemplo')
    await waitFor(() => expect(lib.search).toHaveBeenLastCalledWith(expect.objectContaining({ q: 'exemplo' })))
    const card = await screen.findByTestId('series-card')
    expect(card).toHaveTextContent('Serie Exemplo')
    expect(card).toHaveTextContent('12 episódios')
    expect(card).toHaveTextContent('2019')
    expect(card).toHaveTextContent('1 de 12 vistos')
    expect(screen.getByText('Filme Teste.mp4')).toBeInTheDocument()
    expect(screen.queryByText('ep 01.mkv')).not.toBeInTheDocument()
    await userEvent.click(card)
    expect((await screen.findAllByTestId('episode-row')).map((r) => r.textContent)).toEqual([expect.stringContaining('Episódio 1'), expect.stringContaining('Episódio 2')])
    await userEvent.click(screen.getByRole('button', { name: /Continuar/ }))
    expect(screen.getByTestId('player')).toHaveTextContent('ep 02.mkv de 2')
  })

  it('revisão de tópicos duplicados: juntar escolhendo o canônico e desfazer', async () => {
    role = 'admin'
    adm.stats.mockResolvedValue({})
    adm.aiStatus.mockResolvedValue({ paused: false })
    adm.review.mockResolvedValue({ suggestions: [], total: 0 })
    adm.topicDupes.mockResolvedValue({
      threshold: 0.86,
      groups: [{ kind: 'creator', score: 0.93, canonical: { id: 1, title: 'Autor Alfa', count: 10, score: 0, examples: ['a.mp4'] },
        members: [{ id: 2, title: 'autor_alfa', count: 3, score: 0.93, examples: ['b.mp4'] }] }],
    })
    adm.topicMerges.mockResolvedValue([{ batchId: 'mold', into: 'Serie X', merged: ['Serie-X'], actor: 'adm', createdAt: 0, undone: false }])
    adm.acceptTopicDupes.mockResolvedValue({ batchId: 'mnew', merged: 1 })
    adm.undoTopicMerge.mockResolvedValue({ restored: 1 })
    render(<Colecao />)
    await userEvent.click(await screen.findByRole('tab', { name: /Revisão/ }))
    const group = await screen.findByTestId('dup-group')
    expect(group).toHaveTextContent('Autor Alfa')
    expect(group).toHaveTextContent('10 itens')
    expect(group).toHaveTextContent('ex.: b.mp4')
    await userEvent.click(screen.getByRole('radio', { name: 'Usar "autor_alfa" como canônico' }))
    await userEvent.click(screen.getByRole('button', { name: /Juntar/ }))
    expect(adm.acceptTopicDupes).toHaveBeenCalledWith(2, [1])
    await userEvent.click(await screen.findByRole('button', { name: 'Desfazer fusão em Serie X' }))
    expect(adm.undoTopicMerge).toHaveBeenCalledWith('mold')
  })

  it('playlist "Tocar tudo" abre o player com a fila inteira', async () => {
    lib.playlists.mockResolvedValue([{ id: 9, name: 'Maratona', itemCount: 2, durationS: 0, canEdit: true, ownerId: 'u', description: '', visibility: 'private', createdAt: 0, updatedAt: 0 }])
    lib.playlist.mockResolvedValue({ playlist: { id: 9 }, items: [item(1, 'ep 01.mkv'), item(2, 'ep 02.mkv')] })
    render(<Colecao />)
    await userEvent.click(await screen.findByRole('tab', { name: /Playlists/ }))
    await userEvent.click(await screen.findByText('Maratona'))
    await userEvent.click(await screen.findByRole('button', { name: /Tocar tudo/ }))
    expect(screen.getByTestId('player')).toHaveTextContent('ep 01.mkv de 2')
  })

  it('a playlist aberta fica na URL; voltar fecha', async () => {
    lib.playlists.mockResolvedValue([{ id: 9, name: 'Maratona', itemCount: 2, durationS: 0, canEdit: true, ownerId: 'u', description: '', visibility: 'private', createdAt: 0, updatedAt: 0 }])
    lib.playlist.mockResolvedValue({ playlist: { id: 9, name: 'Maratona' }, items: [item(1, 'ep 01.mkv')] })
    const { router } = render(<Colecao />)
    await userEvent.click(await screen.findByRole('tab', { name: /Playlists/ }))
    await userEvent.click(await screen.findByText('Maratona'))
    await waitFor(() => expect(router.state.location.href).toBe('/colecao/playlists?playlist=9'))
    expect(await screen.findByRole('button', { name: /Tocar tudo/ })).toBeInTheDocument()
    router.history.back()
    await waitFor(() => expect(screen.queryByRole('button', { name: /Tocar tudo/ })).not.toBeInTheDocument())
    expect(router.state.location.pathname).toBe('/colecao/playlists')
  })
})
