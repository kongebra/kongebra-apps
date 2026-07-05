-- +goose Up
-- Tournament: årlig ramme rundt Games (SPEC §2). Tenant-eid.
--
-- RLS fra første tabell (SPEC §8): tenant_id NOT NULL, policy filtrerer på
-- current_setting('app.tenant_id'). App-laget filtrerer også; RLS er
-- sikkerhetsnettet. FORCE gjør at policyen også gjelder tabelleieren
-- (tjenestens egen ikke-superuser DB-bruker).
CREATE TABLE tournament (
    id          uuid PRIMARY KEY,               -- UUIDv7, generert i app
    tenant_id   uuid NOT NULL,
    name        text NOT NULL,
    year        integer,                        -- nullable (SPEC §2: årlig ramme, f.eks. 2026)
    description text,                            -- nullable
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX tournament_tenant_idx ON tournament (tenant_id);

ALTER TABLE tournament ENABLE ROW LEVEL SECURITY;
ALTER TABLE tournament FORCE ROW LEVEL SECURITY;
CREATE POLICY tournament_tenant_isolation ON tournament
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- +goose Down
DROP TABLE tournament;
