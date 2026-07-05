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
import { SiteHeader } from '@/components/layout/site-header'
import type { SessionUser } from '@/lib/auth/session'

// Router-kontekst: innlogget bruker deles til alle ruter (UI-gating + beforeLoad).
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
      { title: 'TrønderLeikan' },
      {
        name: 'description',
        content:
          'Turneringer, konkurranser og live-scoreboards for miljøet ditt.',
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
        <SiteHeader user={user} />
        <main className="flex-1">
          <Outlet />
        </main>
        <footer className="text-muted-foreground border-t py-6 text-center text-xs">
          TrønderLeikan - lab for HA, multi-tenancy og event-drevet arkitektur
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
