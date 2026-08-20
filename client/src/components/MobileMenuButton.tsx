import { Menu } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useAppStore } from '@/lib/store'
import { useIsCompactLayout } from '@/hooks/useMediaQuery'

export function MobileMenuButton() {
  const isCompact = useIsCompactLayout()
  const setSidebarOpen = useAppStore((state) => state.setSidebarOpen)

  if (!isCompact) {
    return null
  }

  return (
    <Button
      variant="ghost"
      size="icon"
      className="shrink-0"
      aria-label="Abrir menu"
      onClick={() => setSidebarOpen(true)}
    >
      <Menu className="h-6 w-6" />
    </Button>
  )
}
