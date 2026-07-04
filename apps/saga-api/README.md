# saga-api

Backend for saga.kongebra.no - a personal agent platform running on the
local Ollama box. Postgres-backed job queue, module registry, SSE progress.

Design: [docs/superpowers/specs/2026-07-04-saga-platform-design.md](../../docs/superpowers/specs/2026-07-04-saga-platform-design.md)

## Run locally

    docker run --rm -d -p 5433:5432 -e POSTGRES_PASSWORD=test postgres:17-alpine
    DATABASE_URL=postgres://postgres:test@localhost:5433/postgres go run .

Requires tailnet access (Ollama at 100.125.242.93:11434) and yt-dlp on PATH
for real jobs.

## Test

    export TEST_DATABASE_URL=postgres://postgres:test@localhost:5433/postgres
    go test ./... -race

## API

- `POST /api/jobs` `{"module":"yt-summary","input":{"url":"...","lang":"no","model":"gemma4:e4b"}}`
- `GET /api/jobs`, `GET /api/jobs/{id}`, `POST /api/jobs/{id}/retry`
- `GET /api/events?job={id}` - SSE: snapshot first, then progress/tokens, closes on done/failed

## Modules

- `yt-summary`: YouTube URL -> transcript (yt-dlp, needs residential IP -> home node) -> map-reduce summary (Ollama) -> Markdown
