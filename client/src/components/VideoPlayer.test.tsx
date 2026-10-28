import { describe, it, expect, vi, beforeEach } from 'vitest'
import '@testing-library/jest-dom/vitest'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
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

  it('no modo direto, cabeçalho mais longo que vídeo e áudio não estica a barra', async () => {
    // legenda com tempo errado faz o mkv anunciar 45:39; as faixas de vídeo e áudio têm 23:56
    vi.mocked(api.getMediaInfo).mockResolvedValue({ duration: 1436, audio: [], subtitles: [], fonts: [] } as never)
    renderPlayer()
    const video = document.querySelector('video') as HTMLVideoElement
    await waitFor(() => expect(video.src).toContain('token='))
    await waitFor(() => expect(api.getMediaInfo).toHaveBeenCalled())
    Object.defineProperty(video, 'duration', { configurable: true, value: 2739 })
    fireEvent.loadedMetadata(video)
    await waitFor(() => expect(document.body.textContent).toContain('/ 23:56'))
    expect(document.body.textContent).not.toContain('45:39')
  })

  it('barra mostra o quanto do vídeo já carregou', async () => {
    vi.mocked(api.getMediaInfo).mockResolvedValue({ duration: 1000, audio: [], subtitles: [], fonts: [] } as never)
    renderPlayer()
    const video = document.querySelector('video') as HTMLVideoElement
    await waitFor(() => expect(api.getMediaInfo).toHaveBeenCalled())
    Object.defineProperty(video, 'duration', { configurable: true, value: 1000 })
    fireEvent.loadedMetadata(video)
    // dois trechos carregados; vale o que contém a posição atual
    Object.defineProperty(video, 'buffered', { configurable: true, value: { length: 2, start: (i: number) => [0, 500][i], end: (i: number) => [120, 900][i] } })
    Object.defineProperty(video, 'currentTime', { configurable: true, writable: true, value: 600 })
    fireEvent.progress(video)
    await waitFor(() => expect(screen.getByTestId('buffered')).toHaveStyle({ width: '90%' }))
  })

  it('celular: toque pausa como no computador; toque duplo nas laterais pula 10 s sem pausar', async () => {
    renderPlayer()
    const video = document.querySelector('video') as HTMLVideoElement
    await waitFor(() => expect(video.src).toContain('token='))
    video.getBoundingClientRect = () => ({ left: 0, top: 0, width: 300, height: 200, right: 300, bottom: 200, x: 0, y: 0, toJSON: () => ({}) })
    Object.defineProperty(video, 'paused', { configurable: true, get: () => false })
    fireEvent.canPlay(video)
    fireEvent.playing(video)
    const tap = (x: number) => { fireEvent.touchStart(video); fireEvent.click(video, { clientX: x }) }

    tap(280)
    tap(280)
    expect(await screen.findByTestId('seek-flash')).toHaveTextContent('+10 s')
    tap(20)
    tap(20)
    await waitFor(() => expect(screen.getByTestId('seek-flash')).toHaveTextContent('−10 s'))
    await new Promise((r) => setTimeout(r, 400))
    expect(HTMLMediaElement.prototype.pause).not.toHaveBeenCalled()

    tap(150) // meio: pausa na hora
    expect(HTMLMediaElement.prototype.pause).toHaveBeenCalledTimes(1)
    expect(screen.queryByTestId('tap-overlay')).not.toBeInTheDocument()
  })

  it('celular: arrastar para o lado não troca de episódio', async () => {
    const onNavigate = vi.fn()
    const other = { ...file, name: 'b.mkv', path: 'Filmes/b.mkv' }
    render(<VideoPlayer file={file} files={[file, other]} currentIndex={0} onClose={() => {}} onNavigate={onNavigate} />)
    const video = document.querySelector('video') as HTMLVideoElement
    fireEvent.touchStart(video, { touches: [{ clientX: 250 }] })
    fireEvent.touchEnd(video, { changedTouches: [{ clientX: 20 }] })
    expect(onNavigate).not.toHaveBeenCalled()
  })
})
