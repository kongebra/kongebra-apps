import {
  Link,
  Outlet,
  createFileRoute,
  redirect,
} from '@tanstack/react-router'

// Arrangør-layout. Gates på organizer/tenant_admin (SPEC §6). Ikke innlogget ->
// til Zitadel-login med retur hit; innlogget uten rolle -> tilbake til oversikten.
export const Route = createFileRoute('/t/$slug/organize')({
  beforeLoad: ({ context, location, params }) => {
    if (!context.user) {
      throw redirect({
        href: `/auth/login?returnTo=${encodeURIComponent(location.href)}`,
      })
    }
    const canOrganize =
      context.user.roles.includes('organizer') ||
      context.user.roles.includes('tenant_admin')
    if (!canOrganize) {
      throw redirect({ to: '/t/$slug', params: { slug: params.slug } })
    }
  },
  component: OrganizeLayout,
})

function OrganizeLayout() {
  const { slug } = Route.useParams()
  return (
    <div className="space-y-6">
      <nav className="flex flex-wrap items-center gap-1">
        <SubLink to="/t/$slug/organize" params={{ slug }} exact>
          Oversikt
        </SubLink>
        <SubLink to="/t/$slug/organize/roster" params={{ slug }}>
          Roster
        </SubLink>
        <SubLink to="/t/$slug/organize/games" params={{ slug }}>
          Konkurranser
        </SubLink>
      </nav>
      <Outlet />
    </div>
  )
}

function SubLink({
  to,
  params,
  exact,
  children,
}: {
  to: string
  params: { slug: string }
  exact?: boolean
  children: React.ReactNode
}) {
  return (
    <Link
      to={to}
      params={params}
      activeOptions={{ exact }}
      className="text-muted-foreground data-[status=active]:bg-secondary data-[status=active]:text-secondary-foreground rounded-md px-3 py-1.5 text-sm font-medium transition-colors hover:text-foreground"
    >
      {children}
    </Link>
  )
}
