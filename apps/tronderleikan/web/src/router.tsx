import { createRouter as createTanStackRouter } from '@tanstack/react-router'

import { routeTree } from './routeTree.gen'
import { NotFound } from '@/components/not-found'

// TanStack Start-entrypunktet importerer `getRouter` fra denne fila.
export function getRouter() {
  return createTanStackRouter({
    routeTree,
    context: { user: null },
    defaultPreload: 'intent',
    scrollRestoration: true,
    defaultNotFoundComponent: NotFound,
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
