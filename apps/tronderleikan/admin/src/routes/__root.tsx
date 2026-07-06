/// <reference types="vite/client" />
import {
  HeadContent,
  Outlet,
  Scripts,
  createRootRouteWithContext,
} from '@tanstack/react-router'
import type { ReactNode } from 'react'

import appCss from '@/styles/app.css?url'
import { fetchCurrentUser } from '@/lib/auth/user'
import { AdminHeader } from '@/components/layout/site-header'
import type { SessionUser } from '@/lib/auth/session'

// Router-kontekst: innlogget bruker deles til alle ruter (gating + beforeLoad).
export interface RouterContext {
  user: SessionUser | null
}

// Setter tema-klassen før paint (unngår FOUC). Kjører kun i nettleseren.
const themeScript = `(function(){try{var t=localStorage.getItem('theme');if(t==='dark'||(!t&&window.matchMedia('(prefers-color-scheme: dark)').matches)){document.documentElement.classList.add('dark')}}catch(e){}})();`

export const Route = createRootRouteWithContext<RouterContext>()({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'robots', content: 'noindex, nofollow' },
      { title: 'TrønderLeikan Admin' },
      {
        name: 'description',
        content: 'Admin-planet: tenants, provisjonering og tverr-tenant innsyn.',
      },
    ],
    links: [{ rel: 'stylesheet', href: appCss }],
  }),
  // Henter innlogget bruker én gang og legger den i router-konteksten.
  beforeLoad: async (): Promise<RouterContext> => ({
    user: await fetchCurrentUser(),
  }),
  component: RootComponent,
})

function RootComponent() {
  const { user } = Route.useRouteContext()
  return (
    <RootDocument>
      <div className="flex min-h-screen flex-col">
        <AdminHeader user={user} />
        <main className="flex-1">
          <Outlet />
        </main>
        <footer className="text-muted-foreground border-t py-6 text-center text-xs">
          TrønderLeikan Admin - plattform-planet (kun platform_admin)
        </footer>
      </div>
    </RootDocument>
  )
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="no" suppressHydrationWarning>
      <head>
        <HeadContent />
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
