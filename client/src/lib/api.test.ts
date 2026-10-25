import { describe, expect, it, vi } from 'vitest'

const get = vi.hoisted(() => vi.fn())
vi.mock('axios', () => ({
  default: {
    create: () => ({ get, interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } } }),
  },
}))

import { api } from './api'

const item = (i: number) => ({ name: `ep ${i}.mp4`, path: `pasta/ep ${i}.mp4`, isDirectory: false })
const page = (from: number, to: number, hasMore: boolean) => ({
  data: { currentPath: 'pasta', items: Array.from({ length: to - from }, (_, k) => item(from + k)), pagination: { hasMore } },
})

describe('getFiles', () => {
  it('junta todas as páginas da pasta', async () => {
    get.mockReset()
    get.mockResolvedValueOnce(page(0, 100, true)).mockResolvedValueOnce(page(100, 117, false))
    const dir = await api.getFiles('pasta')
    expect(dir.items).toHaveLength(117)
    expect(dir.items[116].name).toBe('ep 116.mp4')
    expect(get.mock.calls[1][1].params).toMatchObject({ path: 'pasta', offset: 100 })
  })

  it('para numa página vazia mesmo com hasMore', async () => {
    get.mockReset()
    get.mockResolvedValueOnce(page(0, 3, true)).mockResolvedValueOnce(page(0, 0, true))
    expect((await api.getFiles('pasta')).items).toHaveLength(3)
    expect(get).toHaveBeenCalledTimes(2)
  })
})
