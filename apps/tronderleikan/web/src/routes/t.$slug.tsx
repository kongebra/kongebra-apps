import {
  Link,
  Outlet,
  createFileRoute,
  notFound,
} from '@tanstack/react-router'
import { ShieldCheck } from 'lucide-react'

import { getTenantBySlug } from '@/lib/api/platform'
import { ApiError } from '@/lib/api/errors'

// Tenant-layout: utleder tenant fra slug (anonymt, SPEC §10) og deler den til
// barne-rutene via konteksten.
export const Route = createFileRoute('/t/$slug')({
  beforeLoad: async ({ params }) => {
    try {
      const tenant = await getTenantBySlug({ data: params.slug })
      return { tenant }
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound()
      throw err
    }
  },
  component: TenantLayout,
})

function TenantLayout() {
  const { slug } = Route.useParams()
  const { tenant, user } = Route.useRouteContext()
  const canOrganize =
    !!user &&
    (user.roles.includes('organizer') || user.roles.includes('tenant_admin'))

  return (
    <div className="mx-auto max-w-6xl px-4 py-8 sm:px-6">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{tenant.name}</h1>
          <p className="text-muted-foreground text-sm">/t/{tenant.slug}</p>
        </div>
        {!tenant.public_visibility ? (
          <span className="text-muted-foreground bg-muted inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium">
            <ShieldCheck className="size-3.5" />
            Privat - krever innlogging
          </span>
        ) : null}
      </div>

      <nav className="mb-6 flex items-center gap-1 border-b">
        <TabLink to="/t/$slug" params={{ slug }} exact>
          Oversikt
        </TabLink>
        {canOrganize ? (
          <TabLink to="/t/$slug/organize" params={{ slug }}>
            Arrangør
          </TabLink>
        ) : null}
      </nav>

      <Outlet />
    </div>
  )
}

function TabLink({
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
      className="text-muted-foreground data-[status=active]:border-primary data-[status=active]:text-foreground -mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium transition-colors hover:text-foreground"
    >
      {children}
    </Link>
  )
}
