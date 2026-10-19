import { describe, expect, it } from 'vitest'
import { clampDelay, describeDelay, formatDelay, loadSync, saveSync, stepDelay, syncKey, SYNC_LIMIT_MS } from './sync'

function memStore() {
  const m = new Map<string, string>()
  return {
    m,
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => { m.set(k, v) },
    removeItem: (k: string) => { m.delete(k) },
  }
}

describe('ajuste de sincronia', () => {
  it('passos de 50 ms, limitados', () => {
    let d = { audio: 0, sub: 0 }
    d = stepDelay(d, 'audio', -1)
    d = stepDelay(d, 'audio', -1)
    d = stepDelay(d, 'sub', 1)
    expect(d).toEqual({ audio: -100, sub: 50 })
    expect(stepDelay({ audio: 1490, sub: 0 }, 'audio', 1).audio).toBe(SYNC_LIMIT_MS.audio)
    expect(clampDelay('audio', -99999)).toBe(-1500)
    expect(clampDelay('sub', 45000)).toBe(30000)
    expect(clampDelay('sub', NaN)).toBe(0)
    expect(clampDelay('audio', 12.6)).toBe(13)
  })

  it('atalhos G/H (legenda) e J/K (áudio), sem diferenciar maiúscula', () => {
    expect(syncKey('g')).toEqual({ kind: 'sub', dir: -1 })
    expect(syncKey('H')).toEqual({ kind: 'sub', dir: 1 })
    expect(syncKey('j')).toEqual({ kind: 'audio', dir: -1 })
    expect(syncKey('K')).toEqual({ kind: 'audio', dir: 1 })
    expect(syncKey('x')).toBeNull()
  })

  it('guarda por arquivo e apaga quando volta a zero', () => {
    const s = memStore()
    saveSync('Filmes/a.mkv', { audio: -200, sub: 350 }, s)
    expect(loadSync('Filmes/a.mkv', s)).toEqual({ audio: -200, sub: 350 })
    expect(loadSync('Filmes/b.mkv', s)).toEqual({ audio: 0, sub: 0 })
    saveSync('Filmes/a.mkv', { audio: 0, sub: 0 }, s)
    expect(s.m.size).toBe(0)
  })

  it('ignora lixo e valores fora do limite no armazenamento', () => {
    const s = memStore()
    s.setItem('drivelocal:sync:x.mkv', '{nope')
    expect(loadSync('x.mkv', s)).toEqual({ audio: 0, sub: 0 })
    s.setItem('drivelocal:sync:x.mkv', JSON.stringify({ audio: 9000, sub: 'abc' }))
    expect(loadSync('x.mkv', s)).toEqual({ audio: 1500, sub: 0 })
    expect(loadSync('x.mkv', null)).toEqual({ audio: 0, sub: 0 })
    const broken = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') }, removeItem: () => {} }
    expect(loadSync('x.mkv', broken)).toEqual({ audio: 0, sub: 0 })
    expect(() => saveSync('x.mkv', { audio: 50, sub: 0 }, broken)).not.toThrow()
  })

  it('formata para exibição', () => {
    expect(formatDelay(0)).toBe('0 ms')
    expect(formatDelay(150)).toBe('+150 ms')
    expect(formatDelay(-50)).toBe('−50 ms')
    expect(formatDelay(-1250)).toBe('−1,25 s')
    expect(describeDelay('audio', -200)).toBe('Áudio: −200 ms (antes da imagem)')
    expect(describeDelay('sub', 0)).toBe('Legenda: sem ajuste')
  })
})
