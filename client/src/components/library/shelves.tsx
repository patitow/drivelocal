// Estantes da Coleção: cada estante mora em shelves/; aqui só o roteamento.
import { MangaShelf } from './MangaShelf'
import type { OpenFn } from './catalogShared'
import { shelfById, type ShelfId } from './shelves/shelfUi'
import { RootShelf } from './shelves/AnimesShelf'
import { FilmsShelf } from './shelves/FilmsShelf'
import { CreatorsShelf } from './shelves/CreatorsShelf'

export { SHELVES, shelfById, useCreatorNews, useShelfCounts, type Shelf, type ShelfId } from './shelves/shelfUi'
export { SeriesDetail, SeriesStackCard } from './shelves/series'

export function ShelfPage({ id, onOpen, counts }: {
  id: ShelfId
  onOpen: OpenFn
  counts: Partial<Record<ShelfId, number>>
}) {
  const shelf = shelfById(id)
  if (id === 'mangas') return <MangaShelf />
  if (id === 'criadores') return <CreatorsShelf shelf={shelf} onOpen={onOpen} />
  if (id === 'filmes') return <FilmsShelf shelf={shelf} onOpen={onOpen} count={counts[id]} />
  return <RootShelf shelf={shelf} onOpen={onOpen} count={counts[id]} />
}
