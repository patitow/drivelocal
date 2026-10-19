import '@testing-library/jest-dom/vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/__tests__/render'
import { library, type TopicInfo } from '@/lib/library'
import { CreatorActions, CreatorItemMenu } from './CreatorActions'

vi.mock('sonner', () => ({ toast: Object.assign(vi.fn(), { success: vi.fn(), error: vi.fn() }) }))
vi.mock('@/lib/library', async (orig) => {
  const real = await orig<typeof import('@/lib/library')>()
  return {
    ...real,
    library: {
      ...real.library,
      suggestTopic: vi.fn(),
      admin: { ...real.library.admin, acceptTopicDupes: vi.fn(), dissolveTopic: vi.fn(), renameTopic: vi.fn(), setItem: vi.fn() },
    },
  }
})

type Fn = ReturnType<typeof vi.fn>
const lib = library as unknown as { suggestTopic: Fn; admin: { acceptTopicDupes: Fn; dissolveTopic: Fn; renameTopic: Fn; setItem: Fn } }

const topic = (id: number, title: string, count: number): TopicInfo => ({
  id, kind: 'creator', slug: title.toLowerCase(), title, description: '', source: 'auto', count, watched: 0, favorite: false, aliases: [],
})
const a = topic(1, 'Autor Alfa', 3)
const all = [a, topic(2, 'Autor Beta', 10), topic(3, 'Outro Nome', 1)]

describe('CreatorActions', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('admin junta com outro criador, ficando com o nome escolhido', async () => {
    lib.admin.acceptTopicDupes.mockResolvedValue({ batchId: 'b1', merged: 1 })
    const changed = vi.fn()
    render(<CreatorActions t={a} all={all} isAdmin onChanged={changed} onRefresh={vi.fn()} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Mais ações' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Juntar com outro criador/ }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Buscar criador' }), 'beta')
    expect(screen.queryByText('Outro Nome')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Autor Beta/ }))
    // padrão: fica o nome com mais itens
    expect(screen.getByRole('radio', { name: /Autor Beta/ })).toBeChecked()
    await userEvent.click(screen.getByRole('radio', { name: /Autor Alfa/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Juntar' }))
    expect(lib.admin.acceptTopicDupes).toHaveBeenCalledWith(1, [2])
    await waitFor(() => expect(changed).toHaveBeenCalled())
  })

  it('quem não é admin só sugere', async () => {
    lib.suggestTopic.mockResolvedValue({})
    render(<CreatorActions t={a} all={all} isAdmin={false} onChanged={vi.fn()} onRefresh={vi.fn()} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Mais ações' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Não é um criador válido/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Enviar sugestão' }))
    expect(lib.suggestTopic).toHaveBeenCalledWith(1, 'invalid')
    expect(lib.admin.dissolveTopic).not.toHaveBeenCalled()
  })

  it('admin renomeia; quem não é admin nem vê a opção', async () => {
    lib.admin.renameTopic.mockResolvedValue({})
    const refresh = vi.fn()
    const { unmount } = render(<CreatorActions t={a} all={all} isAdmin onChanged={vi.fn()} onRefresh={refresh} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Mais ações' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Renomear/ }))
    const input = screen.getByRole('textbox', { name: 'Nome do criador' })
    await userEvent.clear(input)
    await userEvent.type(input, 'Autor Gama{Enter}')
    expect(lib.admin.renameTopic).toHaveBeenCalledWith(1, 'Autor Gama')
    await waitFor(() => expect(refresh).toHaveBeenCalled())
    unmount()
    render(<CreatorActions t={a} all={all} isAdmin={false} onChanged={vi.fn()} onRefresh={vi.fn()} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Mais ações' }))
    expect(screen.queryByRole('menuitem', { name: /Renomear/ })).not.toBeInTheDocument()
  })

  it('move um vídeo para outro criador ou para um novo', async () => {
    lib.admin.setItem.mockResolvedValue({ result: { batchId: 'c1' } })
    const changed = vi.fn()
    const item = { id: 77, title: 'video sintético', name: 'v.mp4' } as never
    render(<CreatorItemMenu item={item} t={a} all={all} onChanged={changed} />)
    await userEvent.click(await screen.findByRole('button', { name: /Mais ações: video sintético/ }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Buscar criador' }), 'beta')
    await userEvent.click(screen.getByRole('button', { name: /Autor Beta/ }))
    expect(lib.admin.setItem).toHaveBeenCalledWith(77, { topic: { id: 2 }, remove_topic: { id: 1 } })
    await waitFor(() => expect(changed).toHaveBeenCalled())

    await userEvent.click(screen.getByRole('button', { name: /Mais ações: video sintético/ }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Buscar criador' }), 'Pessoa Nova')
    await userEvent.click(screen.getByRole('button', { name: /Novo criador "Pessoa Nova"/ }))
    expect(lib.admin.setItem).toHaveBeenLastCalledWith(77, { topic: { kind: 'creator', title: 'Pessoa Nova' }, remove_topic: { id: 1 } })
  })
})
