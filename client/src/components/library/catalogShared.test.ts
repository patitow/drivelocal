import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { LibraryItem } from '@/lib/library'

vi.mock('@/lib/library', async (orig) => ({
  ...(await orig<typeof import('@/lib/library')>()),
  library: { search: vi.fn() },
}))

import { library } from '@/lib/library'
import { inSeries, introGuessable } from './catalogShared'

const ep = (id: number, topic?: number) => ({
  id, path: `p/${id}.mkv`, name: `${id}.mkv`, title: String(id), kind: 'video', root: 'Series', size: 1, mtime: 1,
  maturity: 'geral', favorite: false, tags: [], topics: topic ? [{ id: topic, kind: 'series', title: 'Serie Exemplo' }] : [],
}) as LibraryItem

describe('inSeries', () => {
  beforeEach(() => vi.clearAllMocks())

  it('episódio aberto do "continuar" vira a fila da série, na ordem', async () => {
    const shelf = [ep(31, 7), ep(90, 8), ep(5)]
    const episodes = [ep(29, 7), ep(30, 7), ep(31, 7), ep(32, 7)]
    vi.mocked(library.search).mockResolvedValue({ total: 4, truncated: false, groups: [{ type: 'series', series: { key: 't7', topicId: 7, title: 'Serie Exemplo', coverPath: '', matched: 4, total: 4, watched: 0, maturity: 'geral', best: episodes[0], items: episodes } }] })
    const r = await inSeries(shelf, 0)
    expect(library.search).toHaveBeenCalledWith(expect.objectContaining({ topicId: 7 }))
    expect(r.list.map((x) => x.id)).toEqual([29, 30, 31, 32])
    expect(r.index).toBe(2)
  })

  it('lista já da série, item sem série ou falha na busca: fica como veio', async () => {
    const same = [ep(1, 7), ep(2, 7)]
    expect((await inSeries(same, 1)).list).toBe(same)
    const loose = [ep(5), ep(1, 7)]
    expect((await inSeries(loose, 0)).list).toBe(loose)
    expect(library.search).not.toHaveBeenCalled()
    vi.mocked(library.search).mockRejectedValue(new Error('x'))
    const r = await inSeries(loose, 1)
    expect(r).toEqual({ list: loose, index: 1 })
  })
})

describe('introGuessable', () => {
  const it_ = (extra: Partial<LibraryItem>) => ({ ...ep(1), root: 'Animes', episode: 3, durationS: 1420, ...extra }) as LibraryItem

  it('episódio de anime oferece pular a abertura', () => {
    expect(introGuessable(it_({}))).toBe(true)
  })

  it('filme, série, gravação e vídeo sem episódio não oferecem', () => {
    expect(introGuessable(it_({ durationS: 6000 }))).toBe(false)
    expect(introGuessable(it_({ root: 'Series' }))).toBe(false)
    expect(introGuessable(it_({ root: 'Filmes' }))).toBe(false)
    expect(introGuessable(it_({ episode: undefined }))).toBe(false)
    expect(introGuessable(it_({ kind: 'image' }))).toBe(false)
  })
})
