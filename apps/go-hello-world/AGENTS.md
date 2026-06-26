# go-hello-world

Minimal Go HTTP-tjeneste. Vehikkel for å teste deploy-pipelinen (CI -> GHCR -> Dokploy) og observability.
Stateless, distroless, instrumentert med OpenTelemetry. Se [root AGENTS.md](../../AGENTS.md) for repo-konvensjoner.

## Endepunkter

- `GET /` -> `Hello World`
- `GET /health` -> `{"status":"ok"}` (JSON, rask, ingen deps)
- `GET /version` -> `{"version":"<tag>"}` (innebygd versjon)
- `GET /whoami` -> `{"pod":<pod>,"node":<k8s-node>,"namespace":<ns>,"version":<tag>}` (for å se LB/plassering)

## Bygg og kjør

```sh
go run .                       # PORT=8080 default; OTEL no-op uten endpoint
go build -ldflags "-X main.version=$(git rev-parse --short HEAD)" -o /tmp/app .
```

- Versjon bakes ved build via `-ldflags "-X main.version=<tag>"`. Env `VERSION` overstyrer.
- Image: `gcr.io/distroless/static-debian12` (ingen shell). Dockerfile bygger statisk binær.

## Env

| Var | Effekt |
|-----|--------|
| `PORT` | lytteport (default 8080) |
| `VERSION` | overstyrer innebygd versjon |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP-mål; **tom = OTEL av** (no-op, lokal kjøring funker) |
| `NODE_NAME` | k8s-node (Downward API `spec.nodeName`), mappes til `k8s.node.name` |
| `POD_NAMESPACE` | k8s-namespace (Downward API `metadata.namespace`), mappes til `k8s.namespace.name` |

## OTEL

- `otelhttp` gir auto server-spans (navngitt per rute) + `http.server`-metrics.
- OTLP HTTP til `OTEL_EXPORTER_OTLP_ENDPOINT` (`http://`-scheme = insecure). Resource: `service.name`, `service.version`, `k8s.pod.name` (alltid, fra hostname), `k8s.node.name` + `k8s.namespace.name` (kun når Downward API-env er satt).
- Graceful shutdown på SIGTERM flusher telemetri (k8s-vennlig: pod får SIGTERM ved rolling update/evict).

## Deploy

- Image: `ghcr.io/kongebra/go-hello-world`. Dokploy applicationId: `86e3IUkJ3jtIro6GoOWdL` (production-env).
- Push til `main` som rører `apps/go-hello-world/**` -> `.github/workflows/go-hello-world.yml` -> bygg + deploy.
- Rollback: `gh workflow run go-hello-world.yml -f image_tag=<tag>`.
