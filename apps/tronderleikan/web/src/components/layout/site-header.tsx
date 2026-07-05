import { Link, useRouterState } from '@tanstack/react-router'
import { LogIn, LogOut } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { Wordmark } from '@/components/layout/wordmark'
import type { SessionUser } from '@/lib/auth/session'

export function SiteHeader({ user }: { user: SessionUser | null }) {
  const href = useRouterState({ select: (s) => s.location.href })
  const loginHref = `/auth/login?returnTo=${encodeURIComponent(href)}`

  return (
    <header className="bg-background/85 supports-[backdrop-filter]:bg-background/70 sticky top-0 z-40 border-b backdrop-blur">
      <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4 sm:px-6">
        <Link to="/" className="flex items-center gap-2">
          <Wordmark />
        </Link>

        <div className="ml-auto flex items-center gap-1.5">
          <ThemeToggle />
          {user ? (
            <>
              <span className="text-muted-foreground hidden px-2 text-sm sm:inline">
                {user.name}
              </span>
              <Button asChild variant="outline" size="sm">
                {/* Full navigasjon: /auth/* er server-ruter, ikke klient-ruting. */}
                <a href="/auth/logout">
                  <LogOut className="size-4" />
                  Logg ut
                </a>
              </Button>
            </>
          ) : (
            <Button asChild size="sm">
              <a href={loginHref}>
                <LogIn className="size-4" />
                Logg inn
              </a>
            </Button>
          )}
        </div>
      </div>
    </header>
  )
}
