import { ChevronRight, Home } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface BreadcrumbItem {
  name: string
  path: string
}

interface BreadcrumbsProps {
  currentPath: string
  onNavigate: (path: string) => void
  className?: string
}

export function Breadcrumbs({ currentPath, onNavigate, className }: BreadcrumbsProps) {
  // Normalize path separators (handle both / and \)
  // Replace backslashes with forward slashes for URL consistency
  const normalizedPath = currentPath.replace(/\\/g, '/')
  // Split by forward slash and filter out empty segments
  const pathSegments = normalizedPath.split('/').filter(Boolean)
  
  const breadcrumbs: BreadcrumbItem[] = [
    { name: 'Início', path: '' }
  ]
  
  // Build breadcrumbs with proper path accumulation
  // Use forward slashes for consistency (server will normalize to Windows separators)
  let accumulatedPath = ''
  pathSegments.forEach((segment) => {
    accumulatedPath += (accumulatedPath ? '/' : '') + segment
    breadcrumbs.push({
      name: segment,
      path: accumulatedPath
    })
  })

  return (
    <nav className={cn('flex max-w-full items-center space-x-1 overflow-x-auto text-sm', className)}>
      {breadcrumbs.map((item, index) => (
        <div key={item.path} className="flex shrink-0 items-center">
          {index > 0 && (
            <ChevronRight className="w-4 h-4 text-muted-foreground mx-1" />
          )}
          
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onNavigate(item.path)}
            className={cn(
              "h-11 px-2 lg:h-8 hover:bg-accent hover:text-accent-foreground cursor-pointer",
              index === breadcrumbs.length - 1 && "font-medium text-foreground",
              index < breadcrumbs.length - 1 && "text-muted-foreground hover:text-foreground",
              index === 0 && "flex items-center"
            )}
          >
            {index === 0 && <Home className="w-4 h-4 mr-1" />}
            <span>{item.name}</span>
          </Button>
        </div>
      ))}
    </nav>
  )
}

// Default export for better module compatibility
export default Breadcrumbs
