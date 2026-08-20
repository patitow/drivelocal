export type ThumbnailStatus = 'ready' | 'pending' | 'missing' | 'unknown'

function normalizePath(path: string): string {
  return path.replace(/\\/g, '/')
}

const statuses = new Map<string, ThumbnailStatus>()
const listeners = new Set<() => void>()
let pollTimer: ReturnType<typeof setInterval> | null = null
let pollInFlight = false

function emit() {
  listeners.forEach((listener) => listener())
}

export function subscribeThumbnailStatuses(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function getThumbnailStatus(path: string): ThumbnailStatus {
  return statuses.get(normalizePath(path)) ?? 'unknown'
}

export function resetThumbnailStatuses() {
  statuses.clear()
  emit()
}

export async function prefetchThumbnails(paths: string[]): Promise<void> {
  const unique = [...new Set(paths.map(normalizePath).filter(Boolean))]
  if (unique.length === 0) {
    return
  }

  const { api } = await import('./api')
  for (let i = 0; i < unique.length; i += 80) {
    const chunk = unique.slice(i, i + 80)
    const response = await api.post<{ items: Array<{ path: string; status: ThumbnailStatus }> }>(
      '/api/thumbnails',
      { paths: chunk }
    )
    for (const item of response.items ?? []) {
      statuses.set(normalizePath(item.path), item.status)
    }
  }
  emit()
  ensurePendingPoll()
}

function ensurePendingPoll() {
  if (pollTimer) {
    return
  }
  pollTimer = setInterval(async () => {
    if (pollInFlight) {
      return
    }
    const pending = [...statuses.entries()]
      .filter(([, status]) => status === 'pending')
      .map(([path]) => path)
    if (pending.length === 0) {
      if (pollTimer) {
        clearInterval(pollTimer)
        pollTimer = null
      }
      return
    }
    pollInFlight = true
    try {
      await prefetchThumbnails(pending)
    } catch {
      // Keep polling; the next interval retries.
    } finally {
      pollInFlight = false
    }
  }, 2500)
}
