import { useState, useMemo, useEffect, useRef, useLayoutEffect } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useRouter, useRouterState } from '@tanstack/react-router'
import { useMediaFiles, useDownloadMultiple } from '@/hooks/useMediaFiles'
import { useAppStore } from '@/lib/store'
import { prefetchThumbnails, resetThumbnailStatuses } from '@/lib/thumbnailStatus'
import { SERVER_RESTORED_EVENT } from '@/lib/reconnect'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent } from '@/components/ui/card'
import { Breadcrumbs } from '@/components/Breadcrumbs'
import { FileCard } from '@/components/FileCard'
import { VideoPlayer } from '@/components/VideoPlayer'
import { ImageViewer } from '@/components/ImageViewer'
import { MobileMenuButton } from '@/components/MobileMenuButton'
import { 
  Search, 
  Download, 
  Grid3X3, 
  List, 
  RefreshCw,
  Folder,
  Play,
  MoreVertical,
  ExternalLink,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
  FileText,
  Calendar,
  HardDrive
} from 'lucide-react'
import { cn, buildDownloadUrlSync } from '@/lib/utils'
import { encodeDriveUrl, pathFromBrowserLocation } from '@/lib/mediaPath'
import { toast } from 'sonner'
import { saveAs } from 'file-saver'
import type { MediaFile } from '@/types'

function gridColumnCount(width: number) {
  if (width >= 1536) return 6
  if (width >= 1280) return 5
  if (width >= 1024) return 4
  if (width >= 640) return 3
  return 2
}

const SORT_LABELS: Record<'name' | 'size' | 'date' | 'type', string> = {
  name: 'Nome',
  size: 'Tamanho',
  date: 'Data',
  type: 'Tipo',
}

export function FileBrowser() {
  const [searchQuery, setSearchQuery] = useState('')
  const [filterType, setFilterType] = useState<'all' | 'video' | 'image'>('all')
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid')
  const [selectedFile, setSelectedFile] = useState<any>(null)
  const [showVideoPlayer, setShowVideoPlayer] = useState(false)
  const [showImageViewer, setShowImageViewer] = useState(false)
  const [contextMenu, setContextMenu] = useState<{file: any, x: number, y: number} | null>(null)
  
  const { 
    selectedFiles, 
    setSelectedFiles, 
    currentPath, 
    setCurrentPath,
    settings,
    setSortBy,
    setSortOrder
  } = useAppStore()
  
  const { data: filesData, isLoading, error, refetch } = useMediaFiles(currentPath)
  const downloadMultiple = useDownloadMultiple()

  // Servidor voltou após uma queda: refaz a listagem da pasta atual
  useEffect(() => {
    const handleRestored = () => {
      void refetch()
    }
    window.addEventListener(SERVER_RESTORED_EVENT, handleRestored)
    return () => window.removeEventListener(SERVER_RESTORED_EVENT, handleRestored)
  }, [refetch])

  // A pasta vem do endereço (/drive/...): voltar/avançar, links e recarregar
  // caem na mesma pasta. Navegar só muda o endereço; o estado segue a URL.
  const router = useRouter()
  const pathname = useRouterState({ select: (st) => st.location.pathname })
  useEffect(() => {
    const p = pathFromBrowserLocation(window.location.pathname, window.location.search)
    if (p !== useAppStore.getState().currentPath) setCurrentPath(p)
  }, [pathname, setCurrentPath])

  // Mouse button navigation (mouse 4 = back, mouse 5 = forward) and keyboard shortcuts
  useEffect(() => {
    const handleMouseButton = (e: MouseEvent) => {
      // Mouse button 3 = back, button 4 = forward
      // But browsers map: button 3 = back, button 4 = forward
      if (e.button === 3) {
        // Back button
        e.preventDefault()
        window.history.back()
      } else if (e.button === 4) {
        // Forward button
        e.preventDefault()
        window.history.forward()
      }
    }

    // Handle keyboard shortcuts (Alt+Left/Right for back/forward)
    const handleKeyDown = (e: KeyboardEvent) => {
      // Alt+Left Arrow = back, Alt+Right Arrow = forward
      if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
        if (e.key === 'ArrowLeft') {
          e.preventDefault()
          window.history.back()
        } else if (e.key === 'ArrowRight') {
          e.preventDefault()
          window.history.forward()
        }
      }
    }

    // Listen on the document to catch all mouse events
    document.addEventListener('mousedown', handleMouseButton)
    document.addEventListener('keydown', handleKeyDown)
    
    return () => {
      document.removeEventListener('mousedown', handleMouseButton)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  const parentRef = useRef<HTMLDivElement>(null)
  const [gridCols, setGridCols] = useState(2)
  
  // Close context menu when clicking outside
  useEffect(() => {
    const handleClickOutside = () => {
      if (contextMenu) {
        setContextMenu(null)
      }
    }

    if (contextMenu) {
      document.addEventListener('click', handleClickOutside)
      return () => document.removeEventListener('click', handleClickOutside)
    }
  }, [contextMenu])

  const filteredFiles = useMemo(() => {
    if (!filesData?.items) return []
    
    let filtered = [...filesData.items] // Create copy to avoid mutating original
    
    // Apply search filter
    if (searchQuery) {
      filtered = filtered.filter(file => 
        file.name.toLowerCase().includes(searchQuery.toLowerCase())
      )
    }
    
    // Apply type filter
    if (filterType !== 'all') {
      filtered = filtered.filter(file => {
        if (filterType === 'video') return file.isVideo
        if (filterType === 'image') return file.isImage
        return true
      })
    }
    
    // Apply sorting (default: name, ascending)
    filtered.sort((a, b) => {
      // Directories first, then files
      if (a.isDirectory && !b.isDirectory) return -1
      if (!a.isDirectory && b.isDirectory) return 1
      
      let comparison = 0
      switch (settings.sortBy) {
        case 'name':
          comparison = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
          break
        case 'size':
          comparison = (a.size || 0) - (b.size || 0)
          break
        case 'date':
          const dateA = new Date(a.modifiedAt || a.createdAt || 0).getTime()
          const dateB = new Date(b.modifiedAt || b.createdAt || 0).getTime()
          comparison = dateA - dateB
          break
        case 'type':
          const extA = a.name.split('.').pop()?.toLowerCase() || ''
          const extB = b.name.split('.').pop()?.toLowerCase() || ''
          comparison = extA.localeCompare(extB)
          break
        default:
          comparison = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
      }
      
      return settings.sortOrder === 'asc' ? comparison : -comparison
    })
    
    return filtered
  }, [filesData?.items, searchQuery, filterType, settings.sortBy, settings.sortOrder])

  useEffect(() => {
    resetThumbnailStatuses()
  }, [currentPath])

  useEffect(() => {
    const paths = filteredFiles
      .filter((file) => file.isDirectory || file.isVideo || file.isImage)
      .map((file) => file.path)
    if (paths.length === 0) {
      return
    }
    prefetchThumbnails(paths).catch(() => {})
  }, [currentPath, filteredFiles])

  useLayoutEffect(() => {
    const el = parentRef.current
    if (!el) {
      return
    }
    const update = () => setGridCols(gridColumnCount(el.clientWidth))
    update()
    const observer = new ResizeObserver(update)
    observer.observe(el)
    return () => observer.disconnect()
  }, [isLoading, viewMode, filteredFiles.length])

  const virtualRowCount = viewMode === 'grid'
    ? Math.ceil(filteredFiles.length / Math.max(gridCols, 1))
    : filteredFiles.length

  const virtualizer = useVirtualizer({
    count: isLoading ? 0 : virtualRowCount,
    getScrollElement: () => parentRef.current,
    estimateSize: () => (viewMode === 'grid' ? 220 : 88),
    overscan: 6,
  })

  const renderFileCard = (file: MediaFile) => (
    <FileCard
      key={`${currentPath}-${file.path}`}
      file={file}
      viewMode={viewMode}
      selected={selectedFiles.has(file.path)}
      currentPath={currentPath}
      onClick={() => handleFileClick(file)}
      onContextMenu={(event) => handleContextMenu(event, file)}
      onSelect={() => handleFileSelect(file.path)}
      onPlay={() => {
        setSelectedFile(file)
        setShowVideoPlayer(true)
      }}
      onView={() => {
        setSelectedFile(file)
        setShowImageViewer(true)
      }}
      onDownload={() => handleDownloadFile(file)}
    />
  )

  const handleFileSelect = (filePath: string) => {
    const newSelected = new Set(selectedFiles)
    if (newSelected.has(filePath)) {
      newSelected.delete(filePath)
    } else {
      newSelected.add(filePath)
    }
    setSelectedFiles(newSelected)
  }

  const handleDownloadSelected = async () => {
    if (selectedFiles.size === 0) {
      toast.warning('No files selected')
      return
    }

    try {
      toast.info(`Downloading ${selectedFiles.size} files...`)
      const blob = await downloadMultiple.mutateAsync(Array.from(selectedFiles)) as Blob
      saveAs(blob, 'media-files.zip')
      toast.success('Download completed!')
      setSelectedFiles(new Set())
    } catch (error) {
      toast.error('Download failed')
      if (import.meta.env.DEV) {
        console.error('Download error:', error)
      }
    }
  }


  const handleDownloadFile = async (file: any) => {
    try {
      const downloadUrl = buildDownloadUrlSync(file.path)
      
      // Show loading toast
      toast.info(`Downloading ${file.name}...`)
      
      const response = await fetch(downloadUrl, {
        method: 'GET',
        headers: {
          'Accept': '*/*',
        },
      })
      
      
      if (!response.ok) {
        const errorText = await response.text()
        if (import.meta.env.DEV) {
          console.error('Download error response:', errorText)
        }
        throw new Error(`HTTP ${response.status}: ${response.statusText}`)
      }
      
      const blob = await response.blob()
      
      if (blob.size === 0) {
        throw new Error('Downloaded file is empty')
      }
      
      if (blob.size < 1000) {
        // File is very small, might be an error message
      }
      
      saveAs(blob, file.name)
      toast.success(`Downloaded ${file.name} (${Math.round(blob.size / 1024)}KB)`)
    } catch (error) {
      if (import.meta.env.DEV) {
        console.error('Download error details:', error)
      }
      const errorMessage = error instanceof Error ? error.message : String(error)
      toast.error(`Download failed: ${errorMessage}`)
    }
  }

  const handleNavigate = (path: string) => {
    router.history.push(encodeDriveUrl(path))
  }

  const handleFileClick = (file: any) => {
    
    if (file.isDirectory) {
      handleNavigate(file.path)
    } else if (file.isVideo) {
      setSelectedFile(file)
      setShowVideoPlayer(true)
    } else if (file.isImage) {
      setSelectedFile(file)
      setShowImageViewer(true)
    } else {
      // Download file
      handleDownloadFile(file)
    }
  }

  const handleContextMenu = (e: React.MouseEvent, file: any) => {
    e.preventDefault()
    const menuWidth = 180
    const menuHeight = 180
    const x = Math.min(Math.max(8, e.clientX), window.innerWidth - menuWidth - 8)
    const y = Math.min(Math.max(8, e.clientY), window.innerHeight - menuHeight - 8)
    setContextMenu({
      file,
      x,
      y
    })
  }

  const handleContextAction = (action: string, file: any) => {
    setContextMenu(null)
    
    switch (action) {
      case 'open':
        handleFileClick(file)
        break
      case 'download':
        handleDownloadFile(file)
        break
      case 'select':
        handleFileSelect(file.path)
        break
      case 'play':
        if (file.isVideo) {
          setSelectedFile(file)
          setShowVideoPlayer(true)
        }
        break
    }
  }


  if (error) {
    return (
      <div className="p-6">
        <Card>
          <CardContent className="p-6">
            <div className="text-center">
              <h3 className="text-lg font-semibold text-destructive mb-2">
                Error loading files
              </h3>
              <p className="text-muted-foreground mb-4">
                {error.message || 'Failed to load files'}
              </p>
              <Button onClick={() => refetch()}>
                <RefreshCw className="w-4 h-4 mr-2" />
                Try Again
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="h-full min-h-0 flex flex-col">
      {/* Header */}
      <div className="border-b px-3 py-2 lg:p-6">
        <div className="flex flex-col gap-2 lg:gap-4 landscape:max-lg:flex-row landscape:max-lg:items-center">
          <div className="flex min-w-0 flex-wrap items-center gap-2 landscape:max-lg:contents">
            <MobileMenuButton />
            <div className="min-w-0 flex-1 landscape:max-lg:hidden">
              <h1 className="hidden text-2xl font-bold lg:block">Arquivos</h1>
              <Breadcrumbs
                currentPath={currentPath}
                onNavigate={handleNavigate}
                className="lg:mt-1"
              />
            </div>
            <div className="flex basis-full flex-wrap items-center gap-2 lg:basis-auto landscape:max-lg:order-last landscape:max-lg:basis-auto">
            <Button
              variant={viewMode === 'grid' ? 'default' : 'outline'}
              size="icon"
              onClick={() => setViewMode('grid')}
              aria-label="Visualização em grade"
              title="Grade"
            >
              <Grid3X3 className="w-4 h-4" />
            </Button>
            <Button
              variant={viewMode === 'list' ? 'default' : 'outline'}
              size="icon"
              onClick={() => setViewMode('list')}
              aria-label="Visualização em lista"
              title="Lista"
            >
              <List className="w-4 h-4" />
            </Button>
            <Button
              variant="outline"
              size="icon"
              onClick={() => refetch()}
              aria-label="Atualizar"
              title="Atualizar"
            >
              <RefreshCw className="w-4 h-4" />
            </Button>
            
            <div className="flex items-center gap-1 lg:border-l lg:pl-2 lg:ml-1">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  const sortOptions: Array<'name' | 'size' | 'date' | 'type'> = ['name', 'size', 'date', 'type']
                  const currentIndex = sortOptions.indexOf(settings.sortBy)
                  const nextIndex = (currentIndex + 1) % sortOptions.length
                  setSortBy(sortOptions[nextIndex])
                }}
                aria-label={`Ordenar por ${SORT_LABELS[settings.sortBy]}`}
                title={`Ordenar por: ${SORT_LABELS[settings.sortBy]}`}
              >
                {settings.sortBy === 'name' && <FileText className="w-4 h-4 mr-1" />}
                {settings.sortBy === 'size' && <HardDrive className="w-4 h-4 mr-1" />}
                {settings.sortBy === 'date' && <Calendar className="w-4 h-4 mr-1" />}
                {settings.sortBy === 'type' && <ArrowUpDown className="w-4 h-4 mr-1" />}
                <span className="text-xs">{SORT_LABELS[settings.sortBy]}</span>
              </Button>
              
              <Button
                variant="outline"
                size="icon"
                onClick={() => setSortOrder(settings.sortOrder === 'asc' ? 'desc' : 'asc')}
                aria-label={settings.sortOrder === 'asc' ? 'Ordem crescente' : 'Ordem decrescente'}
                title={`Ordem: ${settings.sortOrder === 'asc' ? 'Crescente' : 'Decrescente'}`}
              >
                {settings.sortOrder === 'asc' ? (
                  <ArrowUp className="w-4 h-4" />
                ) : (
                  <ArrowDown className="w-4 h-4" />
                )}
              </Button>
            </div>
            </div>
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center landscape:max-lg:contents">
          <div className="relative min-w-0 flex-1">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground w-4 h-4" />
            <Input
              placeholder="Buscar arquivos..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-10"
              aria-label="Buscar arquivos"
            />
          </div>
          
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant={filterType === 'all' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilterType('all')}
            >
              Todos
            </Button>
            <Button
              variant={filterType === 'video' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilterType('video')}
            >
              Vídeos
            </Button>
            <Button
              variant={filterType === 'image' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilterType('image')}
            >
              Imagens
            </Button>
            {selectedFiles.size > 0 && (
              <Button onClick={handleDownloadSelected}>
                <Download className="w-4 h-4 mr-2" />
                Baixar ({selectedFiles.size})
              </Button>
            )}
          </div>
          </div>
        </div>
      </div>

      {/* Content */}
      <div ref={parentRef} className="flex-1 min-h-0 overflow-auto p-3">
        {isLoading ? (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6 gap-2">
            {Array.from({ length: 8 }).map((_, i) => (
              <Card key={i} className="animate-pulse overflow-hidden">
                <CardContent className="p-0">
                  <div className="w-full aspect-video bg-muted" />
                  <div className="px-2 py-1.5 space-y-1">
                    <div className="h-4 bg-muted rounded w-3/4" />
                    <div className="h-3 bg-muted rounded w-1/2" />
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        ) : filteredFiles.length === 0 ? (
          <div className="text-center py-12">
            <Folder className="w-12 h-12 text-muted-foreground mx-auto mb-4" />
            <h3 className="text-lg font-semibold mb-2">Nenhum arquivo encontrado</h3>
            <p className="text-muted-foreground">
              {searchQuery || filterType !== 'all' 
                ? 'Tente ajustar a busca ou os filtros'
                : 'Esta pasta está vazia'
              }
            </p>
          </div>
        ) : (
          <div
            className="relative w-full"
            style={{ height: `${virtualizer.getTotalSize()}px` }}
          >
            {virtualizer.getVirtualItems().map((virtualRow) => {
              const filesInRow = viewMode === 'grid'
                ? filteredFiles.slice(virtualRow.index * gridCols, virtualRow.index * gridCols + gridCols)
                : [filteredFiles[virtualRow.index]]
              return (
                <div
                  key={virtualRow.key}
                  data-index={virtualRow.index}
                  ref={virtualizer.measureElement}
                  className={cn(
                    'absolute top-0 left-0 w-full',
                    viewMode === 'grid'
                      ? 'grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6 gap-2'
                      : 'grid grid-cols-1 gap-2'
                  )}
                  style={{
                    transform: `translateY(${virtualRow.start}px)`,
                    gridTemplateColumns: viewMode === 'grid' ? `repeat(${gridCols}, minmax(0, 1fr))` : undefined,
                  }}
                >
                  {filesInRow.map((file) => renderFileCard(file))}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Video Player Modal */}
      {showVideoPlayer && selectedFile && (() => {
        // Get all media files (images + videos) from filtered files in order
        const mediaFiles = filteredFiles.filter(f => f.isImage || f.isVideo)
        const currentMediaIndex = mediaFiles.findIndex(f => f.path === selectedFile.path)
        
        const handleMediaNavigate = (newIndex: number) => {
          if (newIndex >= 0 && newIndex < mediaFiles.length) {
            const nextFile = mediaFiles[newIndex]
            setSelectedFile(nextFile)
            
            // Close current viewer and open appropriate one
            setShowVideoPlayer(false)
            setShowImageViewer(false)
            
            if (nextFile.isVideo) {
              setShowVideoPlayer(true)
            } else if (nextFile.isImage) {
              setShowImageViewer(true)
            }
          }
        }

        return (
          <VideoPlayer
            file={selectedFile}
            files={mediaFiles}
            currentIndex={currentMediaIndex}
            onClose={() => {
              setShowVideoPlayer(false)
              setSelectedFile(null)
            }}
            onNavigate={handleMediaNavigate}
          />
        )
      })()}

      {/* Image Viewer Modal */}
      {showImageViewer && selectedFile && (() => {
        // Get all media files (images + videos) from filtered files in order
        const mediaFiles = filteredFiles.filter(f => f.isImage || f.isVideo)
        const currentMediaIndex = mediaFiles.findIndex(f => f.path === selectedFile.path)
        
        const handleMediaNavigate = (newIndex: number) => {
          if (newIndex >= 0 && newIndex < mediaFiles.length) {
            const nextFile = mediaFiles[newIndex]
            setSelectedFile(nextFile)
            
            // Close current viewer and open appropriate one
            setShowVideoPlayer(false)
            setShowImageViewer(false)
            
            if (nextFile.isVideo) {
              setShowVideoPlayer(true)
            } else if (nextFile.isImage) {
              setShowImageViewer(true)
            }
          }
        }

        return (
          <ImageViewer
            file={selectedFile}
            files={mediaFiles}
            currentIndex={currentMediaIndex}
            onClose={() => {
              setShowImageViewer(false)
              setSelectedFile(null)
            }}
            onNavigate={handleMediaNavigate}
          />
        )
      })()}

      {/* Context Menu */}
      {contextMenu && (
        <div
          className="fixed z-50 bg-background border border-border rounded-md shadow-lg py-1 min-w-[150px]"
          style={{
            left: contextMenu.x,
            top: contextMenu.y
          }}
          onClick={() => setContextMenu(null)}
        >
          <button
            className="flex min-h-11 w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent"
            onClick={() => handleContextAction('open', contextMenu.file)}
          >
            <ExternalLink className="w-4 h-4" />
            {contextMenu.file.isDirectory ? 'Abrir' : 'Abrir'}
          </button>
          
          {contextMenu.file.isVideo && (
            <button
              className="flex min-h-11 w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => handleContextAction('play', contextMenu.file)}
            >
              <Play className="w-4 h-4" />
              Reproduzir
            </button>
          )}
          
          {!contextMenu.file.isDirectory && (
            <button
              className="flex min-h-11 w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent"
              onClick={() => handleContextAction('download', contextMenu.file)}
            >
              <Download className="w-4 h-4" />
              Baixar
            </button>
          )}
          
          <button
            className="flex min-h-11 w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent"
            onClick={() => handleContextAction('select', contextMenu.file)}
          >
            <MoreVertical className="w-4 h-4" />
            {selectedFiles.has(contextMenu.file.path) ? 'Desselecionar' : 'Selecionar'}
          </button>
        </div>
      )}
    </div>
  )
}
