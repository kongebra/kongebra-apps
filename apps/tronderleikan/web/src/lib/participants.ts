import type { Participant, Person, Team } from '@/lib/api/types'

// Bygger en map participant_id -> visningsnavn. competition refererer kun
// person_id/team_id (verdi-referanser, SPEC §8), så navn hentes fra roster + team.
export function resolveParticipantNames({
  participants,
  teams,
  persons,
}: {
  participants: Participant[]
  teams: Team[]
  persons: Person[]
}): Map<string, string> {
  const personName = new Map(persons.map((p) => [p.id, p.name]))
  const teamName = new Map(teams.map((t) => [t.id, t.name]))

  const out = new Map<string, string>()
  for (const p of participants) {
    if (p.type === 'person' && p.person_id) {
      out.set(p.id, personName.get(p.person_id) ?? 'Ukjent person')
    } else if (p.type === 'team' && p.team_id) {
      out.set(p.id, teamName.get(p.team_id) ?? 'Ukjent lag')
    } else {
      out.set(p.id, 'Ukjent deltaker')
    }
  }
  return out
}
