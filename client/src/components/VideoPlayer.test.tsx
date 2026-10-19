import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, fireEvent, waitFor } from '@testing-library/react'
import { VideoPlayer } from './VideoPlayer'

vi.mock('@/lib/api', () => ({
  api: {
    refreshToken: vi.fn(),
    getHealth: vi.fn(),
    getMediaInfo: vi.fn(),
    getSubtitleVtt: vi.fn(),
    getKeyframe: vi.fn(),
  },
}))
vi.mock('@/hooks/usePlaybackProgress', () => ({
  usePlaybackProgress: () => ({ ready: true, resumeAt: null, prefs: null, save: vi.fn() }),
}))
vi.mock('sonner', () => ({ toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn(), info: vi.fn() }) }))

import { api } from '@/lib/api'
import { toast } from 'sonner'

const file = { name: 'movie.mkv', path: 'Filmes/movie.mkv', isVideo: true }

function renderPlayer() {
  return render(<VideoPlayer file={file} files={[file]} currentIndex={0} onClose={() => {}} onNavigate={() => {}} />)
}

// O token vai na URL do <video>; se expirou, o player renova uma vez e recarrega no mesmo modo.
describe('VideoPlayer: token expirado', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    localStorage.setItem('auth_token', 'velho')
    window.HTMLMediaElement.prototype.load = vi.fn()
    window.HTMLMediaElement.prototype.play = vi.fn().mockResolvedValue(undefined)
    window.HTMLMediaElement.prototype.pause = vi.fn()
    window.ResizeObserver = vi.fn().mockImplementation(() => ({ observe: vi.fn(), unobserve: vi.fn(), disconnect: vi.fn() }))
    window.matchMedia = vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })
    ;(globalThis as { MediaError?: unknown }).MediaError ??= { MEDIA_ERR_ABORTED: 1 }
    vi.mocked(api.getHealth).mockResolvedValue({} as never)
    vi.mocked(api.getMediaInfo).mockResolvedValue({ audio: [], subtitles: [], fonts: [] } as never)
  })

  it('renova o token uma vez e recarrega com o token novo', async () => {
    vi.mocked(api.refreshToken).mockImplementation(async () => {
      localStorage.setItem('auth_token', 'novo')
      return true
    })
    renderPlayer()
    const video = document.querySelector('video') as HTMLVideoElement
    await waitFor(() => expect(video.src).toContain('token=velho'))
    fireEvent.error(video)
    await waitFor(() => expect(api.refreshToken).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(video.src).toContain('token=novo'))
    expect(video.src).not.toContain('mode=')
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('sem renovação, segue para o próximo modo', async () => {
    vi.mocked(api.refreshToken).mockResolvedValue(false)
    renderPlayer()
    const video = document.querySelector('video') as HTMLVideoElement
    await waitFor(() => expect(video.src).toContain('token='))
    fireEvent.error(video)
    await waitFor(() => expect(api.refreshToken).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(toast.info).toHaveBeenCalled())
  })
})
