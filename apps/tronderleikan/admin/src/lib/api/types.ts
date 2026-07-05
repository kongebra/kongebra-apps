// Speiler DTO-ene fra platform-tjenesten (json-tags i platform/tenant.go +
// server.go). Endres disse i backend, oppdateres her. Kontrakten er SPEC §5/§7.

// Full tenant-visning fra admin-plane (GET /api/platform/tenants[/{id}]).
// I motsetning til den anonyme slug-visningen inneholder denne zitadel_org_id.
export interface Tenant {
  id: string
  name: string
  slug: string
  zitadel_org_id: string
  public_visibility: boolean
  created_at: string
  updated_at: string
}

// Første org-admin som opprettes i Zitadel ved provisjonering (platform
// adminDTO). Alle felt er påkrevd av tjenesten (validateAdmin).
export interface AdminInput {
  email: string
  given_name: string
  family_name: string
  password: string
}

// Provisjoneringsinput (POST /api/platform/tenants). public_visibility utelates
// = default true i tjenesten.
export interface CreateTenantInput {
  name: string
  slug: string
  public_visibility: boolean
  admin: AdminInput
}

// Oppdatering (PATCH /api/platform/tenants/{id}). Kun navn + synlighet er
// endelig redigerbart i v1 (slug/org er stabile identifikatorer).
export interface UpdateTenantInput {
  name: string
  public_visibility: boolean
}
