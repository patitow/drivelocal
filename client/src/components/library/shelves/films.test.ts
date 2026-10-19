import { describe, expect, it } from 'vitest'
import type { LibraryItem } from '@/lib/library'
import { filmInfo, franchises, inFranchise } from './films'

const f = (name: string) => ({ id: name.length, name, title: name, path: `Filmes/${name}` }) as unknown as LibraryItem

describe('filmInfo', () => {
  it('limpa etiquetas, ficha técnica e assinatura', () => {
    expect(filmInfo(f('[4K REMASTERIZADO] - Carros Turbo Corrida - 2.0 - Velocidade Máxima - Dual Audio - By Fulano.mkv')))
      .toEqual({ title: 'Carros Turbo Corrida 2: Velocidade Máxima', year: undefined, part: 2 })
    expect(filmInfo(f('Espada Demoníaca Castelo Infinito 2025 1080p WEB DUAL H264-GRP.mkv'))).toEqual({ title: 'Espada Demoníaca Castelo Infinito', year: 2025, part: undefined })
    expect(filmInfo(f('Homem.Serra.O.Filme.Arco.2025.2160p.iT.WEB-DL.DUAL.DDP5.1.H.265-GRP.mkv')).title).toBe('Homem Serra O Filme Arco')
    expect(filmInfo(f('Cidade 2049 (2017) [1080p].mp4'))).toMatchObject({ title: 'Cidade 2049', year: 2017 })
  })
})

describe('franchises', () => {
  it('junta filmes com o mesmo começo e ordena', () => {
    const list = [
      f('Carros Turbo Corrida - 2.0 - B - Dual Audio.mkv'),
      f('Outro Filme Qualquer.mkv'),
      f('Carros Turbo Via Mundial.mkv'),
      f('Carros Turbo Corrida - 1.0 - A - Dual Audio.mkv'),
    ]
    const out = franchises(list)
    expect(out).toHaveLength(1)
    expect(out[0].name).toBe('Carros Turbo')
    expect(out[0].items.map((it) => filmInfo(it).title)).toEqual(['Carros Turbo Corrida 1: A', 'Carros Turbo Corrida 2: B', 'Carros Turbo Via Mundial'])
    expect(inFranchise('Carros Turbo Corrida 1: A', 'Carros Turbo')).toBe('Corrida 1: A')
    expect(inFranchise('Carros Turbo Corrida 1: A', 'Carros Turbo Corrida')).toBe('A')
  })
  it('artigo no começo não vira franquia sozinho', () => {
    expect(franchises([f('The Casa Azul.mkv'), f('The Casa Verde.mkv'), f('The Rua.mkv')])).toHaveLength(0)
  })
})
