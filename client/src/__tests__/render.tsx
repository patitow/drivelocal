// render do Testing Library com um QueryClient novo por teste (sem cache
// entre testes e sem novas tentativas em erro).
// Também monta um roteador em memória, já que as telas guardam o que está
// aberto na URL.
import type { ReactElement } from 'react'
import { render as rtlRender } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createMemoryHistory, createRootRoute, createRouter } from '@tanstack/react-router'

export function render(ui: ReactElement, { url = '/colecao' }: { url?: string } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createRouter({
    routeTree: createRootRoute({ component: () => ui }),
    history: createMemoryHistory({ initialEntries: [url] }),
  })
  return { router, ...rtlRender(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>) }
}
