import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { ShieldAlert } from 'lucide-react'

import { withBase } from '@/lib/basepath'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'

// Pathless layout som gater HELE admin-planet på platform_admin (SPEC §6/§10).
// Ikke innlogget -> til Zitadel-login med retur hit. Innlogget uten rollen ->
// 403-visning (ingen redirect-loop). BFF-en håndhever rollen på nytt per kall.
// /auth/* og /healthz ligger utenfor dette laget og er derfor ikke gatet.
export const Route = createFileRoute('/_shell')({
  beforeLoad: ({ context, location }) => {
    if (!context.user) {
      // href (ikke to) fordi /auth/login er en server-rute utenfor rute-treet.
      // location.href er basepath-relativ, så vi legger på basepath eksplisitt.
      throw redirect({
        href: `${withBase('/auth/login')}?returnTo=${encodeURIComponent(
          withBase(location.href),
        )}`,
      })
    }
  },
  component: ShellLayout,
})

function ShellLayout() {
  const { user } = Route.useRouteContext()

  if (!user?.roles.includes('platform_admin')) {
    return (
      <div className="mx-auto max-w-2xl px-4 py-16 sm:px-6">
        <EmptyState
          title="Ingen tilgang"
          description="Admin-planet krever rollen platform_admin. Kontoen din er innlogget, men mangler den rollen."
          action={
            <Button asChild variant="outline">
              <a href={withBase('/auth/logout')}>
                <ShieldAlert className="size-4" />
                Logg ut
              </a>
            </Button>
          }
        />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <Outlet />
    </div>
  )
}
