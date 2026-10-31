import '@testing-library/jest-dom/vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/__tests__/render'
import { library, type LibraryItem } from '@/lib/library'
import { FilmsShelf } from './FilmsShelf'
import { pickHeroes } from './films'
import { shelfById } from './shelfUi'

vi.mock('@/components/Thumbnail', () => ({ Thumbnail: () => <div data-testid="thumb" /> }))
vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return { ...real, library: { ...real.library, items: vi.fn(), meta: vi.fn().mockResolvedValue(null),
    tvSeries: vi.fn().mockResolvedValue([{ id: 42, kind: 'series', title: 'Serie Sintetica TV', count: 8, watched: 3, coverPath: 'Series/a.mkv' }]) } }
})
const items = library.items as unknown as ReturnType<typeof vi.fn>

let next = 1
const film = (root: string, name: string, durationS: number, size: number, extra: Partial<LibraryItem> = {}) => ({
  id: next++, root, name, title: '', path: `${root}/${name}`, kind: 'video', durationS, size, mtime: next, topics: [], ...extra,
}) as unknown as LibraryItem

describe('FilmsShelf', () => {
  beforeEach(() => {
    items.mockReset()
    items.mockImplementation(({ root }: { root: string }) => Promise.resolve({
      items: root === 'Filmes'
        ? [
          film('Filmes', '[4K] - Carros Turbo Corrida - 2.0 - Segunda - Dual Audio - By Fulano.mkv', 3000, 10),
          film('Filmes', '[4K] - Carros Turbo Corrida - 1.0 - Primeira - Dual Audio - By Fulano.mkv', 3100, 30),
          film('Filmes', 'Filme Solto Qualquer 2019 1080p.mkv', 5400, 20),
        ]
        : [
          film('Animes', 'Espada Mágica O Filme 2025 1080p WEB.mkv', 9000, 50),
          film('Animes', 'Episódio Longo.mkv', 3000, 5, { topics: [{ id: 9, kind: 'series', title: 'S', slug: 's' }] } as never),
        ],
      total: 3, limit: 400, offset: 0,
    }))
  })

  it('mostra franquia em ordem, filmes de anime e o resto', async () => {
    render(<FilmsShelf shelf={shelfById('filmes')} onOpen={vi.fn()} />)
    const fr = await screen.findByRole('region', { name: 'Franquia Carros Turbo Corrida' })
    expect(within(fr).getAllByTestId('film-card').map((b) => b.getAttribute('aria-label')))
      .toEqual(['Abrir Carros Turbo Corrida 1: Primeira', 'Abrir Carros Turbo Corrida 2: Segunda'])
    expect(within(fr).getByText('Primeira')).toBeInTheDocument()
    const anime = screen.getByRole('region', { name: 'Filmes de anime' })
    expect(within(anime).getByText('Espada Mágica O Filme')).toBeInTheDocument()
    expect(within(anime).getByText('Anime')).toBeInTheDocument()
    // episódio de série não é filme
    expect(screen.queryByText(/Episódio Longo/)).not.toBeInTheDocument()
    expect(within(screen.getByRole('region', { name: 'Letra F' })).getByText('Filme Solto Qualquer')).toBeInTheDocument()
  })

  it('lista as séries fora de Animes e abre a série', async () => {
    const { router } = render(<FilmsShelf shelf={shelfById('filmes')} onOpen={vi.fn()} />)
    const tv = await screen.findByRole('region', { name: 'Séries' })
    expect(within(tv).getByText('3 de 8 vistos')).toBeInTheDocument()
    await userEvent.click(within(tv).getByRole('button', { name: 'Abrir Serie Sintetica TV' }))
    expect(router.state.location.search).toMatchObject({ serie: 't42' })
  })

  it('ordena por duração e por tamanho', async () => {
    render(<FilmsShelf shelf={shelfById('filmes')} onOpen={vi.fn()} />)
    await screen.findByRole('region', { name: 'Filmes de anime' })
    const order = () => screen.getAllByTestId('film-card').map((b) => b.getAttribute('aria-label')!.replace('Abrir ', ''))
    await userEvent.selectOptions(screen.getByRole('combobox'), 'longest')
    expect(order()).toEqual(['Espada Mágica O Filme', 'Filme Solto Qualquer', 'Carros Turbo Corrida 1: Primeira', 'Carros Turbo Corrida 2: Segunda'])
    await userEvent.selectOptions(screen.getByRole('combobox'), 'largest')
    expect(order()[1]).toBe('Carros Turbo Corrida 1: Primeira')
  })
})

describe('pickHeroes', () => {
  const going = film('Filmes', 'Pela Metade.mkv', 5000, 1, { progress: { positionS: 900, durationS: 5000, completed: false } } as never)
  const seen = film('Filmes', 'Ja Visto.mkv', 5000, 1, { progress: { positionS: 5000, durationS: 5000, completed: true } } as never)
  const others = Array.from({ length: 8 }, (_, i) => film('Filmes', `Outro ${i}.mkv`, 5000, 1))

  it('sorteia até 6, sem fixar o que está pela metade e sem os já vistos', () => {
    const all = [going, seen, ...others]
    const firsts = new Set<number>()
    for (let k = 0; k < 40; k++) {
      const h = pickHeroes(all)
      expect(h).toHaveLength(6)
      expect(h).not.toContain(seen)
      firsts.add(h[0].id)
    }
    expect(firsts.size).toBeGreaterThan(1)
  })

  it('com tudo visto, sorteia entre os vistos', () => {
    expect(pickHeroes([seen])).toEqual([seen])
  })
})
