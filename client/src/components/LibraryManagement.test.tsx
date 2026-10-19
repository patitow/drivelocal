import '@testing-library/jest-dom/vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { waitForServer } from '@/lib/reconnect'
import { toast } from 'sonner'
import { LibraryManagement } from './LibraryManagement'

vi.mock('@/lib/api', () => ({
  api: {
    getMounts: vi.fn(),
    getBootId: vi.fn(),
    addMount: vi.fn(),
    removeMount: vi.fn(),
    getMountJob: vi.fn(),
    browseHostFolders: vi.fn(),
  },
}))

vi.mock('@/lib/reconnect', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/reconnect')>()),
  waitForServer: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}))

const mockedApi = api as unknown as {
  getMounts: ReturnType<typeof vi.fn>
  getBootId: ReturnType<typeof vi.fn>
  addMount: ReturnType<typeof vi.fn>
  removeMount: ReturnType<typeof vi.fn>
  getMountJob: ReturnType<typeof vi.fn>
}
const mockedWaitForServer = waitForServer as unknown as ReturnType<typeof vi.fn>

const onlineMounts = {
  helper: { available: true, version: '1.0.0' },
  revision: 'rev1',
  mounts: [
    { name: 'Filmes', hostPath: 'E:\\Filmes', target: '/data/Filmes', legacy: false },
    { name: 'Series', hostPath: 'E:\\Series', target: '/data/Series', legacy: true },
  ],
  activeJob: null,
  containerMounts: ['/data/Filmes'],
}

// Contrato real: com o helper offline o servidor envia mounts: [] e só os montados no container.
const offlineMounts = {
  helper: { available: false },
  mounts: [],
  activeJob: null,
  containerMounts: ['/data/Filmes', '/data/Animes'],
}

describe('LibraryManagement', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    mockedApi.getBootId.mockResolvedValue('boot1')
    mockedWaitForServer.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('mostra ativa/pendente conforme os mounts do container e a marca de legado', async () => {
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    render(<LibraryManagement />)

    expect(await screen.findByText('Filmes')).toBeInTheDocument()
    expect(screen.getByText('Ativa')).toBeInTheDocument()
    expect(screen.getByText('Pendente')).toBeInTheDocument()
    expect(screen.getByText('Legado')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Adicionar pasta/ })).toBeEnabled()
  })

  it('com helper offline mostra aviso e desabilita as ações', async () => {
    mockedApi.getMounts.mockResolvedValue(offlineMounts)
    render(<LibraryManagement />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Helper de montagem offline')
    expect(screen.getByRole('button', { name: /Adicionar pasta/ })).toBeDisabled()
    expect(screen.queryByText('Lista indisponível', { exact: false })).not.toBeInTheDocument()
  })

  it('com helper offline mostra os containerMounts como lista somente leitura', async () => {
    mockedApi.getMounts.mockResolvedValue(offlineMounts)
    render(<LibraryManagement />)

    const list = await screen.findByRole('list', { name: 'Bibliotecas montadas (somente leitura)' })
    expect(within(list).getByText('/data/Filmes')).toBeInTheDocument()
    expect(within(list).getByText('/data/Animes')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Remover/ })).not.toBeInTheDocument()
  })

  it('marca como Ativa só os mounts cujo target está em containerMounts', async () => {
    mockedApi.getMounts.mockResolvedValue({ ...onlineMounts, containerMounts: ['/data/Series'] })
    render(<LibraryManagement />)

    expect(await screen.findByText('Filmes')).toBeInTheDocument()
    expect(screen.getByText('Ativa')).toBeInTheDocument()
    expect(screen.getByText('Pendente')).toBeInTheDocument()
  })

  it('tolera erros de rede durante o restart e confirma quando o job chega a done', async () => {
    const user = userEvent.setup()
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    mockedApi.removeMount.mockResolvedValue({ jobId: 'job3' })
    mockedApi.getMountJob
      .mockRejectedValueOnce(new Error('Network Error'))
      .mockResolvedValue({ id: 'job3', op: 'remove', name: 'Filmes', state: 'done', startedAt: '' })
    mockedWaitForServer.mockResolvedValue(true)
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Biblioteca "Filmes" removida'), {
      timeout: 5000,
    })
    expect(mockedApi.getMountJob).toHaveBeenCalledTimes(2)
  })

  it('job rolled_back sem troca de bootId mostra o erro real sem esperar o servidor', async () => {
    const user = userEvent.setup()
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    mockedApi.removeMount.mockResolvedValue({ jobId: 'job4' })
    mockedApi.getMountJob.mockResolvedValue({
      id: 'job4',
      op: 'remove',
      name: 'Filmes',
      state: 'rolled_back',
      error: 'mount falhou',
      startedAt: '',
    })
    // bootId não muda (container não foi recriado): o aguardo do servidor nunca termina.
    mockedWaitForServer.mockReturnValue(new Promise(() => {}))
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    await waitFor(
      () => expect(toast.error).toHaveBeenCalledWith('Alteração revertida: mount falhou'),
      { timeout: 5000 }
    )
    expect(toast.error).not.toHaveBeenCalledWith(expect.stringContaining('não voltou'))
    expect(toast.success).not.toHaveBeenCalled()
    expect(screen.queryByText(/reconectando/)).not.toBeInTheDocument()
  })

  it('job done só vira sucesso depois que o servidor volta', async () => {
    const user = userEvent.setup()
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    mockedApi.removeMount.mockResolvedValue({ jobId: 'job5' })
    mockedApi.getMountJob.mockResolvedValue({
      id: 'job5',
      op: 'remove',
      name: 'Filmes',
      state: 'done',
      startedAt: '',
    })
    mockedWaitForServer.mockResolvedValue(false)
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('O servidor não voltou a tempo. Recarregue a página.')
    )
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('não remove quando o usuário cancela a confirmação', async () => {
    const user = userEvent.setup()
    vi.mocked(window.confirm).mockReturnValue(false)
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    expect(window.confirm).toHaveBeenCalledWith(
      expect.stringContaining('reproduções em andamento serão interrompidas')
    )
    expect(mockedApi.removeMount).not.toHaveBeenCalled()
  })

  it('remove, aguarda o servidor reiniciar, acompanha o job e avisa sucesso', async () => {
    const user = userEvent.setup()
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    mockedApi.removeMount.mockResolvedValue({ jobId: 'job1' })
    mockedApi.getMountJob.mockResolvedValue({
      id: 'job1',
      op: 'remove',
      name: 'Filmes',
      state: 'done',
      startedAt: '',
    })
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith('Biblioteca "Filmes" removida')
    )
    expect(mockedApi.removeMount).toHaveBeenCalledWith('Filmes', 'rev1')
    expect(mockedWaitForServer).toHaveBeenCalledWith({ prevBootId: 'boot1' })
    expect(mockedApi.getMountJob).toHaveBeenCalledWith('job1')
    await waitFor(() => expect(mockedApi.getMounts).toHaveBeenCalledTimes(2))
  })

  it('informa alteração revertida quando o job termina em rolled_back', async () => {
    const user = userEvent.setup()
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    mockedApi.removeMount.mockResolvedValue({ jobId: 'job2' })
    mockedApi.getMountJob.mockResolvedValue({
      id: 'job2',
      op: 'remove',
      name: 'Filmes',
      state: 'rolled_back',
      error: 'docker compose falhou',
      startedAt: '',
    })
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith('Alteração revertida: docker compose falhou')
    )
  })

  it('em revision_conflict recarrega a lista e avisa', async () => {
    const user = userEvent.setup()
    mockedApi.getMounts.mockResolvedValue(onlineMounts)
    mockedApi.removeMount.mockRejectedValue({
      response: { status: 409, data: { error: 'revision_conflict' } },
    })
    render(<LibraryManagement />)

    await user.click((await screen.findAllByRole('button', { name: /Remover/ }))[0])

    await waitFor(() => expect(toast.warning).toHaveBeenCalled())
    expect(mockedApi.getMounts).toHaveBeenCalledTimes(2)
    expect(mockedWaitForServer).not.toHaveBeenCalled()
  })
})
