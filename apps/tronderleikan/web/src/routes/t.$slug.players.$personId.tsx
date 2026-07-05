import { Link, createFileRoute, notFound } from '@tanstack/react-router'
import { ArrowLeft, UserRound } from 'lucide-react'

import { getPerson } from '@/lib/api/roster'
import { ApiError } from '@/lib/api/errors'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { RestrictedNotice } from '@/components/restricted-notice'

// Spillerprofil-stubb (SPEC §4: Person, ikke konto). Historikk/ELO kommer i fase 3.
export const Route = createFileRoute('/t/$slug/players/$personId')({
  loader: async ({ context, params }) => {
    try {
      const person = await getPerson({
        data: { tenantId: context.tenant.id, id: params.personId },
      })
      return { restricted: false as const, person }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        return { restricted: true as const }
      }
      if (err instanceof ApiError && err.status === 404) throw notFound()
      throw err
    }
  },
  component: PlayerProfile,
})

function PlayerProfile() {
  const data = Route.useLoaderData()
  const { slug } = Route.useParams()

  if (data.restricted) return <RestrictedNotice />
  const { person } = data

  return (
    <div className="mx-auto max-w-xl space-y-6">
      <Link
        to="/t/$slug"
        params={{ slug }}
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm"
      >
        <ArrowLeft className="size-4" />
        Tilbake
      </Link>

      <Card>
        <CardContent className="flex items-center gap-4">
          <div className="bg-accent text-accent-foreground flex size-14 items-center justify-center rounded-full">
            <UserRound className="size-7" />
          </div>
          <div>
            <h2 className="text-lg font-semibold">{person.name}</h2>
            {person.department ? (
              <p className="text-muted-foreground text-sm">
                {person.department}
              </p>
            ) : null}
            <div className="mt-2">
              <Badge variant={person.account_id ? 'secondary' : 'outline'}>
                {person.account_id ? 'Koblet konto' : 'Ingen konto'}
              </Badge>
            </div>
          </div>
        </CardContent>
      </Card>

      <p className="text-muted-foreground text-sm">
        Resultathistorikk og ELO per kategori kommer i en senere fase.
      </p>
    </div>
  )
}
