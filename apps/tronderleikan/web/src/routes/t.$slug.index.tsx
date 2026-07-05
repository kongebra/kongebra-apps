import { Link, createFileRoute } from '@tanstack/react-router'
import { CalendarDays, ChevronRight } from 'lucide-react'

import { listGames, listTournaments } from '@/lib/api/competition'
import { ApiError } from '@/lib/api/errors'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { EmptyState } from '@/components/empty-state'
import { RestrictedNotice } from '@/components/restricted-notice'
import type { Game, Tournament } from '@/lib/api/types'

export const Route = createFileRoute('/t/$slug/')({
  loader: async ({ context }) => {
    const tenantId = context.tenant.id
    try {
      const [tournaments, games] = await Promise.all([
        listTournaments({ data: tenantId }),
        listGames({ data: { tenantId } }),
      ])
      return { restricted: false as const, tournaments, games }
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        return { restricted: true as const }
      }
      throw err
    }
  },
  component: TenantHome,
})

function TenantHome() {
  const data = Route.useLoaderData()
  const { slug } = Route.useParams()

  if (data.restricted) return <RestrictedNotice />

  const { tournaments, games } = data
  if (games.length === 0) {
    return (
      <EmptyState
        title="Ingen konkurranser ennå"
        description="Når arrangøren oppretter Games, dukker de offentlige tavlene opp her."
      />
    )
  }

  const byTournament = groupByTournament(tournaments, games)

  return (
    <div className="space-y-8">
      {byTournament.map(({ tournament, games: tGames }) => (
        <section key={tournament?.id ?? 'ukjent'}>
          <div className="mb-3 flex items-center gap-2">
            <CalendarDays className="text-muted-foreground size-4" />
            <h2 className="font-semibold">
              {tournament?.name ?? 'Uten turnering'}
              {tournament?.year ? (
                <span className="text-muted-foreground font-normal">
                  {' '}
                  · {tournament.year}
                </span>
              ) : null}
            </h2>
          </div>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {tGames.map((game) => (
              <GameCard key={game.id} slug={slug} game={game} />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function GameCard({ slug, game }: { slug: string; game: Game }) {
  return (
    <Link
      to="/t/$slug/games/$gameId"
      params={{ slug, gameId: game.id }}
      className="group"
    >
      <Card className="hover:border-primary/50 h-full gap-3 py-4 transition-colors">
        <CardContent className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="truncate font-medium">{game.title}</p>
            <p className="text-muted-foreground mt-1 text-sm capitalize">
              {game.category}
            </p>
          </div>
          <ChevronRight className="text-muted-foreground group-hover:text-foreground mt-0.5 size-4 shrink-0 transition-colors" />
        </CardContent>
        <CardContent>
          <Badge variant={game.status === 'finalized' ? 'secondary' : 'outline'}>
            {game.status === 'finalized' ? 'Avsluttet' : 'Åpen'}
          </Badge>
        </CardContent>
      </Card>
    </Link>
  )
}

function groupByTournament(
  tournaments: Tournament[],
  games: Game[],
): { tournament: Tournament | undefined; games: Game[] }[] {
  const byId = new Map(tournaments.map((t) => [t.id, t]))
  const groups = new Map<string, Game[]>()
  for (const game of games) {
    const list = groups.get(game.tournament_id) ?? []
    list.push(game)
    groups.set(game.tournament_id, list)
  }
  return [...groups.entries()].map(([tid, g]) => ({
    tournament: byId.get(tid),
    games: g,
  }))
}
