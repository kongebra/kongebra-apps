-- +goose Up
-- Participant: en deltaker i ETT Game (SPEC §2). Egen entitet fra dag 1 slik at
-- individ og lag behandles likt (aldri Person direkte i resultat-tabeller).
--
-- type = 'person': person_id satt (VERDI-ref til roster, ingen cross-service FK).
-- type = 'team':   team_id satt (FK innad i competition).
-- CHECK håndhever at nøyaktig ett av feltene er satt for typen.
CREATE TABLE participant (
    id         uuid PRIMARY KEY,                 -- UUIDv7, generert i app
    tenant_id  uuid NOT NULL,
    game_id    uuid NOT NULL REFERENCES game (id) ON DELETE CASCADE,
    type       text NOT NULL CHECK (type IN ('person', 'team')),
    person_id  uuid,                             -- verdi-ref til roster.person (SPEC §8), NULL for lag
    team_id    uuid REFERENCES team (id) ON DELETE CASCADE, -- NULL for person
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT participant_ref_shape CHECK (
        (type = 'person' AND person_id IS NOT NULL AND team_id IS NULL) OR
        (type = 'team'   AND team_id   IS NOT NULL AND person_id IS NULL)
    )
);

CREATE INDEX participant_tenant_idx ON participant (tenant_id);
CREATE INDEX participant_game_idx ON participant (game_id);

-- En person/lag kan delta høyst én gang per game (partielle unik-indekser).
CREATE UNIQUE INDEX participant_game_person_uq
    ON participant (game_id, person_id) WHERE person_id IS NOT NULL;
CREATE UNIQUE INDEX participant_game_team_uq
    ON participant (game_id, team_id) WHERE team_id IS NOT NULL;

ALTER TABLE participant ENABLE ROW LEVEL SECURITY;
ALTER TABLE participant FORCE ROW LEVEL SECURITY;
CREATE POLICY participant_tenant_isolation ON participant
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- +goose Down
DROP TABLE participant;
