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
