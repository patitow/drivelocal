import { useRef } from 'react'
import { Download, Eye, Play } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Thumbnail } from '@/components/Thumbnail'
import { cn, formatFileSize } from '@/lib/utils'
import type { MediaFile } from '@/types'

interface FileCardProps {
  file: MediaFile
  viewMode: 'grid' | 'list'
  selected: boolean
  currentPath: string
  onClick: () => void
  onContextMenu: (event: React.MouseEvent) => void
  onSelect: () => void
  onPlay: () => void
  onView: () => void
  onDownload: () => void
}

export function FileCard({
  file,
  viewMode,
  selected,
  currentPath,
  onClick,
  onContextMenu,
  onSelect,
  onPlay,
  onView,
  onDownload,
}: FileCardProps) {
  const longPressTimer = useRef<number | null>(null)
  const didLongPress = useRef(false)

  const clearLongPress = () => {
    if (longPressTimer.current !== null) {
      window.clearTimeout(longPressTimer.current)
      longPressTimer.current = null
    }
  }

  const handleTouchStart = (event: React.TouchEvent) => {
    const touch = event.touches[0]
    if (!touch) {
      return
    }
    didLongPress.current = false
    const { clientX, clientY } = touch
    longPressTimer.current = window.setTimeout(() => {
      didLongPress.current = true
      onContextMenu({
        preventDefault() {},
        clientX,
        clientY,
      } as React.MouseEvent)
    }, 500)
  }

  const handleClick = () => {
    if (didLongPress.current) {
      didLongPress.current = false
      return
    }
    onClick()
  }

  const typeLabel = file.isDirectory ? 'Pasta' : file.isVideo ? 'Vídeo' : file.isImage ? 'Imagem' : 'Arquivo'

  return (
    <Card
      className={cn(
        'cursor-pointer transition-all hover:shadow-md group relative overflow-hidden',
        selected && 'ring-2 ring-primary'
      )}
      onClick={handleClick}
      onContextMenu={onContextMenu}
      onTouchStart={handleTouchStart}
      onTouchEnd={clearLongPress}
      onTouchMove={clearLongPress}
      onTouchCancel={clearLongPress}
    >
      <CardContent className={cn('relative', viewMode === 'grid' ? 'p-0' : 'p-3')}>
        {!file.isDirectory && (
          <label
            className="absolute top-0 right-0 z-20 flex h-11 w-11 items-center justify-center"
            onClick={(e) => e.stopPropagation()}
          >
            <input
              type="checkbox"
              aria-label={`Selecionar ${file.name}`}
              className="h-5 w-5 cursor-pointer rounded border-2 border-primary bg-background text-primary focus:ring-2 focus:ring-primary"
              checked={selected}
              onChange={(e) => {
                e.stopPropagation()
                onSelect()
              }}
            />
          </label>
        )}

        {viewMode === 'grid' ? (
          <div>
            <Thumbnail key={`${currentPath}-${file.path}`} file={file} size="md" className="rounded-none" />
            <div className="px-2 py-1.5">
              <h3 className="text-sm font-medium truncate leading-tight">{file.name}</h3>
              <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
                <Badge variant="secondary" className="text-xs">
                  {typeLabel}
                </Badge>
                {file.isDirectory && (
                  <span className="text-xs text-muted-foreground">
                    {file.fileCount !== undefined && file.fileCount > 0 && (
                      <>{file.fileCount} {file.fileCount === 1 ? 'arquivo' : 'arquivos'}</>
                    )}
                    {file.fileCount !== undefined && file.fileCount > 0 && file.folderCount !== undefined && file.folderCount > 0 && ' • '}
                    {file.folderCount !== undefined && file.folderCount > 0 && (
                      <>{file.folderCount} {file.folderCount === 1 ? 'pasta' : 'pastas'}</>
                    )}
                    {(!file.fileCount || file.fileCount === 0) && (!file.folderCount || file.folderCount === 0) && 'Vazio'}
                  </span>
                )}
                {!file.isDirectory && file.size && (
                  <span className="text-xs text-muted-foreground">
                    {formatFileSize(file.size)}
                  </span>
                )}
              </div>
            </div>
          </div>
        ) : (
          <div className="flex w-full min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:gap-4">
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <div className="h-16 w-16 flex-shrink-0">
                <Thumbnail key={`${currentPath}-${file.path}`} file={file} size="sm" />
              </div>
              <h3 className="min-w-0 flex-1 truncate font-medium">{file.name}</h3>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="secondary" className="text-xs">
                {typeLabel}
              </Badge>
              {file.isVideo && (
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`Reproduzir ${file.name}`}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onPlay()
                  }}
                >
                  <Play className="w-4 h-4" />
                </Button>
              )}
              {file.isImage && (
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`Ver ${file.name}`}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onView()
                  }}
                >
                  <Eye className="w-4 h-4" />
                </Button>
              )}
              {!file.isDirectory && (
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label={`Baixar ${file.name}`}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onDownload()
                  }}
                >
                  <Download className="w-4 h-4" />
                </Button>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
