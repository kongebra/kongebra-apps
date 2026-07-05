// Speiler DTO-ene fra Go-tjenestene (json-tags). Endres disse i backend,
// oppdateres her. Kontrakten er SPEC §2/§3 + tjenestenes httpapi.

// platform: anonym slug-visning (GET /api/platform/tenants/by-slug/{slug}).
export interface PublicTenant {
  id: string
  name: string
  slug: string
  public_visibility: boolean
}

// roster: Person + input.
export interface Person {
  id: string
  tenant_id: string
  name: string
  department?: string | null
  avatar_url?: string | null
  account_id?: string | null
  created_at: string
  updated_at: string
}

export interface PersonInput {
  name: string
  department?: string | null
  avatar_url?: string | null
}

// competition: Tournament / Game / Participant / Placement.
export interface Tournament {
  id: string
  tenant_id: string
  name: string
  year?: number | null
  description?: string | null
  created_at: string
  updated_at: string
}

export type GameStatus = 'open' | 'finalized'

export interface Game {
  id: string
  tenant_id: string
  tournament_id: string
  title: string
  description?: string | null
  category: string
  requires_approval: boolean
  status: GameStatus
  created_at: string
  updated_at: string
}

export interface GameInput {
  tournament_id: string
  title: string
  description?: string | null
  category: string
  requires_approval: boolean
}

export type ParticipantType = 'person' | 'team'

export interface Participant {
  id: string
  tenant_id: string
  game_id: string
  type: ParticipantType
  person_id?: string | null
  team_id?: string | null
  created_at: string
}

export interface Team {
  id: string
  tenant_id: string
  game_id: string
  name: string
  members: string[]
  created_at: string
  updated_at: string
}

export interface PlacementResult {
  id: string
  tenant_id: string
  game_id: string
  participant_id: string
  rank: number
  created_at: string
  updated_at: string
}

export interface PlacementInput {
  participant_id: string
  rank: number
}
