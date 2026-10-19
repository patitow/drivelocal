// Rotas do app (TanStack Router). Cada tela tem endereço próprio:
//   /colecao            início da Coleção
//   /colecao/:secao     estante ou seção (animes, criadores, busca, ...), com
//                       o que estiver aberto em ?serie= ?criador= ?obra= ...
//   /drive/*            arquivos, na pasta do caminho
//   /configuracoes
// "/" volta para a última tela visitada.
import { lazy } from 'react'
import { createRootRoute, createRoute, createRouter, redirect, useParams } from '@tanstack/react-router'
import { AppShell, SettingsView } from '@/components/AppShell'
import { SHELVES } from '@/components/library/shelves/shelfUi'
import { CATALOG_MORE, type CatalogTab } from '@/components/library/CatalogNav'
import { LAST_KEY, parseCatalogSearch } from '@/lib/nav'
import { encodeDriveUrl } from '@/lib/mediaPath'

const FileBrowser = lazy(() => import('@/components/FileBrowser').then((m) => ({ default: m.FileBrowser })))
const CatalogView = lazy(() => import('@/components/library/CatalogView').then((m) => ({ default: m.CatalogView })))

const OLD_VIEW_KEY = 'drivelocal:view'

// Última tela (salva pelo AppShell); migra o formato antigo {view, tab}.
function lastLocation(): string {
  try {
    // links antigos: /?path=Pasta
    const old = new URLSearchParams(window.location.search).get('path')
    if (old) return encodeDriveUrl(old)
    const last = localStorage.getItem(LAST_KEY)
    if (last && last.startsWith('/') && !last.startsWith('//') && last !== '/') return last
    const v = JSON.parse(localStorage.getItem(OLD_VIEW_KEY) || '{}')
    if (v.view === 'catalog') return v.tab && v.tab !== 'home' ? `/colecao/${v.tab}` : '/colecao'
    if (v.view === 'settings') return '/configuracoes'
  } catch { /* sem armazenamento */ }
  return '/drive'
}

const TABS = new Set<string>([...SHELVES.map((s) => s.id), ...CATALOG_MORE.map((t) => t.id)])
export const isCatalogTab = (t: string): t is CatalogTab => TABS.has(t)

const rootRoute = createRootRoute({ component: AppShell })

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => { throw redirect({ href: lastLocation(), replace: true }) },
})

const catalogRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/colecao',
  validateSearch: parseCatalogSearch,
  component: () => <CatalogView tab="home" />,
})

const catalogTabRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/colecao/$tab',
  validateSearch: parseCatalogSearch,
  beforeLoad: ({ params }) => {
    if (!isCatalogTab(params.tab) || params.tab === 'home') throw redirect({ to: '/colecao', replace: true })
  },
  component: CatalogTabPage,
})

function CatalogTabPage() {
  const { tab } = useParams({ from: '/colecao/$tab' })
  return <CatalogView tab={tab as CatalogTab} />
}

const driveRoute = createRoute({ getParentRoute: () => rootRoute, path: '/drive', component: FileBrowser })
const driveSplatRoute = createRoute({ getParentRoute: () => rootRoute, path: '/drive/$', component: FileBrowser })
const settingsRoute = createRoute({ getParentRoute: () => rootRoute, path: '/configuracoes', component: SettingsView })

// endereço desconhecido: volta para a última tela
const anyRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '$',
  beforeLoad: () => { throw redirect({ to: '/', replace: true }) },
})

const routeTree = rootRoute.addChildren([indexRoute, catalogRoute, catalogTabRoute, driveRoute, driveSplatRoute, settingsRoute, anyRoute])

export const router = createRouter({
  routeTree,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
