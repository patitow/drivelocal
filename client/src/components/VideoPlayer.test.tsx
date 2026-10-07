import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { VideoPlayer } from './VideoPlayer'

vi.mock('@/lib/api', () => ({
  api: {
    refreshToken: vi.fn(),
  },
}))

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

import { api } from '@/lib/api'
import { toast } from 'sonner'

const file = { name: 'movie.mkv', path: '/Animes/movie.mkv', isVideo: true }
const files = [file]

function renderPlayer() {
  return render(
    <VideoPlayer file={file} files={files} currentIndex={0} onClose={() => {}} onNavigate={() => {}} />
  )
}

describe('VideoPlayer - expired access token recovery', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    localStorage.setItem('auth_token', 'stale-token')
    // jsdom doesn't implement media loading or ResizeObserver; stub them so
    // the component can mount and <video> events can be driven manually.
    window.HTMLMediaElement.prototype.load = vi.fn()
    window.HTMLMediaElement.prototype.play = vi.fn().mockResolvedValue(undefined)
    window.HTMLMediaElement.prototype.pause = vi.fn()
    window.ResizeObserver = vi.fn().mockImplementation(() => ({
      observe: vi.fn(),
      unobserve: vi.fn(),
      disconnect: vi.fn(),
    }))
    window.matchMedia = vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })
  })

  it('refreshes the token and reloads the stream once on a load error, resuming at the same position', async () => {
    vi.mocked(api.refreshToken).mockResolvedValue(true)
    renderPlayer()

    const video = document.querySelector('video') as HTMLVideoElement
    expect(video.src).toContain('token=stale-token')

    Object.defineProperty(video, 'currentTime', { value: 42, writable: true })
    fireEvent.error(video)

    await waitFor(() => expect(api.refreshToken).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(video.load).toHaveBeenCalledTimes(1))

    // Error overlay must not be shown while the silent retry is in flight / after it succeeds.
    expect(screen.queryByText('Erro ao carregar vídeo')).toBeNull()

    // Once the reloaded stream reports metadata, playback resumes where it left off.
    Object.defineProperty(video, 'duration', { value: 120, writable: true })
    fireEvent.loadedMetadata(video)

    expect(video.currentTime).toBe(42)
    expect(video.play).toHaveBeenCalled()
  })

  it('only retries once per file and surfaces the error if the token refresh fails', async () => {
    vi.mocked(api.refreshToken).mockResolvedValue(false)
    renderPlayer()

    const video = document.querySelector('video') as HTMLVideoElement
    fireEvent.error(video)

    await waitFor(() => expect(api.refreshToken).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByText('Erro ao carregar vídeo')).not.toBeNull())
    expect(toast.error).toHaveBeenCalledWith('Failed to load video')

    // A second error on the same file must not trigger another refresh attempt.
    fireEvent.error(video)
    expect(api.refreshToken).toHaveBeenCalledTimes(1)
  })

  it('resets the retry guard when switching to a different file', async () => {
    vi.mocked(api.refreshToken).mockResolvedValue(false)
    const { rerender } = renderPlayer()

    let video = document.querySelector('video') as HTMLVideoElement
    fireEvent.error(video)
    await waitFor(() => expect(api.refreshToken).toHaveBeenCalledTimes(1))

    const otherFile = { name: 'other.mkv', path: '/Animes/other.mkv', isVideo: true }
    rerender(
      <VideoPlayer
        file={otherFile}
        files={[file, otherFile]}
        currentIndex={1}
        onClose={() => {}}
        onNavigate={() => {}}
      />
    )

    video = document.querySelector('video') as HTMLVideoElement
    fireEvent.error(video)
    await waitFor(() => expect(api.refreshToken).toHaveBeenCalledTimes(2))
  })
})
