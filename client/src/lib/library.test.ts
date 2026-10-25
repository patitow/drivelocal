import { describe, expect, it, vi, beforeEach } from 'vitest'
import { api } from '@/lib/api'
import { formatDuration, itemQueryParams, library, nextPlayableIndex, resumePoint, shouldSaveProgress } from './library'

vi.mock('@/lib/api', () => ({ api: { request: vi.fn() } }))
const request = (api as unknown as { request: ReturnType<typeof vi.fn> }).request

const prog = (positionS: number, durationS: number, completed = false) => ({ positionS, durationS, completed, updatedAt: 0 })

describe('resumePoint', () => {
  it('retoma do meio', () => expect(resumePoint(prog(600, 1400))).toBe(600))
  it('ignora começo, fim e concluído', () => {
    expect(resumePoint(prog(5, 1400))).toBeNull()
    expect(resumePoint(prog(1350, 1400))).toBeNull()
    expect(resumePoint(prog(600, 1400, true))).toBeNull()
    expect(resumePoint(null)).toBeNull()
  })
  it('usa a duração real quando informada', () => expect(resumePoint(prog(600, 0), 620)).toBeNull())
})

describe('nextPlayableIndex', () => {
  const files = [
    { name: 'a.mkv', isVideo: true },
    { name: 'capa.jpg', isImage: true },
    { name: 'b.mp4', isVideo: true },
  ]
  it('pula imagens', () => expect(nextPlayableIndex(files, 0)).toBe(2))
  it('null no último', () => expect(nextPlayableIndex(files, 2)).toBeNull())
  it('reconhece vídeo pela extensão', () => expect(nextPlayableIndex([{ name: 'x' }, { name: 'ep 02.MKV' }], 0)).toBe(1))
})

describe('shouldSaveProgress', () => {
  it('só grava após andar ~10 s', () => {
    expect(shouldSaveProgress(null, 3)).toBe(false)
    expect(shouldSaveProgress(null, 20)).toBe(true)
    expect(shouldSaveProgress(100, 105)).toBe(false)
    expect(shouldSaveProgress(100, 111)).toBe(true)
    expect(shouldSaveProgress(100, 101, true)).toBe(true)
  })
})

describe('helpers', () => {
  it('itemQueryParams omite vazios e junta tags', () => {
    expect(itemQueryParams({ q: 'ola', tags: ['a', 'b'], favorites: false, unwatched: true, limit: 10, kind: '' }))
      .toEqual({ q: 'ola', tags: 'a,b', unwatched: 'true', limit: '10' })
  })
  it('formatDuration', () => {
    expect(formatDuration(65)).toBe('1:05')
    expect(formatDuration(3725)).toBe('1:02:05')
    expect(formatDuration(undefined)).toBe('')
  })
})

describe('library client', () => {
  beforeEach(() => request.mockReset())
  it('progresso por caminho vai na query, não no path', async () => {
    request.mockResolvedValue({ data: { mediaId: 1, favorite: false, prefs: { autoResume: true, autoplay: true, maxMaturity: 'geral' } }, status: 200 })
    await library.progressByPath('Animes/Serie Exemplo/ep 01.mkv')
    expect(request).toHaveBeenCalledWith({ method: 'GET', url: '/api/me/progress', data: undefined, params: { path: 'Animes/Serie Exemplo/ep 01.mkv' } })
    await library.saveProgressByPath('x.mp4', 120, 1400)
    expect(request).toHaveBeenLastCalledWith({ method: 'PUT', url: '/api/me/progress', data: { positionS: 120, durationS: 1400, completed: undefined }, params: { path: 'x.mp4' } })
  })
  it('favoritar usa PUT/DELETE', async () => {
    request.mockResolvedValue({ data: { favorite: true }, status: 200 })
    await library.favorite(7, true)
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ method: 'PUT', url: '/api/me/favorites/7' }))
    await library.favorite(7, false)
    expect(request).toHaveBeenLastCalledWith(expect.objectContaining({ method: 'DELETE', url: '/api/me/favorites/7' }))
  })
})

describe('segunda barreira do conteúdo explícito', () => {
  const it3 = (id: number, maturity: string) => ({ id, maturity, path: `p/${id}.mp4` })
  const home = () => ({
    shelves: [
      { id: 'continue', kind: 'items', items: [it3(1, 'geral'), it3(2, 'explicito')] },
      { id: 'top-creator', kind: 'items', items: [it3(3, 'explicito')] },
    ],
  })
  const respond = (prefs: object) => request.mockImplementation((cfg?: { url?: string }) =>
    Promise.resolve({ data: cfg?.url === '/api/me/prefs' ? prefs : home() }))

  beforeEach(() => request.mockReset())

  it('resposta velha não mostra explícito com o modo desligado', async () => {
    respond({ restrictedAllowed: true, restrictedMode: false })
    await library.prefs()
    const d = await library.home()
    expect(d.shelves.map((s) => s.id)).toEqual(['continue'])
    expect(d.shelves[0].items?.map((i) => i.id)).toEqual([1])
  })

  it('com o modo ligado passa tudo, e o switch vale na hora', async () => {
    respond({ restrictedAllowed: true, restrictedMode: true })
    await library.prefs()
    expect((await library.home()).shelves).toHaveLength(2)
    respond({ restrictedAllowed: true, restrictedMode: false })
    await library.setRestrictedMode(false)
    expect((await library.home()).shelves[0].items?.map((i) => i.id)).toEqual([1])
  })

  it('cards de tópico explícitos saem da home, da lista de tópicos e das séries', async () => {
    const card = (id: number, maturity?: string) => ({ id, title: `t${id}`, coverPath: `c/${id}.jpg`, maturity })
    request.mockImplementation((cfg?: { url?: string }) => Promise.resolve({
      data: {
        '/api/me/prefs': { restrictedAllowed: true, restrictedMode: false },
        '/api/catalog/home': { shelves: [
          { id: 'series', kind: 'topics', topics: [card(1, 'geral'), card(2, 'explicito')] },
          { id: 'creators', kind: 'topics', topics: [card(3, 'explicito')] },
        ] },
        '/api/topics': { topics: [card(4, 'explicito'), card(5)], total: 2 },
        '/api/catalog/tv-series': { series: [card(6, 'explicito'), card(7, 'sugestivo')] },
      }[cfg?.url ?? ''],
    }))
    await library.prefs()
    const h = await library.home()
    expect(h.shelves.map((s) => s.id)).toEqual(['series'])
    expect(h.shelves[0].topics?.map((t) => t.id)).toEqual([1])
    expect((await library.topics()).topics.map((t) => t.id)).toEqual([5])
    expect((await library.tvSeries()).map((t) => t.id)).toEqual([7])
  })
})
