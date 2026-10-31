import { describe, expect, it } from 'vitest'
import type { LibraryItem } from './library'
import { isAnimePath, isInProgress, isWatched } from './mediaRules'

const at = (positionS: number, completed = false) =>
  ({ progress: { positionS, durationS: 1000, completed } }) as unknown as LibraryItem

describe('mediaRules', () => {
  it('reconhece a pasta de anime pelo caminho', () => {
    expect(isAnimePath('Animes/Serie/ep 01.mkv')).toBe(true)
    expect(isAnimePath('/Animes/Serie/ep 01.mkv')).toBe(true)
    expect(isAnimePath('Filmes/Animes do ano.mkv')).toBe(false)
  })

  it('visto e em andamento usam a mesma régua', () => {
    expect(isWatched(at(960))).toBe(true)
    expect(isInProgress(at(960))).toBe(false)
    expect(isInProgress(at(300))).toBe(true)
    expect(isWatched(at(300, true))).toBe(true)
    expect(isInProgress(at(0))).toBe(false)
  })
})
