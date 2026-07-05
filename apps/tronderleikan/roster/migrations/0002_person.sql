-- +goose Up
-- Person: tenant-eid roster-entitet (SPEC §4, §7). IKKE en brukerkonto.
-- ALL deltakelse (competition, bracket, timing, rating) peker på person.id,
-- aldri på en Zitadel-konto. account_id kobler valgfritt en person til en
-- konto (SPEC §4: nullable, unik per tenant, manuell kobling i v1).
--
-- RLS fra første tabell (SPEC §8): tenant_id NOT NULL på hver rad, policy
-- filtrerer på current_setting('app.tenant_id'). App-laget filtrerer også;
-- RLS er sikkerhetsnettet. FORCE gjør at policyen også gjelder tabelleieren
-- (tjenestens egen ikke-superuser DB-bruker).
CREATE TABLE person (
    id         uuid PRIMARY KEY,               -- UUIDv7, generert i app
    tenant_id  uuid NOT NULL,
    name       text NOT NULL,
    department text,                           -- nullable (SPEC §4: ev. avdeling)
    avatar_url text,                           -- nullable (SPEC §4: ev. avatar)
    account_id text,                           -- nullable Zitadel-konto-kobling (SPEC §4)
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- account_id unik PER TENANT (SPEC §4). Partiell unik-indeks: NULL teller ikke,
-- så mange personer uten konto er lov, men en konto kan kobles til høyst én
-- person i tenanten.
CREATE UNIQUE INDEX person_tenant_account_uq
    ON person (tenant_id, account_id)
    WHERE account_id IS NOT NULL;

-- Vanlig oppslag: alle personer i en tenant.
CREATE INDEX person_tenant_idx ON person (tenant_id);

ALTER TABLE person ENABLE ROW LEVEL SECURITY;
ALTER TABLE person FORCE ROW LEVEL SECURITY;
CREATE POLICY person_tenant_isolation ON person
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- +goose Down
DROP TABLE person;
