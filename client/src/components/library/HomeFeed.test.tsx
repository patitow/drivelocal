import '@testing-library/jest-dom/vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useQueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/__tests__/render'
import { library, type FeedItem } from '@/lib/library'
import { HomeFeed } from './HomeFeed'

vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return { ...real, library: { ...real.library, feed: vi.fn() } }
})
const feed = library.feed as unknown as ReturnType<typeof vi.fn>

const item = (id: number, title: string) =>
  ({ id, title, name: `${title}.mp4`, path: `pasta/${title}.mp4`, kind: 'video', root: '', size: 1, mtime: 0, reason: 'Descubra', reasonKind: 'discover' }) as FeedItem

// trocar uma preferência invalida todas as queries: o que saiu do feed tem que sair da tela
function Feed() {
  const qc = useQueryClient()
  return (
    <>
      <button type="button" onClick={() => void qc.invalidateQueries()}>recarregar</button>
      <HomeFeed onOpen={() => {}} onOpenTopic={() => {}} />
    </>
  )
}

describe('HomeFeed', () => {
  it('tira da tela o que sumiu do feed ao recarregar', async () => {
    feed.mockResolvedValue({ items: [item(1, 'fica'), item(2, 'sai')], next: -1 })
    render(<Feed />)
    expect(await screen.findByText('sai', {}, { timeout: 4000 })).toBeInTheDocument()

    feed.mockResolvedValue({ items: [item(1, 'fica'), item(3, 'entra')], next: -1 })
    await userEvent.click(screen.getByRole('button', { name: 'recarregar' }))
    expect(await screen.findByText('entra', {}, { timeout: 4000 })).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('sai')).not.toBeInTheDocument())
    expect(screen.getByText('fica')).toBeInTheDocument()
  }, 15000)
})
