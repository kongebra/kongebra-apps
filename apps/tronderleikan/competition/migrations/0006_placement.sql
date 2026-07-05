-- +goose Up
-- PlacementResult: plasseringsliste for et Game (SPEC §3, form "Plasseringsliste"
-- for quiz/rebus). Peker på participant (aldri Person direkte - SPEC §2).
--
-- TIES ER LOV (SPEC §2, §3): flere participants kan dele samme rank. Derfor
-- INGEN unik-indeks på (game_id, rank). Unik er (game_id, participant_id) -
-- en participant har høyst én plassering per game.
CREATE TABLE placement_result (
    id             uuid PRIMARY KEY,             -- UUIDv7, generert i app
    tenant_id      uuid NOT NULL,
    game_id        uuid NOT NULL REFERENCES game (id) ON DELETE CASCADE,
    participant_id uuid NOT NULL REFERENCES participant (id) ON DELETE CASCADE,
    rank           integer NOT NULL CHECK (rank >= 1), -- 1 = første plass; ties deler rank
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX placement_tenant_idx ON placement_result (tenant_id);
CREATE INDEX placement_game_idx ON placement_result (game_id);
-- En participant har høyst én plassering per game (ties tillates PÅ rank, ikke
-- flere plasseringer for samme deltaker).
CREATE UNIQUE INDEX placement_game_participant_uq
    ON placement_result (game_id, participant_id);

ALTER TABLE placement_result ENABLE ROW LEVEL SECURITY;
ALTER TABLE placement_result FORCE ROW LEVEL SECURITY;
CREATE POLICY placement_tenant_isolation ON placement_result
    USING (tenant_id = current_setting('app.tenant_id')::uuid)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::uuid);

-- +goose Down
DROP TABLE placement_result;
