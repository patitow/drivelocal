import React, { useState, useRef, useEffect, useCallback } from 'react'
import { X, ZoomIn, ZoomOut, RotateCw, Download, ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { buildStreamUrlSync, buildDownloadUrlSync } from '@/lib/utils'
import { saveAs } from 'file-saver'
import { toast } from 'sonner'

interface ImageViewerProps {
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

export function ImageViewer({ file, files, currentIndex, onClose, onNavigate }: ImageViewerProps) {
  const [scale, setScale] = useState(1)
  const [rotation, setRotation] = useState(0)
  const [isDragging, setIsDragging] = useState(false)
  const [dragStart, setDragStart] = useState({ x: 0, y: 0 })
  const [position, setPosition] = useState({ x: 0, y: 0 })
  const positionRef = useRef({ x: 0, y: 0 })
  const animationFrameRef = useRef<number | null>(null)
  const isDraggingRef = useRef(false)
  const didDragRef = useRef(false)
  const mouseDownPosRef = useRef({ x: 0, y: 0 })
  const pinchStartDist = useRef<number | null>(null)
  const pinchStartScale = useRef(1)
  const swipeStart = useRef<{ x: number; y: number } | null>(null)
  
  const imageRef = useRef<HTMLImageElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const imageContainerRef = useRef<HTMLDivElement>(null)

  const streamUrl = buildStreamUrlSync(file.path)

  const canGoPrevious = currentIndex > 0
  const canGoNext = currentIndex < files.length - 1

  const handleZoomIn = () => {
    setScale(prev => Math.min(prev * 1.2, 5))
  }

  const handleZoomOut = () => {
    setScale(prev => Math.max(prev / 1.2, 0.1))
  }

  const handleRotate = () => {
    setRotation(prev => (prev + 90) % 360)
  }

  const handlePrevious = useCallback(() => {
    if (currentIndex > 0) {
      onNavigate(currentIndex - 1)
      // Reset zoom and position when navigating
      setScale(1)
      setPosition({ x: 0, y: 0 })
      setRotation(0)
    }
  }, [currentIndex, onNavigate])

  const handleNext = useCallback(() => {
    if (currentIndex < files.length - 1) {
      onNavigate(currentIndex + 1)
      // Reset zoom and position when navigating
      setScale(1)
      setPosition({ x: 0, y: 0 })
      setRotation(0)
    }
  }, [currentIndex, files.length, onNavigate])

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Escape always closes the modal, regardless of focus
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }

      // Check if focus is on an input, textarea, or video controls
      const activeElement = document.activeElement
      const isInputFocused = activeElement?.tagName === 'INPUT' || 
                            activeElement?.tagName === 'TEXTAREA' ||
                            activeElement?.tagName === 'VIDEO' ||
                            activeElement?.closest('video') !== null ||
                            activeElement?.closest('[role="slider"]') !== null

      // Don't handle navigation keys if user is interacting with controls
      if (isInputFocused && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
        return
      }

      switch (e.key) {
        case '+':
        case '=':
          handleZoomIn()
          break
        case '-':
          handleZoomOut()
          break
        case 'r':
        case 'R':
          handleRotate()
          break
        case 'ArrowLeft':
          if (canGoPrevious) {
            e.preventDefault()
            handlePrevious()
          }
          break
        case 'ArrowRight':
          if (canGoNext) {
            e.preventDefault()
            handleNext()
          }
          break
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose, canGoPrevious, canGoNext, handlePrevious, handleNext])

  const handleReset = () => {
    setScale(1)
    setRotation(0)
    setPosition({ x: 0, y: 0 })
  }

  const handleDownload = () => {
    const downloadUrl = buildDownloadUrlSync(file.path)
    saveAs(downloadUrl, file.name)
    toast.success(`Downloaded ${file.name}`)
  }

  const handleMouseDown = (e: React.MouseEvent) => {
    if (e.button === 0) { // Left mouse button
      e.preventDefault() // Prevent default behavior
      isDraggingRef.current = true
      didDragRef.current = false
      mouseDownPosRef.current = { x: e.clientX, y: e.clientY }
      setIsDragging(true)
      setDragStart({ x: e.clientX - position.x, y: e.clientY - position.y })
    }
  }

  // Real clicks always carry a pixel or two of jitter between mousedown and
  // mouseup, so only count it as a drag past this threshold - otherwise a
  // deliberate click-to-close on the backdrop keeps getting swallowed.
  const dragThresholdPx = 5

  const handleMouseMove = (e: React.MouseEvent) => {
    if (!isDraggingRef.current) return
    e.preventDefault() // Prevent default behavior
    if (!didDragRef.current) {
      const dx = e.clientX - mouseDownPosRef.current.x
      const dy = e.clientY - mouseDownPosRef.current.y
      if (Math.hypot(dx, dy) > dragThresholdPx) {
        didDragRef.current = true
      }
    }

    // Drag sensitivity factor (lower = slower drag)
    const dragSensitivity = 0.6

    // Throttle position updates with requestAnimationFrame to reduce re-renders
    const nextX = (e.clientX - dragStart.x) * dragSensitivity
    const nextY = (e.clientY - dragStart.y) * dragSensitivity
    positionRef.current = { x: nextX, y: nextY }

    if (animationFrameRef.current === null) {
      animationFrameRef.current = requestAnimationFrame(() => {
        setPosition(positionRef.current)
        animationFrameRef.current = null
      })
    }
  }

  const handleMouseUp = () => {
    isDraggingRef.current = false
    setIsDragging(false)
    if (animationFrameRef.current !== null) {
      cancelAnimationFrame(animationFrameRef.current)
      animationFrameRef.current = null
    }
  }

  // Stop dragging even if mouseup happens outside the image container
  useEffect(() => {
    const handleWindowMouseUp = () => {
      isDraggingRef.current = false
      setIsDragging(false)
      if (animationFrameRef.current !== null) {
        cancelAnimationFrame(animationFrameRef.current)
        animationFrameRef.current = null
      }
    }
    window.addEventListener('mouseup', handleWindowMouseUp)
    return () => window.removeEventListener('mouseup', handleWindowMouseUp)
  }, [])

  const handleWheel = (e: React.WheelEvent) => {
    e.stopPropagation()
    const delta = e.deltaY > 0 ? 0.9 : 1.1
    setScale(prev => Math.max(0.1, Math.min(5, prev * delta)))
  }

  const touchDistance = (a: React.Touch, b: React.Touch) =>
    Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY)

  const handleTouchStart = (e: React.TouchEvent) => {
    if (e.touches.length === 2) {
      pinchStartDist.current = touchDistance(e.touches[0], e.touches[1])
      pinchStartScale.current = scale
      isDraggingRef.current = false
      swipeStart.current = null
      return
    }
    if (e.touches.length === 1) {
      const touch = e.touches[0]
      swipeStart.current = { x: touch.clientX, y: touch.clientY }
      if (scale > 1) {
        isDraggingRef.current = true
        didDragRef.current = false
        mouseDownPosRef.current = { x: touch.clientX, y: touch.clientY }
        setIsDragging(true)
        setDragStart({ x: touch.clientX - position.x, y: touch.clientY - position.y })
      }
    }
  }

  const handleTouchMove = (e: React.TouchEvent) => {
    if (e.touches.length === 2 && pinchStartDist.current) {
      const dist = touchDistance(e.touches[0], e.touches[1])
      const next = Math.max(0.1, Math.min(5, pinchStartScale.current * (dist / pinchStartDist.current)))
      setScale(next)
      return
    }
    if (e.touches.length === 1 && isDraggingRef.current) {
      const touch = e.touches[0]
      if (!didDragRef.current) {
        const dx = touch.clientX - mouseDownPosRef.current.x
        const dy = touch.clientY - mouseDownPosRef.current.y
        if (Math.hypot(dx, dy) > dragThresholdPx) {
          didDragRef.current = true
        }
      }
      const dragSensitivity = 0.6
      positionRef.current = {
        x: (touch.clientX - dragStart.x) * dragSensitivity,
        y: (touch.clientY - dragStart.y) * dragSensitivity,
      }
      if (animationFrameRef.current === null) {
        animationFrameRef.current = requestAnimationFrame(() => {
          setPosition(positionRef.current)
          animationFrameRef.current = null
        })
      }
    }
  }

  const handleTouchEnd = (e: React.TouchEvent) => {
    if (e.touches.length > 0) {
      return
    }
    const wasPinch = pinchStartDist.current !== null
    pinchStartDist.current = null
    isDraggingRef.current = false
    setIsDragging(false)
    if (
      !wasPinch &&
      !didDragRef.current &&
      scale <= 1.05 &&
      swipeStart.current &&
      e.changedTouches[0]
    ) {
      const dx = e.changedTouches[0].clientX - swipeStart.current.x
      if (Math.abs(dx) > 60) {
        if (dx < 0 && canGoNext) {
          handleNext()
        } else if (dx > 0 && canGoPrevious) {
          handlePrevious()
        }
      }
    }
    swipeStart.current = null
  }

  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    // Ignore the click that ends a pan drag, even if it lands off the image.
    if (didDragRef.current) {
      didDragRef.current = false
      return
    }
    // Close only when the click lands on the backdrop itself, not the image
    // or the floating header/footer controls.
    if (e.target === e.currentTarget) {
      onClose()
    }
  }

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex flex-col overflow-hidden bg-black/90"
      onClick={handleBackdropClick}
    >
      {/* Header - floats over the media, doesn't take up layout space */}
      <div className="absolute inset-x-0 top-0 z-20 flex flex-wrap items-center justify-between gap-2 bg-gradient-to-b from-black/70 to-transparent p-2 pt-[max(0.5rem,env(safe-area-inset-top))] sm:p-4">
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold text-white sm:text-lg">{file.name}</h3>
        <div className="flex flex-wrap items-center gap-1 sm:gap-2">
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Diminuir zoom" onClick={handleZoomOut}>
            <ZoomOut className="w-4 h-4" />
          </Button>
          <span className="text-sm text-white/80">
            {Math.round(scale * 100)}%
          </span>
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Aumentar zoom" onClick={handleZoomIn}>
            <ZoomIn className="w-4 h-4" />
          </Button>
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Girar" onClick={handleRotate}>
            <RotateCw className="w-4 h-4" />
          </Button>
          <Button variant="ghost" size="sm" className="hidden text-white hover:bg-white/20 hover:text-white sm:inline-flex" onClick={handleReset}>
            Reset
          </Button>
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Baixar" onClick={handleDownload}>
            <Download className="w-4 h-4" />
          </Button>
          <Button variant="ghost" size="icon" className="text-white hover:bg-white/20 hover:text-white" aria-label="Fechar" onClick={onClose}>
            <X className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Image Container - fills the entire viewport */}
      <div
        ref={imageContainerRef}
        className={`relative flex flex-1 items-center justify-center overflow-hidden ${scale > 1 ? 'touch-none' : ''}`}
        onClick={handleBackdropClick}
        onMouseDown={handleMouseDown}
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseUp}
        onWheel={handleWheel}
        onTouchStart={handleTouchStart}
        onTouchMove={handleTouchMove}
        onTouchEnd={handleTouchEnd}
        onTouchCancel={handleTouchEnd}
      >
        {/* Navigation Buttons */}
        {canGoPrevious && (
          <Button
            variant="ghost"
            size="lg"
            className="absolute left-4 top-1/2 -translate-y-1/2 z-10 bg-transparent hover:bg-transparent text-white border-0 shadow-none p-2"
            onClick={handlePrevious}
            aria-label="Anterior"
          >
            <ChevronLeft className="w-6 h-6" />
          </Button>
        )}
        {canGoNext && (
          <Button
            variant="ghost"
            size="lg"
            className="absolute right-4 top-1/2 -translate-y-1/2 z-10 bg-transparent hover:bg-transparent text-white border-0 shadow-none p-2"
            onClick={handleNext}
            aria-label="Próximo"
          >
            <ChevronRight className="w-6 h-6" />
          </Button>
        )}

        <img
          ref={imageRef}
          src={streamUrl}
          alt={file.name}
          className="select-none"
          onClick={(e) => e.stopPropagation()}
          style={{
            maxHeight: '100%',
            width: 'auto',
            maxWidth: '100%',
            transform: `scale(${scale}) rotate(${rotation}deg) translate(${position.x}px, ${position.y}px)`,
            transformOrigin: 'center center',
            transition: isDragging ? 'none' : 'transform 0.1s ease-out'
          }}
          onLoad={() => {
            // Always start at 100% scale (scale = 1)
            // CSS maxWidth/maxHeight will handle fitting the image to container
            setScale(1)
          }}
          onError={() => {
            toast.error('Failed to load image')
          }}
        />
      </div>

      {/* Footer - floats over the media, doesn't take up layout space */}
      <div className="absolute inset-x-0 bottom-0 z-20 bg-gradient-to-t from-black/70 to-transparent p-2 sm:p-4">
        <div className="flex items-center justify-between text-xs text-white/80 sm:text-sm">
          <div className="flex items-center gap-2 sm:gap-4">
            <span>Scale: {Math.round(scale * 100)}%</span>
            <span>Rotation: {rotation}°</span>
          </div>
          <div className="hidden items-center gap-2 sm:flex">
            <span>Use mouse wheel to zoom</span>
            <span>•</span>
            <span>Drag to pan</span>
            <span>•</span>
            <span>Press R to rotate</span>
          </div>
        </div>
      </div>
    </div>
  )
}
