-- +goose Up
-- Transactional outbox (SPEC §9): domene-events skrives her i SAMME tx som
-- domene-endringen. Publisher-goroutinen (pkg/outbox.Publisher) leser usendte
-- rader, publiserer til NATS JetStream og setter published_at.
--
-- Kopi av pkg/outbox/migrations/0001_outbox.sql - hver tjeneste eier sin egen
-- versjonsrekkefølge (SPEC §8). Ingen RLS: outbox er infrastruktur, og
-- publisheren leser på tvers av tenants.
CREATE TABLE outbox (
    id           uuid PRIMARY KEY,             -- = event_id (UUIDv7, tidsordnet)
    tenant_id    uuid NOT NULL,
    subject      text NOT NULL,                -- NATS-subject: tl.<service>.<entity>.<event>
    payload      jsonb NOT NULL,               -- hele event-envelopet
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz                   -- NULL = ikke publisert ennå
);

-- Partielt indeks: publisher poller kun usendte rader.
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox;
