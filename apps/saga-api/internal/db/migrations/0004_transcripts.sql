CREATE TABLE transcripts (
    sha256     text PRIMARY KEY,
    text       text NOT NULL,
    tokens     int NOT NULL DEFAULT 0,
    chars      int NOT NULL DEFAULT 0,
    lang       text NOT NULL DEFAULT '',
    source     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
