import '@testing-library/jest-dom/vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { HostFolderPicker } from './HostFolderPicker'

vi.mock('@/lib/api', () => ({
  api: {
    browseHostFolders: vi.fn(),
  },
}))

const mockedApi = api as unknown as { browseHostFolders: ReturnType<typeof vi.fn> }

const drives = {
  path: '',
  parent: null,
  entries: [
    { name: 'C:\\', path: 'C:\\', selectable: false, reason: 'Raiz de drive não pode ser montada' },
    { name: 'E:\\', path: 'E:\\', selectable: false, reason: 'Raiz de drive não pode ser montada' },
  ],
}

const eDrive = {
  path: 'E:\\',
  parent: '',
  entries: [
    { name: 'Filmes', path: 'E:\\Filmes', selectable: true },
  ],
}

const filmes = {
  path: 'E:\\Filmes',
  parent: 'E:\\',
  entries: [
    { name: 'Series', path: 'E:\\Filmes\\Series', selectable: true },
  ],
}

const appData = {
  path: 'C:\\Users\\usuario\\AppData',
  parent: 'C:\\Users\\usuario',
  entries: [],
}

describe('HostFolderPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lista os drives ao abrir, sem permitir montar a lista de drives', async () => {
    mockedApi.browseHostFolders.mockResolvedValueOnce(drives)
    render(<HostFolderPicker onConfirm={vi.fn()} onCancel={vi.fn()} />)

    expect(await screen.findAllByText('Raiz de drive não pode ser montada')).toHaveLength(2)
    expect(mockedApi.browseHostFolders).toHaveBeenCalledWith(undefined)
    expect(screen.queryByRole('button', { name: 'Montar esta pasta' })).not.toBeInTheDocument()
  })

  it('navega por subpastas e preenche o nome com o basename da pasta atual', async () => {
    const user = userEvent.setup()
    mockedApi.browseHostFolders
      .mockResolvedValueOnce(drives)
      .mockResolvedValueOnce(eDrive)
      .mockResolvedValueOnce(filmes)
    render(<HostFolderPicker onConfirm={vi.fn()} onCancel={vi.fn()} />)

    await user.click(await screen.findByRole('button', { name: 'E:\\' }))
    await user.click(await screen.findByRole('button', { name: 'Filmes' }))

    expect(await screen.findByText('Series')).toBeInTheDocument()
    expect(mockedApi.browseHostFolders).toHaveBeenLastCalledWith('E:\\Filmes')
    expect(screen.getByLabelText('Nome da biblioteca')).toHaveValue('Filmes')
  })

  it('confirma sem enviar nome quando o usuário não editou o campo (helper aplica o padrão)', async () => {
    const user = userEvent.setup()
    const onConfirm = vi.fn().mockResolvedValue(true)
    mockedApi.browseHostFolders
      .mockResolvedValueOnce(drives)
      .mockResolvedValueOnce(eDrive)
      .mockResolvedValueOnce(filmes)
    render(<HostFolderPicker onConfirm={onConfirm} onCancel={vi.fn()} />)

    await user.click(await screen.findByRole('button', { name: 'E:\\' }))
    await user.click(await screen.findByRole('button', { name: 'Filmes' }))
    await screen.findByText('Series')
    await user.click(screen.getByRole('button', { name: 'Montar esta pasta' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith('E:\\Filmes', undefined))
  })

  it('envia o nome quando o usuário editou o campo', async () => {
    const user = userEvent.setup()
    const onConfirm = vi.fn().mockResolvedValue(true)
    mockedApi.browseHostFolders
      .mockResolvedValueOnce(drives)
      .mockResolvedValueOnce(eDrive)
      .mockResolvedValueOnce(filmes)
    render(<HostFolderPicker onConfirm={onConfirm} onCancel={vi.fn()} />)

    await user.click(await screen.findByRole('button', { name: 'E:\\' }))
    await user.click(await screen.findByRole('button', { name: 'Filmes' }))
    await screen.findByText('Series')
    await user.clear(screen.getByLabelText('Nome da biblioteca'))
    await user.type(screen.getByLabelText('Nome da biblioteca'), '  Meus Filmes  ')
    await user.click(screen.getByRole('button', { name: 'Montar esta pasta' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith('E:\\Filmes', 'Meus Filmes'))
  })

  it('desabilita a confirmação quando a pasta atual está bloqueada e mostra o motivo', async () => {
    const user = userEvent.setup()
    const blocked = {
      name: 'AppData',
      path: 'C:\\Users\\usuario\\AppData',
      selectable: false,
      reason: 'Pasta de sistema do usuário',
    }
    mockedApi.browseHostFolders
      .mockResolvedValueOnce({ ...drives, entries: [blocked] })
      .mockResolvedValueOnce(appData)
    render(<HostFolderPicker onConfirm={vi.fn()} onCancel={vi.fn()} />)

    await user.click(await screen.findByRole('button', { name: 'AppData' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Pasta de sistema do usuário')
    expect(screen.getByRole('button', { name: 'Montar esta pasta' })).toBeDisabled()
  })

  it('não confirma nome vazio', async () => {
    const user = userEvent.setup()
    const onConfirm = vi.fn()
    mockedApi.browseHostFolders
      .mockResolvedValueOnce(drives)
      .mockResolvedValueOnce(eDrive)
      .mockResolvedValueOnce(filmes)
    render(<HostFolderPicker onConfirm={onConfirm} onCancel={vi.fn()} />)

    await user.click(await screen.findByRole('button', { name: 'E:\\' }))
    await user.click(await screen.findByRole('button', { name: 'Filmes' }))
    await screen.findByText('Series')
    await user.clear(screen.getByLabelText('Nome da biblioteca'))
    await user.click(screen.getByRole('button', { name: 'Montar esta pasta' }))

    expect(onConfirm).not.toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('Informe um nome')
  })

  it('mostra erro do helper ao falhar a listagem', async () => {
    mockedApi.browseHostFolders.mockRejectedValueOnce({
      response: { status: 503, data: { error: 'helper_unavailable' } },
    })
    render(<HostFolderPicker onConfirm={vi.fn()} onCancel={vi.fn()} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Helper de montagem offline')
  })
})
