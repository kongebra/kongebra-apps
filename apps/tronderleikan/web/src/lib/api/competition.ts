import { createServerFn } from '@tanstack/react-start'

import { serviceUrls } from '@/lib/config'
import { requireRole, serviceFetch } from '@/lib/api/http'
import { bool, obj, optNum, optStr, str } from '@/lib/api/validate'
import type {
  Game,
  GameInput,
  Participant,
  PlacementInput,
  PlacementResult,
  Team,
  Tournament,
} from '@/lib/api/types'

function tenantBase(tenantId: string): string {
  return `/api/competition/tenants/${encodeURIComponent(tenantId)}`
}

function comp() {
  return serviceUrls().competition
}

// --- Lesninger (anonymt for offentlige tenants) ---

export const listTournaments = createServerFn({ method: 'GET' })
  .validator((tenantId: unknown) => str(tenantId, 'tenantId'))
  .handler(async ({ data: tenantId }): Promise<Tournament[]> => {
    return serviceFetch<Tournament[]>(
      comp(),
      `${tenantBase(tenantId)}/tournaments`,
      { auth: 'optional' },
    )
  })

export const listGames = createServerFn({ method: 'GET' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      tournamentId: optStr(o.tournamentId, 'tournamentId'),
    }
  })
  .handler(async ({ data }): Promise<Game[]> => {
    const q = data.tournamentId
      ? `?tournament_id=${encodeURIComponent(data.tournamentId)}`
      : ''
    return serviceFetch<Game[]>(
      comp(),
      `${tenantBase(data.tenantId)}/games${q}`,
      { auth: 'optional' },
    )
  })

export const getGame = createServerFn({ method: 'GET' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return { tenantId: str(o.tenantId, 'tenantId'), gameId: str(o.gameId, 'gameId') }
  })
  .handler(async ({ data }): Promise<Game> => {
    return serviceFetch<Game>(
      comp(),
      `${tenantBase(data.tenantId)}/games/${encodeURIComponent(data.gameId)}`,
      { auth: 'optional' },
    )
  })

export const listResults = createServerFn({ method: 'GET' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return { tenantId: str(o.tenantId, 'tenantId'), gameId: str(o.gameId, 'gameId') }
  })
  .handler(async ({ data }): Promise<PlacementResult[]> => {
    return serviceFetch<PlacementResult[]>(
      comp(),
      `${tenantBase(data.tenantId)}/games/${encodeURIComponent(data.gameId)}/results`,
      { auth: 'optional' },
    )
  })

export const listParticipants = createServerFn({ method: 'GET' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return { tenantId: str(o.tenantId, 'tenantId'), gameId: str(o.gameId, 'gameId') }
  })
  .handler(async ({ data }): Promise<Participant[]> => {
    return serviceFetch<Participant[]>(
      comp(),
      `${tenantBase(data.tenantId)}/games/${encodeURIComponent(data.gameId)}/participants`,
      { auth: 'optional' },
    )
  })

export const listTeams = createServerFn({ method: 'GET' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return { tenantId: str(o.tenantId, 'tenantId'), gameId: str(o.gameId, 'gameId') }
  })
  .handler(async ({ data }): Promise<Team[]> => {
    return serviceFetch<Team[]>(
      comp(),
      `${tenantBase(data.tenantId)}/games/${encodeURIComponent(data.gameId)}/teams`,
      { auth: 'optional' },
    )
  })

// --- Skrivinger (organizer) ---

interface CreateTournamentArgs {
  tenantId: string
  name: string
  year: number | null
}

export const createTournament = createServerFn({ method: 'POST' })
  .validator((raw: unknown): CreateTournamentArgs => {
    const o = obj(raw)
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      name: str(o.name, 'name'),
      year: optNum(o.year, 'year'),
    }
  })
  .handler(async ({ data }): Promise<Tournament> => {
    await requireRole('organizer')
    return serviceFetch<Tournament>(
      comp(),
      `${tenantBase(data.tenantId)}/tournaments`,
      {
        method: 'POST',
        body: { name: data.name, year: data.year },
        auth: 'required',
      },
    )
  })

interface CreateGameArgs {
  tenantId: string
  input: GameInput
}

export const createGame = createServerFn({ method: 'POST' })
  .validator((raw: unknown): CreateGameArgs => {
    const o = obj(raw)
    const input = obj(o.input)
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      input: {
        tournament_id: str(input.tournament_id, 'tournament_id'),
        title: str(input.title, 'title'),
        description: optStr(input.description, 'description'),
        category: str(input.category, 'category'),
        requires_approval: bool(input.requires_approval),
      },
    }
  })
  .handler(async ({ data }): Promise<Game> => {
    await requireRole('organizer')
    return serviceFetch<Game>(comp(), `${tenantBase(data.tenantId)}/games`, {
      method: 'POST',
      body: data.input,
      auth: 'required',
    })
  })

export const registerParticipant = createServerFn({ method: 'POST' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      gameId: str(o.gameId, 'gameId'),
      personId: str(o.personId, 'personId'),
    }
  })
  .handler(async ({ data }): Promise<Participant> => {
    await requireRole('organizer')
    return serviceFetch<Participant>(
      comp(),
      `${tenantBase(data.tenantId)}/games/${encodeURIComponent(data.gameId)}/participants`,
      {
        method: 'POST',
        body: { type: 'person', person_id: data.personId },
        auth: 'required',
      },
    )
  })

interface RecordResultsArgs {
  tenantId: string
  gameId: string
  placements: PlacementInput[]
}

// Punch plasseringer med ties: flere kan dele samme rank (SPEC §2/§3).
// competition erstatter hele lista atomisk (replace-semantikk).
export const recordResults = createServerFn({ method: 'POST' })
  .validator((raw: unknown): RecordResultsArgs => {
    const o = obj(raw)
    const list = o.placements
    if (!Array.isArray(list)) {
      throw new Error('placements må være en liste')
    }
    const placements: PlacementInput[] = list.map((item) => {
      const p = obj(item)
      const rank = optNum(p.rank, 'rank')
      if (rank === null || rank < 1) {
        throw new Error('rank må være >= 1')
      }
      return { participant_id: str(p.participant_id, 'participant_id'), rank }
    })
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      gameId: str(o.gameId, 'gameId'),
      placements,
    }
  })
  .handler(async ({ data }): Promise<PlacementResult[]> => {
    await requireRole('organizer')
    return serviceFetch<PlacementResult[]>(
      comp(),
      `${tenantBase(data.tenantId)}/games/${encodeURIComponent(data.gameId)}/results`,
      {
        method: 'POST',
        body: { placements: data.placements },
        auth: 'required',
      },
    )
  })
