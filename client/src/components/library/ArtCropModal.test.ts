import { describe, expect, it } from 'vitest'
import { fitCrop } from './ArtCropModal'

describe('fitCrop', () => {
  it('foto quadrada num vídeo 16:9 ocupa a altura toda, centrada', () => {
    const c = fitCrop(16 / 9, 1)
    expect(c.h).toBeCloseTo(1)
    expect(c.w).toBeCloseTo(9 / 16)
    expect(c.x).toBeCloseTo((1 - 9 / 16) / 2)
    expect(c.y).toBeCloseTo(0)
  })
  it('banner largo numa imagem em pé ocupa a largura toda', () => {
    const c = fitCrop(2 / 3, 16 / 6)
    expect(c.w).toBeCloseTo(1)
    expect(c.h).toBeCloseTo((2 / 3) / (16 / 6))
    expect(c.x).toBeCloseTo(0)
  })
})
