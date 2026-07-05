import { Link, createFileRoute } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'

import {
  getGame,
  listParticipants,
  listResults,
  listTeams,
} from '@/lib/api/competition'
import { listPersons } from '@/lib/api/roster'
import { ApiError } from '@/lib/api/errors'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { RankBadge } from '@/components/rank-badge'
import { EmptyState } from '@/components/empty-state'
import { RestrictedNotice } from '@/components/restricted-notice'
import { resolveParticipantNames } from '@/lib/participants'

export const Route = createFileRoute('/t/$slug/games/$gameId')({
  loader: async ({ context, params }) => {
    const tenantId = context.tenant.id
    const args = { tenantId, gameId: params.gameId }
    try {
      const [game, results, participants, teams, persons] = await Promise.all([
        getGame({ data: args }),
        listResults({ data: args }),
        listParticipants({ data: args }),
        listTeams({ data: args }),
        listPersons({ data: tenantId }),
      ])
      return {
        restricted: false as const,
        game,
        results,
        participants,
        teams,
        persons,
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        return { restricted: true as const }
      }
      throw err
    }
  },
  component: Scoreboard,
})

function Scoreboard() {
  const data = Route.useLoaderData()
  const { slug } = Route.useParams()

  if (data.restricted) return <RestrictedNotice />

  const { game, results, participants, teams, persons } = data
  const names = resolveParticipantNames({ participants, teams, persons })
  const ordered = [...results].sort(
    (a, b) => a.rank - b.rank || a.participant_id.localeCompare(b.participant_id),
  )

  return (
    <div className="space-y-6">
      <Link
        to="/t/$slug"
        params={{ slug }}
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm"
      >
        <ArrowLeft className="size-4" />
        Alle konkurranser
      </Link>

      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-xl font-bold tracking-tight">{game.title}</h2>
        <Badge variant="outline" className="capitalize">
          {game.category}
        </Badge>
        <Badge variant={game.status === 'finalized' ? 'secondary' : 'outline'}>
          {game.status === 'finalized' ? 'Avsluttet' : 'Åpen'}
        </Badge>
      </div>
      {game.description ? (
        <p className="text-muted-foreground max-w-2xl text-sm">
          {game.description}
        </p>
      ) : null}

      {ordered.length === 0 ? (
        <EmptyState
          title="Ingen plasseringer ennå"
          description="Resultatene vises her så snart arrangøren har punchet dem."
        />
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-20">Plass</TableHead>
                <TableHead>Deltaker</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ordered.map((r) => (
                <TableRow key={r.id}>
                  <TableCell>
                    <RankBadge rank={r.rank} />
                  </TableCell>
                  <TableCell className="font-medium">
                    {names.get(r.participant_id) ?? 'Ukjent deltaker'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
