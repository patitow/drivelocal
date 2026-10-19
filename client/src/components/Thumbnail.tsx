import { useState, useMemo, useEffect, useSyncExternalStore } from 'react'
import { Image, Video, File, Folder } from 'lucide-react'
import { cn } from '@/lib/utils'
import { buildThumbnailUrlSync } from '@/lib/utils'
import { consumeThumbnailRetry } from '@/lib/thumbnailRetry'
import {
  getThumbnailStatus,
  subscribeThumbnailStatuses,
} from '@/lib/thumbnailStatus'

interface ThumbnailProps {
  file: {
    name: string
    path: string
    isDirectory: boolean
    isVideo: boolean
    isImage: boolean
    size?: number
  }
  className?: string
  size?: 'sm' | 'md' | 'lg'
}

export function Thumbnail({ file, className, size = 'md' }: ThumbnailProps) {
  const [imageError, setImageError] = useState(false)
  const [imageLoading, setImageLoading] = useState(true)
  const [reloadNonce, setReloadNonce] = useState(0)
  const status = useSyncExternalStore(
    subscribeThumbnailStatuses,
    () => getThumbnailStatus(file.path),
    () => getThumbnailStatus(file.path)
  )

  const canFetch = file.isDirectory || file.isVideo || file.isImage
  const skipMissing = canFetch && status === 'missing'
  // Only hold the <img> when the server said it is still encoding.
  // On refresh status is unknown — hit the stable URL so the browser cache can win.
  const waitForBatch = canFetch && status === 'pending'

  const thumbnailUrl = useMemo(() => {
    if (!canFetch || waitForBatch || skipMissing) {
      return null
    }
    return buildThumbnailUrlSync(file.path)
  }, [file.path, canFetch, waitForBatch, skipMissing])

  const sizeClasses = {
    sm: 'w-full h-16',
    md: 'w-full aspect-video h-auto',
    lg: 'w-full h-48'
  }

  const getFileIcon = () => {
    if (file.isDirectory) return <Folder className="w-8 h-8 text-yellow-500" />
    if (file.isVideo) return <Video className="w-8 h-8 text-red-500" />
    if (file.isImage) return <Image className="w-8 h-8 text-blue-500" />
    return <File className="w-8 h-8 text-gray-500" />
  }

  useEffect(() => {
    setReloadNonce(0)
    setImageError(false)
    setImageLoading(true)
  }, [file.path])

  const handleImageLoad = () => {
    setImageLoading(false)
    setImageError(false)
  }

  const handleImageError = () => {
    const wait = consumeThumbnailRetry(file.path)
    if (wait != null) {
      setImageError(false)
      setImageLoading(true)
      window.setTimeout(() => setReloadNonce((n) => n + 1), wait)
      return
    }
    setImageError(true)
    setImageLoading(false)
  }

  return (
    <div className={cn(
      'relative bg-muted overflow-hidden flex items-center justify-center',
      sizeClasses[size],
      className
    )}>
      {thumbnailUrl && !imageError && (
        <img
          key={`${file.path}-${reloadNonce}`}
          src={thumbnailUrl}
          alt={file.name}
          className="absolute inset-0 w-full h-full object-cover transition-opacity"
          loading="lazy"
          decoding="async"
          onLoad={handleImageLoad}
          onError={handleImageError}
          style={{ opacity: imageLoading ? 0 : 1 }}
        />
      )}
      
      {(imageLoading || imageError || !thumbnailUrl) && (
        <div className="flex flex-col items-center justify-center text-muted-foreground">
          {getFileIcon()}
          {(imageLoading && thumbnailUrl) || waitForBatch ? (
            <div className="absolute inset-0 bg-muted animate-pulse" />
          ) : null}
        </div>
      )}
    </div>
  )
}
