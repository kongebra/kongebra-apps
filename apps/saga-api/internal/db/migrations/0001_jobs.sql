CREATE TABLE jobs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    module          text NOT NULL,
    input           jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'done', 'failed')),
    attempts        int NOT NULL DEFAULT 0,
    progress        text NOT NULL DEFAULT '',
    error           text,
    result_markdown text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    lease_at        timestamptz
);

CREATE INDEX jobs_active_idx ON jobs (status) WHERE status IN ('queued', 'running');
