-- +goose Up
-- Game: en konkurranse i et Tournament (SPEC §2, §3). category er fritt
-- definerbar per tenant (SPEC §3: darts, sim-racing, quiz, ...) og brukes av
-- rating-tjenesten (fase 3) til å skille ELO per kategori. requires_approval
-- gjelder open-entry-innsendinger (SPEC §3, default av). status styrer
-- livssyklusen: 'open' (kan endres/punches) -> 'finalized' (låst, event fyrt).
--
-- tournament_id er en FK innad i competition (SAMME tjeneste/DB - SPEC §8
-- forbyr kun cross-service FK). Postgres' referanseintegritets-sjekker
-- bypasser RLS, så FK-en fungerer selv med FORCE RLS; barn-radens WITH CHECK
-- håndhever likevel tenant-scope på selve raden.
CREATE TABLE game (
    id                uuid PRIMARY KEY,          -- UUIDv7, generert i app
    tenant_id         uuid NOT NULL,
    tournament_id     uuid NOT NULL REFERENCES tournament (id) ON DELETE CASCADE,
    title             text NOT NULL,
    description       text,                       -- nullable
    category          text NOT NULL,              -- fri streng (SPEC §3), brukt av rating
    requires_approval boolean NOT NULL DEFAULT false,
    status            text NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'finalized')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX game_tenant_idx ON game (tenant_id);
CREATE INDEX game_tournament_idx ON game (tournament_id);

ALTER TABLE game ENABLE ROW LEVEL SECURITY;
ALTER TABLE game FORCE ROW LEVEL SECURITY;
CREATE POLICY game_tenant_isolation ON game
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- +goose Down
DROP TABLE game;
