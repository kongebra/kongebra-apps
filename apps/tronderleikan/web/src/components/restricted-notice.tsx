import { useRouterState } from '@tanstack/react-router'
import { Lock } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/empty-state'

// Vises når en ikke-offentlig tenant leses anonymt (roster/competition svarer
// 401). Tilbyr innlogging med retur til gjeldende side.
export function RestrictedNotice() {
  const href = useRouterState({ select: (s) => s.location.href })
  return (
    <EmptyState
      title="Denne siden er privat"
      description="Miljøet har skrudd av offentlig innsyn. Logg inn for å se innholdet."
      action={
        <Button asChild>
          <a href={`/auth/login?returnTo=${encodeURIComponent(href)}`}>
            <Lock className="size-4" />
            Logg inn
          </a>
        </Button>
      }
    />
  )
}
