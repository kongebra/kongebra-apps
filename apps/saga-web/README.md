# saga-web

TanStack Start UI for Saga (saga.kongebra.no). Submit a YouTube URL, watch the
summary job stream live, read the rendered Markdown result.

Design: [docs/superpowers/specs/2026-07-04-saga-platform-design.md](../../docs/superpowers/specs/2026-07-04-saga-platform-design.md)

## Architecture

The ingress serves this app's HTML and routes `saga.kongebra.no/api/*` to
saga-api on the same origin. So the browser calls `/api` directly (fetch for
create/retry, EventSource for the SSE progress stream). This app reaches
saga-api server-side (SSR loaders) via `SAGA_API_URL`.

## Run locally

    npm install
    SAGA_API_URL=http://localhost:8080 npm run dev   # needs saga-api on :8080

Build + run the production server:

    npm run build
    SAGA_API_URL=http://localhost:8080 node .output/server/index.mjs

## Env

- `SAGA_API_URL` - saga-api base URL for server-side rendering (default `http://localhost:8080`).

## Routes

- `/` - job list + submit form
- `/jobs/$id` - job detail: live SSE progress, streamed tokens, Markdown result, retry
- `/health` - `{"status":"ok"}` liveness (never gated on saga-api)
