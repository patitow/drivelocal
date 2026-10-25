// Rotas do app (TanStack Router). Cada tela tem endereço próprio:
//   /colecao            início da Coleção
//   /colecao/:secao     estante ou seção (animes, criadores, busca, ...), com
//                       o que estiver aberto em ?serie= ?criador= ?obra= ...
//   /drive/*            arquivos, na pasta do caminho
//   /configuracoes
// "/" abre a Coleção.
import { lazy } from 'react'
import { createRootRoute, createRoute, createRouter, redirect, useParams } from '@tanstack/react-router'
import { AppShell, SettingsView } from '@/components/AppShell'
import { SHELVES } from '@/components/library/shelves/shelfUi'
import { CATALOG_MORE, type CatalogTab } from '@/components/library/CatalogNav'
import { parseCatalogSearch } from '@/lib/nav'
import { encodeDriveUrl } from '@/lib/mediaPath'

const FileBrowser = lazy(() => import('@/components/FileBrowser').then((m) => ({ default: m.FileBrowser })))
const CatalogView = lazy(() => import('@/components/library/CatalogView').then((m) => ({ default: m.CatalogView })))

// Tela inicial: a Coleção; links antigos /?path=Pasta ainda abrem a pasta.
function startLocation(): string {
  const old = new URLSearchParams(window.location.search).get('path')
  return old ? encodeDriveUrl(old) : '/colecao'
}

const TABS = new Set<string>([...SHELVES.map((s) => s.id), ...CATALOG_MORE.map((t) => t.id)])
export const isCatalogTab = (t: string): t is CatalogTab => TABS.has(t)

const rootRoute = createRootRoute({ component: AppShell })

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => { throw redirect({ href: startLocation(), replace: true }) },
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
    // a Revisão virou seção do Gerenciar
    if (params.tab === 'review') throw redirect({ to: '/colecao/$tab', params: { tab: 'manage' }, search: { secao: 'revisao' } as never, replace: true })
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
