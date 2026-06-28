# status-web

Frontend for status.newb.no. TanStack Start SSR (React), vehikkel for topp-lag observability i k8s-clusteret.
Eneste tjeneste med IngressRoute (trafikk utenfra). Se [root AGENTS.md](../../AGENTS.md) for repo-konvensjoner.

## Arkitektur

**Single data path via createServerFn:** Én server-funksjon (`fetchStatus`) kjører server-side under SSR og eksponeres som RPC for klient-refresh.
Nettleseren når aldri checker - CHECKER_URL er server-only.
30s auto-refresh: klient invalider loader hver 30s via `router.invalidate()`, samme server-fn kalles igjen.

## Endepunkter

- `GET /` - Status-grid (SSR), viser tjenesters status fra checker.
- `GET /health` - `200 OK` (fast, self-contained, ingen avhengigheter).

## Bygg og kjør

```sh
npm run dev        # Vite dev-server (localhost:5173)
npm run build      # vite build -> .output/server/index.mjs (Nitro node-server)
npm start          # node .output/server/index.mjs
npm run typecheck  # TypeScript verify
```

- Build output: `.output/server/index.mjs` (Nitro node-server for SSR).
- **IKKE distroless** - bevisst avvik. Node SSR runtime er krav; frontend-tjenester bruker node:22-slim.
- Pinnet versjon: `@tanstack/react-start` 1.168.26, `@tanstack/react-router` 1.170.16, `nitro` 3.0.260610-beta (beta for Vite Environments API + router-skew workaround).

## Env

| Var | Effekt |
|-----|--------|
| `CHECKER_URL` | Basis-URL til checker (in-cluster service-DNS, env-suffikset namespace, e.g. `http://status-checker.status-checker-dev.svc.cluster.local:8080`); server-only |
| `PORT` | Lytteport (default 3000) |

## Deploy

- Image: `ghcr.io/kongebra/status-web`.
- Push til `main` som rører `apps/status-web/**` -> `.github/workflows/status-web.yml` -> bygg + push GHCR -> promote dev/prod.
- **IngressRoute:** `status.newb.no` (eneste app med direkte DNS-tilgang; andre bruker Tailscale tailnet).
- Rollback: `gh workflow run status-web.yml -f image_tag=<tag>`.

## Observability

TanStack Start + React gir inline performance: client-side hydration, SSR timing, route-måling.
OTEL: AppHost (Aspire) håndterer lokal dev; prod sender til in-cluster otel-lgtm.

## Template (fremtid)

Go backend + TanStack Start frontend = malen for multiservice apper i clusteret.
Status-web + status-checker demonstrerer pattern: backend-api (Go) + frontend-ssr (TanStack).
