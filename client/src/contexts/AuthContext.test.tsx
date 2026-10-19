import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { SERVER_RESTORED_EVENT } from '@/lib/reconnect'
import { AuthProvider, useAuth } from './AuthContext'

vi.mock('@/lib/api', () => ({
  api: {
    checkHealth: vi.fn(),
    getAuthStatus: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    setToken: vi.fn(),
  },
}))

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const mockedApi = api as unknown as {
  checkHealth: ReturnType<typeof vi.fn>
  getAuthStatus: ReturnType<typeof vi.fn>
}

function wrapper({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>
}

const authenticatedStatus = { authenticated: true, username: 'joao', role: 'admin', permissions: [], authEnabled: true }

describe('AuthContext - reconexão', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedApi.checkHealth.mockResolvedValue({ status: 'ok', authEnabled: true })
  })

  it('erro de rede não desloga o usuário', async () => {
    mockedApi.getAuthStatus.mockResolvedValueOnce(authenticatedStatus)
    const { result } = renderHook(() => useAuth(), { wrapper })

    await waitFor(() => expect(result.current.isAuthenticated).toBe(true))

    mockedApi.getAuthStatus.mockRejectedValueOnce(new Error('Network Error'))
    await act(async () => {
      await result.current.checkAuth()
    })

    expect(result.current.isAuthenticated).toBe(true)
    expect(result.current.username).toBe('joao')
  })

  it('401 continua deslogando', async () => {
    mockedApi.getAuthStatus.mockResolvedValueOnce(authenticatedStatus)
    const { result } = renderHook(() => useAuth(), { wrapper })

    await waitFor(() => expect(result.current.isAuthenticated).toBe(true))

    mockedApi.getAuthStatus.mockRejectedValueOnce({ response: { status: 401 } })
    await act(async () => {
      await result.current.checkAuth()
    })

    expect(result.current.isAuthenticated).toBe(false)
  })

  it('server:restored refaz a verificação que falhou durante a indisponibilidade', async () => {
    mockedApi.getAuthStatus
      .mockRejectedValueOnce(new Error('Network Error'))
      .mockResolvedValueOnce(authenticatedStatus)
    const { result } = renderHook(() => useAuth(), { wrapper })

    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.isAuthenticated).toBe(false)

    act(() => {
      window.dispatchEvent(new Event(SERVER_RESTORED_EVENT))
    })

    await waitFor(() => expect(result.current.isAuthenticated).toBe(true))
    expect(mockedApi.getAuthStatus).toHaveBeenCalledTimes(2)
  })
})
