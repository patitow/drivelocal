import '@testing-library/jest-dom/vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/__tests__/render'
import { library, type Entry } from '@/lib/library'
import { ManageTab } from './ManageTab'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return {
    ...real,
    library: {
      ...real.library,
      items: vi.fn().mockResolvedValue({ items: [], total: 0, limit: 0, offset: 0 }),
      admin: { ...real.library.admin, entries: vi.fn(), createEntry: vi.fn(), updateEntry: vi.fn(), deleteEntry: vi.fn(), searchMeta: vi.fn(), imageFolders: vi.fn() },
    },
  }
})
const admin = library.admin as unknown as Record<string, ReturnType<typeof vi.fn>>

const serie: Entry = { ref: 'topic:7', cat: 'animes', title: 'Serie Exemplo', count: 12, topicId: 7, aliases: [], maturity: 'geral' }

describe('ManageTab', () => {
  beforeEach(() => {
    Object.values(admin).forEach((f) => f.mockReset?.())
    admin.entries.mockImplementation((cat: string) => Promise.resolve(cat === 'animes' ? [serie] : []))
    admin.updateEntry.mockResolvedValue({ ref: 'topic:7' })
    admin.createEntry.mockResolvedValue({ ref: 'manga:Pasta X' })
    admin.imageFolders.mockResolvedValue([{ dir: 'Pasta X', pages: 30 }])
    admin.searchMeta.mockResolvedValue([{ source: 'kitsu', title: 'Serie Exemplo', year: 2019, synopsis: 'Sinopse da fonte.', genres: ['Ação'], score: 80, cover: 'https://img/c.jpg' }])
  })

  it('busca dados numa fonte pública e salva a ficha', async () => {
    render(<ManageTab />)
    await userEvent.click(await screen.findByRole('button', { name: 'Editar Serie Exemplo' }))
    await userEvent.selectOptions(screen.getByLabelText('Fonte'), 'kitsu')
    await userEvent.click(screen.getByRole('button', { name: 'Buscar' }))
    expect(admin.searchMeta).toHaveBeenCalledWith('kitsu', 'anime', 'Serie Exemplo')
    await userEvent.click(await screen.findByRole('button', { name: 'Usar Serie Exemplo' }))
    expect(screen.getByLabelText('Sinopse')).toHaveValue('Sinopse da fonte.')
    await userEvent.type(screen.getByLabelText(/Outros nomes/), 'SE')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect(admin.updateEntry).toHaveBeenCalledWith(expect.objectContaining({
      ref: 'topic:7', aliases: ['SE'],
      meta: expect.objectContaining({ source: 'kitsu', synopsis: 'Sinopse da fonte.', year: 2019, score: 80, genres: ['Ação'] }),
    }))
  })

  it('cadastra um mangá a partir de uma pasta', async () => {
    render(<ManageTab />)
    await userEvent.click(await screen.findByRole('tab', { name: 'Mangás' }))
    await userEvent.click(screen.getByRole('button', { name: /Novo mangá/ }))
    await userEvent.click(await within(screen.getByRole('list', { name: 'Pastas' })).findByRole('button', { name: /Pasta X/ }))
    expect(screen.getByLabelText('Título')).toHaveValue('Pasta X')
    await userEvent.click(screen.getByRole('button', { name: 'Salvar' }))
    expect(admin.createEntry).toHaveBeenCalledWith(expect.objectContaining({ cat: 'mangas', dir: 'Pasta X', title: 'Pasta X' }))
  })

  it('exclui com confirmação', async () => {
    admin.deleteEntry.mockResolvedValue({})
    render(<ManageTab />)
    await userEvent.click(await screen.findByRole('button', { name: 'Editar Serie Exemplo' }))
    await userEvent.click(screen.getByRole('button', { name: 'Excluir' }))
    expect(admin.deleteEntry).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Confirmar exclusão' }))
    expect(admin.deleteEntry).toHaveBeenCalledWith('topic:7')
  })
})
