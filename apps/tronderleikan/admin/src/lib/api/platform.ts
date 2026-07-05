import { createServerFn } from '@tanstack/react-start'

import { serviceUrls } from '@/lib/config'
import { requireRole, serviceFetch } from '@/lib/api/http'
import { bool, obj, str } from '@/lib/api/validate'
import type {
  CreateTenantInput,
  Tenant,
  UpdateTenantInput,
} from '@/lib/api/types'

// Alle admin-plane-kall krever platform_admin (SPEC §6). requireRole er den
// bindende sjekken i BFF-en; platform-tjenesten håndhever den samme rollen på
// nytt (defense in depth). serviceFetch auth:'required' sender access-token.

const TENANTS = '/api/platform/tenants'

function tenantsPath(id?: string): string {
  return id ? `${TENANTS}/${encodeURIComponent(id)}` : TENANTS
}

// GET /api/platform/tenants -> alle tenants (admin-plane, tverr-tenant).
export const listTenants = createServerFn({ method: 'GET' }).handler(
  async (): Promise<Tenant[]> => {
    await requireRole('platform_admin')
    return serviceFetch<Tenant[]>(serviceUrls().platform, tenantsPath(), {
      auth: 'required',
    })
  },
)

// GET /api/platform/tenants/{id} -> én tenant med zitadel_org_id.
export const getTenant = createServerFn({ method: 'GET' })
  .validator((id: unknown) => str(id, 'id'))
  .handler(async ({ data: id }): Promise<Tenant> => {
    await requireRole('platform_admin')
    return serviceFetch<Tenant>(serviceUrls().platform, tenantsPath(id), {
      auth: 'required',
    })
  })

function parseCreateInput(raw: Record<string, unknown>): CreateTenantInput {
  const admin = obj(raw.admin)
  return {
    name: str(raw.name, 'name'),
    slug: str(raw.slug, 'slug'),
    public_visibility: bool(raw.public_visibility),
    admin: {
      email: str(admin.email, 'admin.email'),
      given_name: str(admin.given_name, 'admin.given_name'),
      family_name: str(admin.family_name, 'admin.family_name'),
      password: str(admin.password, 'admin.password'),
    },
  }
}

// POST /api/platform/tenants -> provisjonerer Zitadel-org + grant + første admin,
// skriver tenant-raden og tl.platform.tenant.provisioned-eventet (SPEC §5/§9).
export const createTenant = createServerFn({ method: 'POST' })
  .validator((raw: unknown) => parseCreateInput(obj(raw)))
  .handler(async ({ data }): Promise<Tenant> => {
    await requireRole('platform_admin')
    return serviceFetch<Tenant>(serviceUrls().platform, tenantsPath(), {
      method: 'POST',
      body: data,
      auth: 'required',
    })
  })

interface UpdateArgs {
  id: string
  input: UpdateTenantInput
}

// PATCH /api/platform/tenants/{id} -> navn + offentlig synlighet.
export const updateTenant = createServerFn({ method: 'POST' })
  .validator((raw: unknown): UpdateArgs => {
    const o = obj(raw)
    const input = obj(o.input)
    return {
      id: str(o.id, 'id'),
      input: {
        name: str(input.name, 'name'),
        public_visibility: bool(input.public_visibility),
      },
    }
  })
  .handler(async ({ data }): Promise<Tenant> => {
    await requireRole('platform_admin')
    return serviceFetch<Tenant>(serviceUrls().platform, tenantsPath(data.id), {
      method: 'PATCH',
      body: data.input,
      auth: 'required',
    })
  })
