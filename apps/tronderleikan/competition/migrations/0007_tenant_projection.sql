-- +goose Up
-- Tenant-projeksjon: lokal read-model av tenant-fakta competition trenger, men
-- ikke eier. platform-tjenesten (SPEC §7) eier tenant-registeret; competition
-- lærer public_visibility via platform sine tenant-events (SPEC §7). Konsumenten
-- (consumer.go) upserter denne fra tl.platform.tenant.provisioned|updated.
--
-- Ingen RLS: dette er infrastruktur på tvers av tenants (som outbox). Read-gaten
-- (httpapi.go) slår opp public_visibility før tenant-tx-en åpnes, og konsumenten
-- skriver på tvers av tenants.
--
-- public_visibility default true (SPEC §6: default på). Ukjent tenant = ingen rad
-- = default på ved oppslag. platform emitter en rad når en tenant skrur den AV.
CREATE TABLE tenant_projection (
    tenant_id         uuid PRIMARY KEY,
    public_visibility boolean NOT NULL DEFAULT true,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE tenant_projection;
