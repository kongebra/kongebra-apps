-- +goose Up
-- Tenant-registeret (SPEC §5, §7, §8). Dette ER tenant-tabellen, så den er
-- bevisst IKKE tenant-scopet: ingen tenant_id-kolonne, ingen RLS. Alle andre
-- tjenester scoper på tenant_id + RLS; platform er det dokumenterte unntaket
-- (SPEC §8: "platform sin tenant-registry er per definisjon ikke tenant-scopet").
CREATE TABLE tenants (
    id                uuid PRIMARY KEY,              -- UUIDv7, generert i app
    name              text NOT NULL,
    slug              text NOT NULL UNIQUE,          -- URL-trygg identifikator (SPEC §10)
    zitadel_org_id    text NOT NULL,                 -- 1:1 med Zitadel Organization (SPEC §5)
    public_visibility boolean NOT NULL DEFAULT true, -- anonym scoreboard-tilgang (SPEC §6)
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE tenants;
