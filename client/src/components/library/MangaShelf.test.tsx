import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { render } from '@/__tests__/render'
import { MangaShelf } from './MangaShelf'
import { mergeProgress } from './mangaStore'
import { library, type MangaWork } from '@/lib/library'

vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return { ...real, library: { ...real.library, mangas: vi.fn(), mangaPages: vi.fn(), mangaProgress: vi.fn(), saveMangaProgress: vi.fn(), meta: vi.fn() } }
})

type Fn = ReturnType<typeof vi.fn>
const lib = library as unknown as { mangas: Fn; mangaPages: Fn; mangaProgress: Fn; saveMangaProgress: Fn; meta: Fn }

const work: MangaWork = {
  dir: 'Mangás/Obra Exemplo',
  title: 'Obra Exemplo',
  author: 'Autor',
  cover: 'Mangás/Obra Exemplo/c1/01.jpg',
  pages: 3,
  mtime: 1,
  chapters: [
    { dir: 'Mangás/Obra Exemplo/c1', title: 'Capítulo 1', pages: 2, cover: 'Mangás/Obra Exemplo/c1/01.jpg' },
    { dir: 'Mangás/Obra Exemplo/c2', title: 'Capítulo 2', pages: 1, cover: 'Mangás/Obra Exemplo/c2/01.jpg' },
  ],
}

const page = (p: string, id: number) => ({ id, path: p, name: p.split('/').pop()!, title: '', kind: 'image', root: '', size: 0, mtime: 0, maturity: 'general', favorite: false, tags: [], topics: [] })

describe('MangaShelf', () => {
  beforeEach(() => {
    localStorage.clear()
    lib.mangas.mockResolvedValue([work])
    vi.clearAllMocks()
    lib.mangaProgress.mockResolvedValue({})
    lib.saveMangaProgress.mockResolvedValue({})
    lib.meta.mockResolvedValue(null)
    lib.mangaPages.mockResolvedValue([page('Mangás/Obra Exemplo/c1/10.jpg', 2), page('Mangás/Obra Exemplo/c1/2.jpg', 1)])
  })

  it('lista obras, abre a obra e o leitor em ordem natural, salvando o progresso', async () => {
    render(<MangaShelf />)
    expect(await screen.findByText('1 obra · 2 capítulos')).toBeTruthy()
    fireEvent.click(screen.getByTestId('manga-card'))
    expect(await screen.findByText('Capítulo 2')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Ler do início/ }))
    const imgs = await screen.findAllByAltText(/Página \d/)
    expect(imgs.map((i) => i.getAttribute('src'))).toEqual([expect.stringContaining('2.jpg'), expect.stringContaining('10.jpg')])
    // vai para o servidor (com atraso curto para juntar páginas)
    await waitFor(() => expect(lib.saveMangaProgress).toHaveBeenCalledWith({
      [work.dir]: expect.objectContaining({ chapter: work.chapters[0].dir, pages: 2 }),
    }), { timeout: 4000 })
  })

  it('migra o progresso antigo do navegador para o servidor uma vez', async () => {
    const old = { [work.dir]: { chapter: work.chapters[1].dir, page: 0, pages: 1, at: 5, done: [work.chapters[0].dir] } }
    localStorage.setItem('drivelocal:manga:progress', JSON.stringify(old))
    render(<MangaShelf />)
    await waitFor(() => expect(lib.saveMangaProgress).toHaveBeenCalledWith(old))
    await waitFor(() => expect(localStorage.getItem('drivelocal:manga:progress')).toBeNull())
    expect(await screen.findByText('1/2 lidos')).toBeTruthy()
  })

  it('servidor mais novo ganha do espelho local', () => {
    const a = { chapter: 'c1', page: 3, pages: 9, at: 10, done: [] }
    const b = { chapter: 'c2', page: 0, pages: 9, at: 20, done: ['c1'] }
    expect(mergeProgress({ w: b }, { w: a })).toEqual({ merged: { w: b }, push: {} })
    const r = mergeProgress({ w: a }, { w: b })
    expect(r.push.w.chapter).toBe('c2')
  })
})
