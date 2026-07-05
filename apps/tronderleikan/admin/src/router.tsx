import { createRouter as createTanStackRouter } from '@tanstack/react-router'

import { routeTree } from './routeTree.gen'
import { NotFound } from '@/components/not-found'
import { BASE_PATH } from '@/lib/basepath'

// TanStack Start-entrypunktet importerer `getRouter` fra denne fila.
// basepath monterer hele rute-treet under /admin (SPEC §10). Matcher `base` i
// vite.config.ts, slik at klient-ruter og assets deler samme prefiks.
export function getRouter() {
  return createTanStackRouter({
    routeTree,
    basepath: BASE_PATH,
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
