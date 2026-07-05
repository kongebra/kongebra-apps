-- +goose Up
-- Team: et lag i ETT Game (SPEC §2). Et lag har N medlemmer (Persons).
-- team_member.person_id lagres som VERDI - person eies av roster-tjenesten,
-- INGEN cross-service FK (SPEC §8). Integritet håndheves ved skriving
-- (ref-validering mot roster) + events (person slettet -> ryddes senere).
CREATE TABLE team (
    id         uuid PRIMARY KEY,                 -- UUIDv7, generert i app
    tenant_id  uuid NOT NULL,
    game_id    uuid NOT NULL REFERENCES game (id) ON DELETE CASCADE,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX team_tenant_idx ON team (tenant_id);
CREATE INDEX team_game_idx ON team (game_id);

ALTER TABLE team ENABLE ROW LEVEL SECURITY;
ALTER TABLE team FORCE ROW LEVEL SECURITY;
CREATE POLICY team_tenant_isolation ON team
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- team_member: kobling lag -> person. person_id er en VERDI-referanse til
-- roster (ingen FK på tvers av tjenester, SPEC §8). team_id er FK innad.
CREATE TABLE team_member (
    team_id   uuid NOT NULL REFERENCES team (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    person_id uuid NOT NULL,                     -- verdi-ref til roster.person (SPEC §8)
    PRIMARY KEY (team_id, person_id)
);

CREATE INDEX team_member_tenant_idx ON team_member (tenant_id);

ALTER TABLE team_member ENABLE ROW LEVEL SECURITY;
ALTER TABLE team_member FORCE ROW LEVEL SECURITY;
CREATE POLICY team_member_tenant_isolation ON team_member
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- +goose Down
DROP TABLE team_member;
DROP TABLE team;
