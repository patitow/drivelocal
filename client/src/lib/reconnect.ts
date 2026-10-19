// Detecção de indisponibilidade do servidor e reconexão por polling de /api/health.
// Módulo sem dependência de api.ts (evita ciclo e facilita mock nos testes).

export const SERVER_UNAVAILABLE_EVENT = 'server:unavailable'
export const SERVER_RESTORED_EVENT = 'server:restored'

export interface WaitForServerOptions {
  /**
   * bootId conhecido antes da indisponibilidade. Só resolve com sucesso quando o
   * servidor responde com um bootId diferente (ou seja, reiniciou). Sem valor, basta
   * qualquer /api/health 200.
   */
  prevBootId?: string | null
  timeoutMs?: number
  intervalMs?: number
}

const PROBE_TIMEOUT_MS = 5000

/** Erro de rede/timeout ou 502/503/504 do servidor (exceto helper_unavailable do app). */
export function isServerUnavailableError(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const e = error as {
    code?: string
    response?: { status?: number; data?: { error?: string } }
  }
  if (e.code === 'ERR_CANCELED') return false
  if (!e.response) return true
  const status = e.response.status
  if (status === 502 || status === 503 || status === 504) {
    return e.response.data?.error !== 'helper_unavailable'
  }
  return false
}

function apiBaseUrl(): string {
  return import.meta.env.VITE_API_URL || window.location.origin
}

interface ProbeResult {
  ok: boolean
  bootId?: string
}

async function probeHealth(): Promise<ProbeResult> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS)
  try {
    const res = await fetch(`${apiBaseUrl()}/api/health`, {
      method: 'GET',
      cache: 'no-store',
      credentials: 'omit',
      signal: controller.signal,
    })
    if (!res.ok) return { ok: false }
    const body = (await res.json().catch(() => ({}))) as { bootId?: string }
    return { ok: true, bootId: typeof body.bootId === 'string' ? body.bootId : undefined }
  } catch {
    return { ok: false }
  } finally {
    clearTimeout(timer)
  }
}

/**
 * Faz polling de /api/health até o servidor voltar. Resolve true (e dispara
 * `server:restored`) quando responde 200 e, se prevBootId foi informado, o bootId mudou.
 * Resolve false quando o tempo esgota.
 */
export async function waitForServer(options: WaitForServerOptions = {}): Promise<boolean> {
  const { prevBootId, timeoutMs = 120000, intervalMs = 1500 } = options
  const deadline = Date.now() + timeoutMs

  while (true) {
    const probe = await probeHealth()
    if (probe.ok) {
      const restarted = prevBootId == null || (probe.bootId != null && probe.bootId !== prevBootId)
      if (restarted) {
        window.dispatchEvent(new Event(SERVER_RESTORED_EVENT))
        return true
      }
    }
    if (Date.now() + intervalMs > deadline) {
      return false
    }
    await new Promise<void>((resolve) => setTimeout(resolve, intervalMs))
  }
}
