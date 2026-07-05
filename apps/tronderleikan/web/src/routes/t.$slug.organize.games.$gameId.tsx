import { Link, createFileRoute, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { ArrowLeft, Save, UserPlus } from 'lucide-react'

import {
  getGame,
  listParticipants,
  listResults,
  listTeams,
  recordResults,
  registerParticipant,
} from '@/lib/api/competition'
import { listPersons } from '@/lib/api/roster'
import { errorMessage } from '@/lib/errors'
import { resolveParticipantNames } from '@/lib/participants'
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
import type { PlacementInput } from '@/lib/api/types'

export const Route = createFileRoute('/t/$slug/organize/games/$gameId')({
  loader: async ({ context, params }) => {
    const tenantId = context.tenant.id
    const args = { tenantId, gameId: params.gameId }
    const [game, participants, teams, persons, results] = await Promise.all([
      getGame({ data: args }),
      listParticipants({ data: args }),
      listTeams({ data: args }),
      listPersons({ data: tenantId }),
      listResults({ data: args }),
    ])
    return { game, participants, teams, persons, results }
  },
  component: GameAdmin,
})

function GameAdmin() {
  const { game, participants, teams, persons, results } = Route.useLoaderData()
  const { tenant } = Route.useRouteContext()
  const { slug } = Route.useParams()
  const router = useRouter()

  const names = resolveParticipantNames({ participants, teams, persons })

  // Personer som ennå ikke er deltakere.
  const registeredPersonIds = new Set(
    participants.filter((p) => p.person_id).map((p) => p.person_id),
  )
  const available = persons.filter((p) => !registeredPersonIds.has(p.id))

  const [personId, setPersonId] = useState('')
  const [addError, setAddError] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)

  // Plasseringer per participant_id. Prefylles fra eksisterende resultater.
  const [ranks, setRanks] = useState<Record<string, string>>(() =>
    Object.fromEntries(results.map((r) => [r.participant_id, String(r.rank)])),
  )
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  async function addParticipant(e: React.FormEvent) {
    e.preventDefault()
    const chosen = personId || available[0]?.id
    if (!chosen) return
    setAdding(true)
    setAddError(null)
    try {
      await registerParticipant({
        data: { tenantId: tenant.id, gameId: game.id, personId: chosen },
      })
      setPersonId('')
      await router.invalidate()
    } catch (err) {
      setAddError(errorMessage(err))
    } finally {
      setAdding(false)
    }
  }

  async function savePlacements(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setSaveError(null)
    const placements: PlacementInput[] = []
    for (const [participantId, value] of Object.entries(ranks)) {
      const n = Number(value)
      if (value.trim() !== '' && Number.isInteger(n) && n >= 1) {
        placements.push({ participant_id: participantId, rank: n })
      }
    }
    try {
      await recordResults({
        data: { tenantId: tenant.id, gameId: game.id, placements },
      })
      await router.invalidate()
    } catch (err) {
      setSaveError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-6">
      <Link
        to="/t/$slug/organize/games"
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

      <div className="grid gap-6 lg:grid-cols-[20rem_1fr]">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="text-base">Legg til deltaker</CardTitle>
            <CardDescription>Velg en person fra roster.</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={addParticipant} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="p-person">Person</Label>
                <NativeSelect
                  id="p-person"
                  value={personId || available[0]?.id || ''}
                  onChange={(e) => setPersonId(e.target.value)}
                  disabled={available.length === 0}
                >
                  {available.length === 0 ? (
                    <option value="">Alle er lagt til</option>
                  ) : (
                    available.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))
                  )}
                </NativeSelect>
              </div>
              {addError ? (
                <p className="text-destructive text-sm" role="alert">
                  {addError}
                </p>
              ) : null}
              <Button
                type="submit"
                disabled={adding || available.length === 0}
              >
                <UserPlus className="size-4" />
                Legg til
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Plasseringer</CardTitle>
            <CardDescription>
              Gi hver deltaker en plassering. Samme tall = delt plassering (ties).
            </CardDescription>
          </CardHeader>
          <CardContent>
            {participants.length === 0 ? (
              <EmptyState
                title="Ingen deltakere ennå"
                description="Legg til deltakere før du puncher plasseringer."
              />
            ) : (
              <form onSubmit={savePlacements} className="space-y-4">
                <ul className="divide-y">
                  {participants.map((p) => (
                    <li
                      key={p.id}
                      className="flex items-center justify-between gap-4 py-2.5"
                    >
                      <span className="font-medium">
                        {names.get(p.id) ?? 'Ukjent'}
                      </span>
                      <div className="flex items-center gap-2">
                        <Label htmlFor={`rank-${p.id}`} className="sr-only">
                          Plassering
                        </Label>
                        <Input
                          id={`rank-${p.id}`}
                          type="number"
                          min={1}
                          inputMode="numeric"
                          value={ranks[p.id] ?? ''}
                          onChange={(e) =>
                            setRanks((prev) => ({
                              ...prev,
                              [p.id]: e.target.value,
                            }))
                          }
                          className="w-20 text-center tabular-nums"
                          placeholder="-"
                        />
                      </div>
                    </li>
                  ))}
                </ul>
                {saveError ? (
                  <p className="text-destructive text-sm" role="alert">
                    {saveError}
                  </p>
                ) : null}
                <Button type="submit" disabled={saving}>
                  <Save className="size-4" />
                  Lagre plasseringer
                </Button>
              </form>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
