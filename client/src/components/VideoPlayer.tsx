import { useState, useRef, useEffect, useCallback } from 'react'
import { Play, Pause, Volume2, VolumeX, Maximize, Download, X, Loader2, ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { buildStreamUrlSync, buildDownloadUrlSync } from '@/lib/utils'
import { api } from '@/lib/api'
import { saveAs } from 'file-saver'
import { toast } from 'sonner'

interface VideoPlayerProps {
  file: {
    name: string
    path: string
    isImage?: boolean
    isVideo?: boolean
  }
  files: Array<{ name: string; path: string; isImage?: boolean; isVideo?: boolean }>
  currentIndex: number
  onClose: () => void
  onNavigate: (index: number) => void
}

export function VideoPlayer({ file, files, currentIndex, onClose, onNavigate }: VideoPlayerProps) {
  const [isPlaying, setIsPlaying] = useState(false)
  const [isMuted, setIsMuted] = useState(false)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [isLoading, setIsLoading] = useState(true)
  const [hasError, setHasError] = useState(false)
  const [hasStartedPlaying, setHasStartedPlaying] = useState(false) // Track if video has started playing
  const [showControls, setShowControls] = useState(true) // Control visibility of UI
  const [videoBox, setVideoBox] = useState<{ left: number; width: number } | null>(null)
  const controlsTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const swipeStartXRef = useRef<number | null>(null)
  const didRetryAuthRef = useRef(false)
  const resumeTimeRef = useRef(0)
  // Tracks the file currently shown so an in-flight token refresh can tell,
  // once it resolves, whether the user has since navigated to another file.
  const currentFilePathRef = useRef(file.path)
  currentFilePathRef.current = file.path

  const videoRef = useRef<HTMLVideoElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const videoContainerRef = useRef<HTMLDivElement>(null)

  // Reads the token fresh from cookie/localStorage on every render, so a
  // state update after refreshing the token rebuilds this with the new one.
  const streamUrl = buildStreamUrlSync(file.path)

  const canGoPrevious = currentIndex > 0
  const canGoNext = currentIndex < files.length - 1

  const handlePrevious = useCallback(() => {
    if (currentIndex > 0) {
      onNavigate(currentIndex - 1)
    }
  }, [currentIndex, onNavigate])

  const handleNext = useCallback(() => {
    if (currentIndex < files.length - 1) {
      onNavigate(currentIndex + 1)
    }
  }, [currentIndex, files.length, onNavigate])

  // Reset states when file changes
  useEffect(() => {
    setIsLoading(true)
    setHasError(false)
    setHasStartedPlaying(false)
    setCurrentTime(0)
    setDuration(0)
    setIsPlaying(false)
    setVideoBox(null)
    didRetryAuthRef.current = false
    resumeTimeRef.current = 0
  }, [file.path])

  // Compute the video's actual rendered box (object-fit: contain math) so the
  // controls bar can match a portrait/vertical video's width instead of
  // spanning the full letterboxed backdrop - otherwise clicking the seek bar
  // area beside a narrow video lands on the backdrop and closes the modal.
  const updateVideoBox = useCallback(() => {
    const video = videoRef.current
    const container = videoContainerRef.current
    if (!video || !container || !video.videoWidth || !video.videoHeight) return

    const containerWidth = container.clientWidth
    const containerHeight = container.clientHeight
    const videoRatio = video.videoWidth / video.videoHeight
    const containerRatio = containerWidth / containerHeight

    let displayWidth: number
    if (videoRatio > containerRatio) {
      displayWidth = containerWidth
    } else {
      displayWidth = containerHeight * videoRatio
    }

    setVideoBox({ left: (containerWidth - displayWidth) / 2, width: displayWidth })
  }, [])

  useEffect(() => {
    const container = videoContainerRef.current
    if (!container) return
    const observer = new ResizeObserver(() => updateVideoBox())
    observer.observe(container)
    return () => observer.disconnect()
  }, [updateVideoBox])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const handleLoadedMetadata = () => {
      setDuration(video.duration)
      updateVideoBox()
      if (resumeTimeRef.current > 0) {
        video.currentTime = resumeTimeRef.current
        resumeTimeRef.current = 0
        video.play().catch(() => {})
      }
    }

    const handleTimeUpdate = () => {
      setCurrentTime(video.currentTime)
    }

    const handleEnded = () => {
      setIsPlaying(false)
    }

    const handleError = () => {
      // <video src> can't go through the axios 401-refresh interceptor, so an
      // expired access token just fails the element forever. Try one silent
      // token refresh and src reload (resuming at the same position) before
      // surfacing the error to the user.
      if (!didRetryAuthRef.current) {
        didRetryAuthRef.current = true
        resumeTimeRef.current = video.currentTime
        const erroredFilePath = file.path
        api.refreshToken().then((refreshed) => {
          // The user may have navigated to a different file while the
          // refresh was in flight - don't clobber its <video> with this one.
          if (currentFilePathRef.current !== erroredFilePath) {
            return
          }
          if (refreshed) {
            setHasError(false)
            setIsLoading(true)
            video.src = buildStreamUrlSync(file.path)
            video.load()
          } else {
            setIsLoading(false)
            setHasError(true)
            toast.error('Failed to load video')
          }
        })
        return
      }
      setIsLoading(false)
      setHasError(true)
      toast.error('Failed to load video')
    }

    const handleLoadStart = () => {
      setIsLoading(true)
      setHasError(false)
    }

    const handleCanPlay = () => {
      // Only hide loading if video hasn't started playing yet
      if (!hasStartedPlaying) {
        setIsLoading(false)
      }
    }

    const handleLoadedData = () => {
      // Only hide loading if video hasn't started playing yet
      if (!hasStartedPlaying) {
        setIsLoading(false)
      }
    }

    const handleWaiting = () => {
      // Only show loading if video hasn't started playing yet
      // During playback, buffering should not show loading overlay
      if (!hasStartedPlaying) {
        setIsLoading(true)
      }
    }

    const handlePlaying = () => {
      setHasStartedPlaying(true)
      setIsLoading(false)
    }

    video.addEventListener('loadedmetadata', handleLoadedMetadata)
    video.addEventListener('timeupdate', handleTimeUpdate)
    video.addEventListener('ended', handleEnded)
    video.addEventListener('error', handleError)
    video.addEventListener('loadstart', handleLoadStart)
    video.addEventListener('canplay', handleCanPlay)
    video.addEventListener('loadeddata', handleLoadedData)
    video.addEventListener('waiting', handleWaiting)
    video.addEventListener('playing', handlePlaying)

    return () => {
      video.removeEventListener('loadedmetadata', handleLoadedMetadata)
      video.removeEventListener('timeupdate', handleTimeUpdate)
      video.removeEventListener('ended', handleEnded)
      video.removeEventListener('error', handleError)
      video.removeEventListener('loadstart', handleLoadStart)
      video.removeEventListener('canplay', handleCanPlay)
      video.removeEventListener('loadeddata', handleLoadedData)
      video.removeEventListener('waiting', handleWaiting)
      video.removeEventListener('playing', handlePlaying)
    }
  }, [streamUrl, hasStartedPlaying])

  const togglePlay = () => {
    const video = videoRef.current
    if (!video) return

    if (isPlaying) {
      video.pause()
    } else {
      video.play()
    }
    setIsPlaying(!isPlaying)
  }

  const toggleMute = () => {
    const video = videoRef.current
    if (!video) return

    video.muted = !isMuted
    setIsMuted(!isMuted)
  }

  const handleVolumeChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const video = videoRef.current
    if (!video) return

    const newVolume = parseFloat(e.target.value)
    video.volume = newVolume
    setVolume(newVolume)
    setIsMuted(newVolume === 0)
  }

  const handleSeek = (e: React.ChangeEvent<HTMLInputElement>) => {
    const video = videoRef.current
    if (!video) return

    const newTime = parseFloat(e.target.value)
    video.currentTime = newTime
    setCurrentTime(newTime)
  }

  const toggleFullscreen = async () => {
    try {
      if (!document.fullscreenElement) {
        // Request fullscreen on the video container (better than Card)
        const element = videoContainerRef.current || containerRef.current
        if (element) {
          // Try standard API first
          if (element.requestFullscreen) {
            await element.requestFullscreen()
          } else if ((element as any).webkitRequestFullscreen) {
            // Safari
            await (element as any).webkitRequestFullscreen()
          } else if ((element as any).mozRequestFullScreen) {
            // Firefox
            await (element as any).mozRequestFullScreen()
          } else if ((element as any).msRequestFullscreen) {
            // IE/Edge
            await (element as any).msRequestFullscreen()
          }
        }
      } else {
        // Exit fullscreen
        if (document.exitFullscreen) {
          await document.exitFullscreen()
        } else if ((document as any).webkitExitFullscreen) {
          await (document as any).webkitExitFullscreen()
        } else if ((document as any).mozCancelFullScreen) {
          await (document as any).mozCancelFullScreen()
        } else if ((document as any).msExitFullscreen) {
          await (document as any).msExitFullscreen()
        }
      }
    } catch (error) {
      if (import.meta.env.DEV) {
        console.error('Error toggling fullscreen:', error)
      }
      toast.error('Não foi possível entrar em tela cheia')
    }
  }


  const handleDownload = () => {
    const downloadUrl = buildDownloadUrlSync(file.path)
    saveAs(downloadUrl, file.name)
  }

  const formatTime = (time: number) => {
    const minutes = Math.floor(time / 60)
    const seconds = Math.floor(time % 60)
    return `${minutes}:${seconds.toString().padStart(2, '0')}`
  }

  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    // Close if clicking on the backdrop (not on the card content)
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  // Show controls when mouse moves, hide after 3 seconds
  const showControlsTemporarily = useCallback(() => {
    setShowControls(true)
    if (controlsTimeoutRef.current) {
      clearTimeout(controlsTimeoutRef.current)
    }
    controlsTimeoutRef.current = setTimeout(() => {
      setShowControls(false)
    }, 3000)
  }, [])

  // Handle mouse movement to show controls. Listens on the whole modal
  // (not just the video area) because the header floats on top as a sibling
  // element - movement over it wouldn't bubble into a video-scoped listener,
  // which made the header (and its close button) hide mid-hover.
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const handleMouseMove = () => {
      showControlsTemporarily()
    }

    const handleMouseLeave = () => {
      setShowControls(false)
      if (controlsTimeoutRef.current) {
        clearTimeout(controlsTimeoutRef.current)
      }
    }

    container.addEventListener('mousemove', handleMouseMove)
    const canHover = window.matchMedia('(hover: hover)').matches
    if (canHover) {
      container.addEventListener('mouseleave', handleMouseLeave)
    }

    return () => {
      container.removeEventListener('mousemove', handleMouseMove)
      container.removeEventListener('mouseleave', handleMouseLeave)
      if (controlsTimeoutRef.current) {
        clearTimeout(controlsTimeoutRef.current)
      }
    }
  }, [showControlsTemporarily])

  // Handle keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Escape always closes the modal, regardless of focus
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }

      // Check if focus is on video controls (input range, button, etc.)
      const activeElement = document.activeElement
      const isVideoControlFocused = activeElement?.tagName === 'INPUT' ||
                                   activeElement?.tagName === 'BUTTON' ||
                                   activeElement?.closest('button') !== null ||
                                   activeElement?.closest('input[type="range"]') !== null ||
                                   activeElement?.closest('[role="slider"]') !== null

      const video = videoRef.current
      if (!video) return

      switch (e.key) {
        case ' ': // Spacebar - play/pause
          if (isVideoControlFocused) {
            return
          }
          e.preventDefault()
          togglePlay()
          showControlsTemporarily()
          break
        case 'ArrowUp': // Increase volume
          if (isVideoControlFocused) {
            return
          }
          e.preventDefault()
          const newVolumeUp = Math.min(1, video.volume + 0.1)
          video.volume = newVolumeUp
          setVolume(newVolumeUp)
          setIsMuted(false)
          showControlsTemporarily()
          break
        case 'ArrowDown': // Decrease volume
          if (isVideoControlFocused) {
            return
          }
          e.preventDefault()
          const newVolumeDown = Math.max(0, video.volume - 0.1)
          video.volume = newVolumeDown
          setVolume(newVolumeDown)
          setIsMuted(newVolumeDown === 0)
          showControlsTemporarily()
          break
        case 'ArrowLeft':
          if (isVideoControlFocused) {
            return
          }
          e.preventDefault()
          const newTimeBack = Math.max(0, video.currentTime - 5)
          video.currentTime = newTimeBack
          setCurrentTime(newTimeBack)
          showControlsTemporarily()
          break
        case 'ArrowRight':
          if (isVideoControlFocused) {
            return
          }
          e.preventDefault()
          const newTimeForward = Math.min(video.duration || 0, video.currentTime + 5)
          video.currentTime = newTimeForward
          setCurrentTime(newTimeForward)
          showControlsTemporarily()
          break
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose, showControlsTemporarily])

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex flex-col overflow-hidden bg-black/90"
      onClick={handleBackdropClick}
      onTouchStart={(e) => {
        showControlsTemporarily()
        const target = e.target as HTMLElement
        if (target.closest('input, button')) {
          swipeStartXRef.current = null
          return
        }
        if (e.touches.length === 1) {
          swipeStartXRef.current = e.touches[0].clientX
        }
      }}
      onTouchEnd={(e) => {
        if (swipeStartXRef.current === null) {
          return
        }
        const endX = e.changedTouches[0]?.clientX
        const startX = swipeStartXRef.current
        swipeStartXRef.current = null
        if (endX === undefined) {
          return
        }
        const dx = endX - startX
        if (Math.abs(dx) < 60) {
          return
        }
        if (dx < 0 && canGoNext) {
          handleNext()
        } else if (dx > 0 && canGoPrevious) {
          handlePrevious()
        }
      }}
    >
      {/* Header - floats over the video, fades with the other controls */}
      <div
        className={`absolute inset-x-0 top-0 z-20 flex items-center justify-between gap-2 bg-gradient-to-b from-black/70 to-transparent p-2 pt-[max(0.5rem,env(safe-area-inset-top))] transition-opacity duration-300 sm:p-4 ${
          showControls ? 'opacity-100' : 'opacity-0 pointer-events-none'
        }`}
      >
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-white sm:text-lg">{file.name}</h3>
        <div className="flex items-center gap-1 sm:gap-2">
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Baixar" onClick={handleDownload}>
            <Download className="w-4 h-4" />
          </Button>
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Fechar" onClick={onClose}>
            <X className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Video */}
      <div
        ref={videoContainerRef}
        className="relative flex flex-1 items-center justify-center overflow-hidden"
        onClick={handleBackdropClick}
      >
            {/* Navigation Buttons - Only show when controls are visible */}
            {canGoPrevious && showControls && (
              <Button
                variant="ghost"
                size="lg"
                className="absolute left-4 top-1/2 -translate-y-1/2 z-10 bg-black/40 hover:bg-black/60 text-white border-0 shadow-lg backdrop-blur-sm transition-all duration-300 p-3"
                onClick={handlePrevious}
                aria-label="Anterior"
              >
                <ChevronLeft className="w-6 h-6" />
              </Button>
            )}
            {canGoNext && showControls && (
              <Button
                variant="ghost"
                size="lg"
                className="absolute right-4 top-1/2 -translate-y-1/2 z-10 bg-black/40 hover:bg-black/60 text-white border-0 shadow-lg backdrop-blur-sm transition-all duration-300 p-3"
                onClick={handleNext}
                aria-label="Próximo"
              >
                <ChevronRight className="w-6 h-6" />
              </Button>
            )}

            {isLoading && !hasError && (
              <div className="absolute inset-0 flex items-center justify-center z-10 bg-black/50">
                <div className="flex flex-col items-center gap-4">
                  <Loader2 className="w-12 h-12 animate-spin text-white" />
                  <p className="text-white text-sm">Carregando vídeo...</p>
                </div>
              </div>
            )}
            
            {hasError && (
              <div className="absolute inset-0 flex items-center justify-center z-10 bg-black/50">
                <div className="flex flex-col items-center gap-4">
                  <p className="text-white text-sm">Erro ao carregar vídeo</p>
                  <Button variant="outline" onClick={() => {
                    setHasError(false)
                    setIsLoading(true)
                    didRetryAuthRef.current = false
                    if (videoRef.current) {
                      videoRef.current.src = buildStreamUrlSync(file.path)
                      videoRef.current.load()
                    }
                  }}>
                    Tentar novamente
                  </Button>
                </div>
              </div>
            )}
            
            <video
              ref={videoRef}
              src={streamUrl}
              className="h-auto w-auto"
              style={{
                maxHeight: '100%',
                maxWidth: '100%',
                objectFit: 'contain'
              }}
              onClick={(e) => {
                e.stopPropagation()
                togglePlay()
              }}
              controls={false}
              preload="metadata"
            />
            
            {/* Controls Overlay - matches the video's own rendered width so it
                doesn't overhang into the backdrop next to a portrait video */}
            <div
              className={`absolute bottom-0 bg-gradient-to-t from-black/60 to-transparent p-4 pb-[max(1rem,env(safe-area-inset-bottom))] transition-all duration-300 ${
                isLoading || !showControls ? 'opacity-0 pointer-events-none' : 'opacity-100'
              }`}
              style={videoBox ? { left: videoBox.left, width: videoBox.width } : { left: 0, right: 0 }}
              onMouseMove={showControlsTemporarily}
              onClick={(e) => e.stopPropagation()}
            >
              {/* Progress Bar */}
              <div className="mb-2">
                <input
                  type="range"
                  min="0"
                  max={duration || 0}
                  value={currentTime}
                  onChange={handleSeek}
                  aria-label="Posição do vídeo"
                  className="touch-range w-full cursor-pointer appearance-none bg-transparent"
                />
              </div>

              {/* Controls */}
              <div className="flex items-center justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2 sm:gap-3">
                  <Button 
                    variant="ghost" 
                    size="icon" 
                    onClick={togglePlay}
                    aria-label={isPlaying ? 'Pausar' : 'Reproduzir'}
                    className="text-white hover:bg-white/20 hover:text-white"
                  >
                    {isPlaying ? <Pause className="w-5 h-5" /> : <Play className="w-5 h-5" />}
                  </Button>
                  
                  <span className="text-white/90 text-xs font-medium sm:text-sm">
                    {formatTime(currentTime)} / {formatTime(duration)}
                  </span>
                </div>

                <div className="flex items-center gap-1 sm:gap-3">
                  <Button 
                    variant="ghost" 
                    size="icon" 
                    onClick={toggleMute}
                    aria-label={isMuted ? 'Ativar som' : 'Silenciar'}
                    className="text-white hover:bg-white/20 hover:text-white"
                  >
                    {isMuted ? <VolumeX className="w-5 h-5" /> : <Volume2 className="w-5 h-5" />}
                  </Button>
                  
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.05"
                    value={volume}
                    onChange={handleVolumeChange}
                    aria-label="Volume"
                    className="touch-range hidden w-20 cursor-pointer appearance-none bg-transparent sm:block sm:w-24"
                  />
                  
                  <Button 
                    variant="ghost" 
                    size="icon" 
                    onClick={toggleFullscreen}
                    aria-label="Tela cheia"
                    className="text-white hover:bg-white/20 hover:text-white"
                  >
                    <Maximize className="w-5 h-5" />
                  </Button>
                </div>
              </div>
            </div>
          </div>
    </div>
  )
}
