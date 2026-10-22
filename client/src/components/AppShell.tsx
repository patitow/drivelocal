import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Outlet, useNavigate, useRouter, useRouterState } from '@tanstack/react-router'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { SERVER_RESTORED_EVENT, SERVER_UNAVAILABLE_EVENT, waitForServer } from '@/lib/reconnect'
import { Login } from '@/components/Login'
import { CatalogNav, type CatalogTab } from '@/components/library/CatalogNav'
import { MobileMenuButton } from '@/components/MobileMenuButton'
import { RestrictedToggle } from '@/components/library/RestrictedToggle'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { useAppStore } from '@/lib/store'
import { LAST_KEY } from '@/lib/nav'
import { encodeDriveUrl } from '@/lib/mediaPath'
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

// Cada tela vira um pedaço separado do bundle: abrir o app baixa só a atual.
const UserManagement = lazy(() => import('@/components/UserManagement').then((m) => ({ default: m.UserManagement })))

type View = 'files' | 'catalog' | 'settings'

// A tela e a seção da Coleção saem do endereço (rotas em router.tsx).
function viewOf(pathname: string): { view: View; tab: CatalogTab } {
  const m = pathname.match(/^\/colecao(?:\/([^/]+))?/)
  if (m) return { view: 'catalog', tab: (m[1] ? decodeURIComponent(m[1]) : 'home') as CatalogTab }
  return { view: pathname.startsWith('/configuracoes') ? 'settings' : 'files', tab: 'home' }
}

function ViewFallback() {
  return (
    <div className="flex h-full items-center justify-center">
      <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
    </div>
  )
}

export function AppShell() {
  const { isAuthenticated, isLoading, username, logout, authEnabled, role } = useAuth()
  const { sidebarOpen, setSidebarOpen } = useAppStore()
  const location = useRouterState({ select: (s) => s.location })
  const { view: currentView, tab: catalogTab } = viewOf(location.pathname)
  const navigate = useNavigate()
  const router = useRouter()
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

  // Recarregar ou abrir "/" volta para a última tela.
  useEffect(() => {
    try { localStorage.setItem(LAST_KEY, location.href) } catch { /* sem armazenamento */ }
  }, [location.href])

  const goTo = (view: View) => {
    closeDrawerIfCompact()
    if (view === currentView) return
    if (view === 'files') router.history.push(encodeDriveUrl(useAppStore.getState().currentPath))
    else void navigate({ to: '/configuracoes' })
  }

  // Clique numa seção abre a raiz dela (fecha série, perfil etc. abertos).
  const pickCatalog = (t: CatalogTab) => {
    closeDrawerIfCompact()
    void (t === 'home' ? navigate({ to: '/colecao' }) : navigate({ to: '/colecao/$tab', params: { tab: t } }))
  }

  const showLabels = isCompact || sidebarOpen

  // Reconexão: detecta indisponibilidade do servidor e aguarda o retorno (novo boot ou health 200)
  const [serverDown, setServerDown] = useState(false)
  const bootIdRef = useRef<string | null>(null)
  const reconnectingRef = useRef(false)
  const mountedRef = useRef(false)

  useEffect(() => {
    mountedRef.current = true
    api.getBootId().then((id) => { bootIdRef.current = id }).catch(() => {})
    const handleDown = () => setServerDown(true)
    const handleUp = () => setServerDown(false)
    window.addEventListener(SERVER_UNAVAILABLE_EVENT, handleDown)
    window.addEventListener(SERVER_RESTORED_EVENT, handleUp)
    return () => {
      mountedRef.current = false
      window.removeEventListener(SERVER_UNAVAILABLE_EVENT, handleDown)
      window.removeEventListener(SERVER_RESTORED_EVENT, handleUp)
    }
  }, [])

  useEffect(() => {
    if (!serverDown || reconnectingRef.current) return
    reconnectingRef.current = true
    const run = async () => {
      while (mountedRef.current) {
        const restored = await waitForServer({ prevBootId: bootIdRef.current })
        if (restored) break
      }
      reconnectingRef.current = false
      if (mountedRef.current) {
        api.getBootId().then((id) => { bootIdRef.current = id }).catch(() => {})
      }
    }
    void run()
  }, [serverDown])

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
      {serverDown && (
        <div
          role="status"
          aria-live="polite"
          className="fixed inset-x-0 top-0 z-[60] flex items-center justify-center gap-2 bg-amber-500 px-4 py-2 text-sm font-medium text-black"
        >
          <Loader2 className="w-4 h-4 animate-spin" />
          Conexão com o servidor perdida. Tentando reconectar...
        </div>
      )}
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
            // Na Coleção (sempre escura) a barra acompanha o tema dela.
            currentView === 'catalog' && 'dark bg-[#121318] text-foreground',
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
          <div className={cn('flex min-h-0 flex-1 flex-col overflow-y-auto p-4', !showLabels && 'p-2')}>
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

            <nav className="space-y-1" aria-label="Principal">
              <CatalogNav
                active={currentView === 'catalog' ? catalogTab : null}
                onPick={pickCatalog}
                showLabels={showLabels}
                isAdmin={role === 'admin'}
              />
              <div className="mx-3 my-2 h-px bg-border" />
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
              <RestrictedToggle showLabels={showLabels} />

              {authEnabled && username && (
                <>
                  {showLabels && (
                    <div className="pt-2 mt-2 border-t">
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
          <Suspense fallback={<ViewFallback />}>
            <Outlet />
          </Suspense>
        </div>
      </div>
    </div>
  )
}

export function SettingsView() {
  const { role } = useAuth()
  const isAdmin = role === 'admin'
  const navigate = useNavigate()

  return (
    <div className="h-full min-h-0 overflow-auto p-4 lg:p-6 landscape:max-lg:p-3">
      <div className="mb-4 flex items-center gap-2 lg:hidden">
        <MobileMenuButton />
        <h1 className="text-lg font-semibold">DriveLocal</h1>
      </div>
      {isAdmin ? (
        <>
          <UserManagement />
          {/* bibliotecas, montagens e regras de pasta ficam no Gerenciar da Coleção */}
          <Card className="mt-6">
            <CardHeader><CardTitle>Bibliotecas</CardTitle></CardHeader>
            <CardContent className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
              <span className="flex-1">Pastas, montagens e maturidade por pasta agora ficam em Coleção → Gerenciar → Bibliotecas.</span>
              <Button variant="outline" onClick={() => void navigate({ to: '/colecao/$tab', params: { tab: 'manage' }, search: { secao: 'bibliotecas' } as never })}>Abrir</Button>
            </CardContent>
          </Card>
        </>
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
