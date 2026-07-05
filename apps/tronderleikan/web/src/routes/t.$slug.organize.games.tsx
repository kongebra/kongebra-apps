import { Link, createFileRoute, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { Plus, Settings2 } from 'lucide-react'

import {
  createGame,
  createTournament,
  listGames,
  listTournaments,
} from '@/lib/api/competition'
import { errorMessage } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { EmptyState } from '@/components/empty-state'

export const Route = createFileRoute('/t/$slug/organize/games')({
  loader: async ({ context }) => {
    const tenantId = context.tenant.id
    const [tournaments, games] = await Promise.all([
      listTournaments({ data: tenantId }),
      listGames({ data: { tenantId } }),
    ])
    return { tournaments, games }
  },
  component: GamesAdmin,
})

function GamesAdmin() {
  const { tournaments, games } = Route.useLoaderData()
  const { tenant } = Route.useRouteContext()
  const { slug } = Route.useParams()
  const router = useRouter()

  return (
    <div className="space-y-6">
      <div className="grid gap-4 md:grid-cols-2">
        <TournamentForm
          tenantId={tenant.id}
          onDone={() => router.invalidate()}
        />
        <GameForm
          tenantId={tenant.id}
          tournaments={tournaments}
          onDone={() => router.invalidate()}
        />
      </div>

      {games.length === 0 ? (
        <EmptyState
          title="Ingen konkurranser"
          description="Opprett en turneringsramme og legg til den første konkurransen."
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {games.map((game) => (
            <Card key={game.id} className="gap-3 py-4">
              <CardContent className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate font-medium">{game.title}</p>
                  <p className="text-muted-foreground text-sm capitalize">
                    {game.category}
                  </p>
                </div>
                <Badge
                  variant={game.status === 'finalized' ? 'secondary' : 'outline'}
                >
                  {game.status === 'finalized' ? 'Avsluttet' : 'Åpen'}
                </Badge>
              </CardContent>
              <CardContent>
                <Button asChild variant="outline" size="sm" className="w-full">
                  <Link
                    to="/t/$slug/organize/games/$gameId"
                    params={{ slug, gameId: game.id }}
                  >
                    <Settings2 className="size-4" />
                    Administrer
                  </Link>
                </Button>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

function TournamentForm({
  tenantId,
  onDone,
}: {
  tenantId: string
  onDone: () => void
}) {
  const [name, setName] = useState('')
  const [year, setYear] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      await createTournament({
        data: {
          tenantId,
          name: name.trim(),
          year: year ? Number(year) : null,
        },
      })
      setName('')
      setYear('')
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Ny turnering</CardTitle>
        <CardDescription>Den årlige rammen konkurranser hører til.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="t-name">Navn</Label>
            <Input
              id="t-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="TrønderLeikan 2026"
              required
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="t-year">År (valgfritt)</Label>
            <Input
              id="t-year"
              type="number"
              value={year}
              onChange={(e) => setYear(e.target.value)}
              placeholder="2026"
            />
          </div>
          {error ? (
            <p className="text-destructive text-sm" role="alert">
              {error}
            </p>
          ) : null}
          <Button type="submit" disabled={pending || !name.trim()}>
            <Plus className="size-4" />
            Opprett turnering
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function GameForm({
  tenantId,
  tournaments,
  onDone,
}: {
  tenantId: string
  tournaments: { id: string; name: string }[]
  onDone: () => void
}) {
  const [tournamentId, setTournamentId] = useState('')
  const [title, setTitle] = useState('')
  const [category, setCategory] = useState('')
  const [requiresApproval, setRequiresApproval] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const effectiveTournament = tournamentId || tournaments[0]?.id || ''

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!effectiveTournament) {
      setError('Opprett en turnering først.')
      return
    }
    setPending(true)
    setError(null)
    try {
      await createGame({
        data: {
          tenantId,
          input: {
            tournament_id: effectiveTournament,
            title: title.trim(),
            category: category.trim(),
            description: null,
            requires_approval: requiresApproval,
          },
        },
      })
      setTitle('')
      setCategory('')
      setRequiresApproval(false)
      onDone()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPending(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Ny konkurranse</CardTitle>
        <CardDescription>Quiz, dart, sim-racing - fri kategori.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="g-tournament">Turnering</Label>
            <NativeSelect
              id="g-tournament"
              value={effectiveTournament}
              onChange={(e) => setTournamentId(e.target.value)}
              disabled={tournaments.length === 0}
            >
              {tournaments.length === 0 ? (
                <option value="">Ingen turnering ennå</option>
              ) : (
                tournaments.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))
              )}
            </NativeSelect>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="g-title">Tittel</Label>
            <Input
              id="g-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Fredagsquiz"
              required
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="g-category">Kategori</Label>
            <Input
              id="g-category"
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              placeholder="quiz"
              required
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={requiresApproval}
              onChange={(e) => setRequiresApproval(e.target.checked)}
              className="border-input size-4 rounded"
            />
            Krever godkjenning av innsendinger
          </label>
          {error ? (
            <p className="text-destructive text-sm" role="alert">
              {error}
            </p>
          ) : null}
          <Button
            type="submit"
            disabled={pending || !title.trim() || !category.trim()}
          >
            <Plus className="size-4" />
            Opprett konkurranse
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}
