import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SERVER_RESTORED_EVENT, isServerUnavailableError, waitForServer } from './reconnect'

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('isServerUnavailableError', () => {
  it('considera erro de rede (sem resposta) como indisponibilidade', () => {
    expect(isServerUnavailableError({ message: 'Network Error' })).toBe(true)
  })

  it('considera 502, 503 e 504 como indisponibilidade', () => {
    for (const status of [502, 503, 504]) {
      expect(isServerUnavailableError({ response: { status, data: {} } })).toBe(true)
    }
  })

  it('ignora helper_unavailable (helper de montagem offline não derruba a conexão)', () => {
    expect(
      isServerUnavailableError({ response: { status: 503, data: { error: 'helper_unavailable' } } })
    ).toBe(false)
  })

  it('ignora requisições canceladas, 401 e 404', () => {
    expect(isServerUnavailableError({ code: 'ERR_CANCELED' })).toBe(false)
    expect(isServerUnavailableError({ response: { status: 401, data: {} } })).toBe(false)
    expect(isServerUnavailableError({ response: { status: 404, data: {} } })).toBe(false)
  })

  it('retorna false para valores que não são erro', () => {
    expect(isServerUnavailableError(null)).toBe(false)
    expect(isServerUnavailableError('boom')).toBe(false)
  })
})

describe('waitForServer', () => {
  let fetchMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.useFakeTimers()
    fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('resolve true e dispara server:restored quando o bootId muda', async () => {
    fetchMock
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok', bootId: 'novo' }))
    const listener = vi.fn()
    window.addEventListener(SERVER_RESTORED_EVENT, listener)

    try {
      const promise = waitForServer({ prevBootId: 'antigo', intervalMs: 1000 })
      await vi.advanceTimersByTimeAsync(1000)

      await expect(promise).resolves.toBe(true)
      expect(listener).toHaveBeenCalledTimes(1)
    } finally {
      window.removeEventListener(SERVER_RESTORED_EVENT, listener)
    }
  })

  it('continua tentando enquanto o bootId for o mesmo', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse({ status: 'ok', bootId: 'antigo' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok', bootId: 'antigo' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok', bootId: 'novo' }))

    const promise = waitForServer({ prevBootId: 'antigo', intervalMs: 1000 })
    await vi.advanceTimersByTimeAsync(2000)

    await expect(promise).resolves.toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('sem prevBootId, qualquer /api/health 200 conta como restaurado', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: 'ok' }))

    await expect(waitForServer()).resolves.toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('resolve false quando o tempo esgota, sem disparar server:restored', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'))
    const listener = vi.fn()
    window.addEventListener(SERVER_RESTORED_EVENT, listener)

    try {
      const promise = waitForServer({ prevBootId: 'antigo', timeoutMs: 5000, intervalMs: 1000 })
      await vi.advanceTimersByTimeAsync(10000)

      await expect(promise).resolves.toBe(false)
      expect(listener).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener(SERVER_RESTORED_EVENT, listener)
    }
  })
})
