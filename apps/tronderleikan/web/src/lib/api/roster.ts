import { createServerFn } from '@tanstack/react-start'

import { serviceUrls } from '@/lib/config'
import { requireRole, serviceFetch } from '@/lib/api/http'
import { obj, optStr, str } from '@/lib/api/validate'
import type { Person, PersonInput } from '@/lib/api/types'

function personsPath(tenantId: string): string {
  return `/api/roster/tenants/${encodeURIComponent(tenantId)}/persons`
}

function parsePersonInput(raw: Record<string, unknown>): PersonInput {
  return {
    name: str(raw.name, 'name'),
    department: optStr(raw.department, 'department'),
    avatar_url: optStr(raw.avatar_url, 'avatar_url'),
  }
}

// Les roster. Anonymt tillatt for offentlige tenants (readGate i roster).
export const listPersons = createServerFn({ method: 'GET' })
  .validator((tenantId: unknown) => str(tenantId, 'tenantId'))
  .handler(async ({ data: tenantId }): Promise<Person[]> => {
    return serviceFetch<Person[]>(serviceUrls().roster, personsPath(tenantId), {
      auth: 'optional',
    })
  })

export const getPerson = createServerFn({ method: 'GET' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return { tenantId: str(o.tenantId, 'tenantId'), id: str(o.id, 'id') }
  })
  .handler(async ({ data }): Promise<Person> => {
    return serviceFetch<Person>(
      serviceUrls().roster,
      `${personsPath(data.tenantId)}/${encodeURIComponent(data.id)}`,
      { auth: 'optional' },
    )
  })

interface CreatePersonArgs {
  tenantId: string
  input: PersonInput
}

// Skriv roster: krever organizer-rollen (håndheves i BFF + i roster).
export const createPerson = createServerFn({ method: 'POST' })
  .validator((raw: unknown): CreatePersonArgs => {
    const o = obj(raw)
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      input: parsePersonInput(obj(o.input)),
    }
  })
  .handler(async ({ data }): Promise<Person> => {
    await requireRole('organizer')
    return serviceFetch<Person>(serviceUrls().roster, personsPath(data.tenantId), {
      method: 'POST',
      body: data.input,
      auth: 'required',
    })
  })

interface UpdatePersonArgs extends CreatePersonArgs {
  id: string
}

export const updatePerson = createServerFn({ method: 'POST' })
  .validator((raw: unknown): UpdatePersonArgs => {
    const o = obj(raw)
    return {
      tenantId: str(o.tenantId, 'tenantId'),
      id: str(o.id, 'id'),
      input: parsePersonInput(obj(o.input)),
    }
  })
  .handler(async ({ data }): Promise<Person> => {
    await requireRole('organizer')
    return serviceFetch<Person>(
      serviceUrls().roster,
      `${personsPath(data.tenantId)}/${encodeURIComponent(data.id)}`,
      { method: 'PUT', body: data.input, auth: 'required' },
    )
  })

export const deletePerson = createServerFn({ method: 'POST' })
  .validator((raw: unknown) => {
    const o = obj(raw)
    return { tenantId: str(o.tenantId, 'tenantId'), id: str(o.id, 'id') }
  })
  .handler(async ({ data }): Promise<{ ok: true }> => {
    await requireRole('organizer')
    await serviceFetch<void>(
      serviceUrls().roster,
      `${personsPath(data.tenantId)}/${encodeURIComponent(data.id)}`,
      { method: 'DELETE', auth: 'required' },
    )
    return { ok: true }
  })
