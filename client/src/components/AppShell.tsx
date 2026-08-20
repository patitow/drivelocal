import { useEffect, useState } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { Login } from '@/components/Login'
import { FileBrowser } from '@/components/FileBrowser'
import { UserManagement } from '@/components/UserManagement'
import { MobileMenuButton } from '@/components/MobileMenuButton'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppStore } from '@/lib/store'
import { useTheme } from '@/hooks/useTheme'
import { useIsCompactLayout } from '@/hooks/useMediaQuery'
import { cn } from '@/lib/utils'
import {
  Menu,
  X,
  Settings,
  Folder,
  LogOut,
  Loader2
} from 'lucide-react'

export function AppShell() {
  const { isAuthenticated, isLoading, username, logout, authEnabled } = useAuth()
  const { sidebarOpen, setSidebarOpen } = useAppStore()
  const [currentView, setCurrentView] = useState<'files' | 'settings'>('files')
  const isCompact = useIsCompactLayout()

  useTheme()

  useEffect(() => {
    if (isCompact) {
      setSidebarOpen(false)
    }
  }, [isCompact, setSidebarOpen])

  const closeDrawerIfCompact = () => {
    if (isCompact) {
      setSidebarOpen(false)
    }
  }

  const goTo = (view: 'files' | 'settings') => {
    setCurrentView(view)
    closeDrawerIfCompact()
  }

  const showLabels = isCompact || sidebarOpen

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="flex flex-col items-center gap-4">
          <Loader2 className="w-16 h-16 animate-spin text-primary" />
          <p className="text-muted-foreground">Carregando...</p>
        </div>
      </div>
    )
  }

  if (!isAuthenticated && authEnabled) {
    return <Login />
  }

  return (
    <div className="min-h-dvh bg-background">
      <div className="flex h-dvh">
        {isCompact && sidebarOpen && (
          <button
            type="button"
            aria-label="Fechar menu"
            className="fixed inset-0 z-40 bg-black/50 lg:hidden"
            onClick={() => setSidebarOpen(false)}
          />
        )}

        <aside
          id="app-sidebar"
          className={cn(
            'bg-card border-r flex flex-col overflow-hidden transition-all duration-300',
            isCompact
              ? cn(
                  'fixed inset-y-0 left-0 z-50 w-64',
                  sidebarOpen ? 'translate-x-0' : '-translate-x-full pointer-events-none'
                )
              : sidebarOpen
                ? 'w-64'
                : 'w-16'
          )}
          aria-hidden={isCompact && !sidebarOpen}
        >
          <div className={cn('p-4', !showLabels && 'p-2')}>
            {showLabels ? (
              <div className="flex items-center justify-between mb-6">
                <h1 className="text-xl font-bold whitespace-nowrap">DriveLocal</h1>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setSidebarOpen(false)}
                  className="flex-shrink-0"
                  aria-label={isCompact ? 'Fechar menu' : 'Recolher menu'}
                  title={isCompact ? 'Fechar menu' : 'Recolher menu'}
                >
                  <X className="w-6 h-6" />
                </Button>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-4 mb-6">
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setSidebarOpen(true)}
                  className="w-full"
                  aria-label="Expandir menu"
                  title="Expandir menu"
                >
                  <Menu className="w-6 h-6 flex-shrink-0" />
                </Button>
              </div>
            )}

            <nav className="space-y-2" aria-label="Principal">
              <Button
                variant={currentView === 'files' ? 'default' : 'ghost'}
                className={cn('w-full min-h-11', showLabels ? 'justify-start' : 'justify-center')}
                onClick={() => goTo('files')}
                title={!showLabels ? 'Arquivos' : undefined}
              >
                <Folder className="w-6 h-6 flex-shrink-0" />
                {showLabels && <span className="ml-2 whitespace-nowrap">Arquivos</span>}
              </Button>
              <Button
                variant={currentView === 'settings' ? 'default' : 'ghost'}
                className={cn('w-full min-h-11', showLabels ? 'justify-start' : 'justify-center')}
                onClick={() => goTo('settings')}
                title={!showLabels ? 'Configurações' : undefined}
              >
                <Settings className="w-6 h-6 flex-shrink-0" />
                {showLabels && <span className="ml-2 whitespace-nowrap">Configurações</span>}
              </Button>

              {authEnabled && username && (
                <>
                  {showLabels && (
                    <div className="pt-4 mt-4 border-t">
                      <div className="px-3 py-2 text-sm text-muted-foreground">
                        Logado como: <span className="font-medium text-foreground">{username}</span>
                      </div>
                    </div>
                  )}
                  <Button
                    variant="ghost"
                    className={cn(
                      'w-full min-h-11 text-destructive hover:text-destructive',
                      showLabels ? 'justify-start' : 'justify-center'
                    )}
                    onClick={() => {
                      closeDrawerIfCompact()
                      logout()
                    }}
                    title={!showLabels ? 'Sair' : undefined}
                  >
                    <LogOut className="w-6 h-6 flex-shrink-0" />
                    {showLabels && <span className="ml-2 whitespace-nowrap">Sair</span>}
                  </Button>
                </>
              )}
            </nav>
          </div>
        </aside>

        <div className="flex-1 min-h-0 min-w-0 overflow-hidden">
          {currentView === 'files' ? <FileBrowser /> : <SettingsView />}
        </div>
      </div>
    </div>
  )
}

function SettingsView() {
  const { role } = useAuth()
  const isAdmin = role === 'admin'

  return (
    <div className="h-full min-h-0 overflow-auto p-4 lg:p-6 landscape:max-lg:p-3">
      <div className="mb-4 flex items-center gap-2 lg:hidden">
        <MobileMenuButton />
        <h1 className="text-lg font-semibold">DriveLocal</h1>
      </div>
      {isAdmin ? (
        <UserManagement />
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Settings className="w-5 h-5" />
              Configurações
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground">
              Apenas administradores podem gerenciar configurações.
            </p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
