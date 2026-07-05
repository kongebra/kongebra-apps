// Minimal mock av platform/roster/competition for Playwright-røyk uten ekte
// backend. Speiler rutene og DTO-formene fra Go-tjenestene (anonyme lesninger).
// Full ende-til-ende mot ekte tjenester + Zitadel hører til arbeidspakke 1.6.
import { createServer } from 'node:http'

const PORT = Number(process.env.MOCK_PORT ?? 4599)

const TENANT_ID = '018f0000-0000-7000-8000-000000000001'
const TOURNAMENT_ID = '018f0000-0000-7000-8000-0000000000a1'
const GAME_ID = '018f0000-0000-7000-8000-0000000000b1'

const persons = [
  { id: 'p1', tenant_id: TENANT_ID, name: 'Kari Nordmann', department: 'Design', created_at: '', updated_at: '' },
  { id: 'p2', tenant_id: TENANT_ID, name: 'Ola Nordmann', department: 'Utvikling', created_at: '', updated_at: '' },
  { id: 'p3', tenant_id: TENANT_ID, name: 'Per Hansen', department: 'Salg', created_at: '', updated_at: '' },
]

const participants = [
  { id: 'pa1', tenant_id: TENANT_ID, game_id: GAME_ID, type: 'person', person_id: 'p1', created_at: '' },
  { id: 'pa2', tenant_id: TENANT_ID, game_id: GAME_ID, type: 'person', person_id: 'p2', created_at: '' },
  { id: 'pa3', tenant_id: TENANT_ID, game_id: GAME_ID, type: 'person', person_id: 'p3', created_at: '' },
]

// Plasseringer med tie: to deltakere deler 2. plass.
const results = [
  { id: 'r1', tenant_id: TENANT_ID, game_id: GAME_ID, participant_id: 'pa1', rank: 1, created_at: '', updated_at: '' },
  { id: 'r2', tenant_id: TENANT_ID, game_id: GAME_ID, participant_id: 'pa2', rank: 2, created_at: '', updated_at: '' },
  { id: 'r3', tenant_id: TENANT_ID, game_id: GAME_ID, participant_id: 'pa3', rank: 2, created_at: '', updated_at: '' },
]

const tournaments = [
  { id: TOURNAMENT_ID, tenant_id: TENANT_ID, name: 'TrønderLeikan 2026', year: 2026, created_at: '', updated_at: '' },
]

const games = [
  { id: GAME_ID, tenant_id: TENANT_ID, tournament_id: TOURNAMENT_ID, title: 'Fredagsquiz', category: 'quiz', requires_approval: false, status: 'finalized', created_at: '', updated_at: '' },
]

function json(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(body))
}

const server = createServer((req, res) => {
  const path = new URL(req.url, `http://localhost`).pathname

  if (path.endsWith('/api/platform/tenants/by-slug/demo')) {
    return json(res, 200, { id: TENANT_ID, name: 'Demo AS', slug: 'demo', public_visibility: true })
  }
  if (path.includes('/api/platform/tenants/by-slug/')) {
    return json(res, 404, { error: 'tenant finnes ikke' })
  }
  if (path.endsWith('/persons')) return json(res, 200, persons)
  if (path.endsWith('/tournaments')) return json(res, 200, tournaments)
  if (path.endsWith('/games')) return json(res, 200, games)
  if (path.endsWith(`/games/${GAME_ID}`)) return json(res, 200, games[0])
  if (path.endsWith('/results')) return json(res, 200, results)
  if (path.endsWith('/participants')) return json(res, 200, participants)
  if (path.endsWith('/teams')) return json(res, 200, [])

  return json(res, 404, { error: 'not found' })
})

server.listen(PORT, () => {
  console.log(`mock-services listening on ${PORT}`)
})
