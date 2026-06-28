# status-checker

HTTP-tjeneste som sjekker helse på andre tjenester.
Prober kun domene-URL-er (*.newb.no), periodisk via konfigurable targets (ConfigMap).
Stateless, distroless, instrumentert med OpenTelemetry. Se [root AGENTS.md](../../AGENTS.md) for repo-konvensjoner.

## Endepunkter

- `GET /api/status` -> JSON array med status per target:
  ```json
  [
    {
      "name": "go-hello-world",
      "url": "https://hello.newb.no/health",
      "status": "up",
      "latency_ms": 42,
      "http_code": 200,
      "reason": "",
      "last_checked": "2026-06-28T12:34:56Z"
    }
  ]
  ```
  Mulige `status`: `up`, `down`, `unknown`.
  `reason`: feilmelding dersom `down` (timeout, DNS, HTTP-error, osv.).

- `GET /health` -> `{"status":"ok"}` (JSON, rask, ingen deps)

## Bygg og kjør

```sh
go run .                       # PORT=8080 default; OTEL no-op uten endpoint
go build -o /tmp/app .
```

- Image: `gcr.io/distroless/static-debian12` (ingen shell). Dockerfile bygger statisk binær.

## Konfigurering

Targets defineres i `targets.yaml` (referert fra `CONFIG_PATH`):

```yaml
targets:
  - name: "go-hello-world"
    url: "https://hello.newb.no/health"
    health_path: "/health"  # alternativt: spesifikt endepunkt å probe
  - name: "api"
    url: "https://api.newb.no"
    health_path: "/health"
```

Kun domene-URL-er ellers `*.newb.no` godtas (sikkerhetssperring: ingen in-cluster DNS-probing).

## Env

| Var | Effekt |
|-----|--------|
| `PORT` | lytteport (default 8080) |
| `CONFIG_PATH` | sti til `targets.yaml` (default `./targets.yaml`) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP-mål; **tom = OTEL av** (no-op, lokal kjøring funker) |
| `NODE_NAME` | k8s-node (Downward API `spec.nodeName`), mappes til `k8s.node.name` |
| `POD_NAMESPACE` | k8s-namespace (Downward API `metadata.namespace`), mappes til `k8s.namespace.name` |

## Probing

- Intervall: 30 sekunder (hardkodet konstant)
- Timeout per probe: 5 sekunder (hardkodet konstant)
- Metode: HTTP GET mot `url` + `health_path` (eller kun `url` dersom health_path er tomt)
- Feil: timeout, DNS-oppslag, HTTP-status utenfor 2xx blir rapportert som `status: down`

## OTEL

- `otelhttp` gir auto server-spans (navngitt per rute) + `http.server`-metrics.
- OTLP HTTP til `OTEL_EXPORTER_OTLP_ENDPOINT` (`http://`-scheme = insecure). Resource: `service.name`, `service.version`, `k8s.pod.name` (alltid, fra hostname), `k8s.node.name` + `k8s.namespace.name` (kun når Downward API-env er satt).
- Per-target metrics:
  - `target_up` (gauge, 0/1): om målet er oppe
  - `target_latency_ms` (histogram): responstid i ms
- Graceful shutdown på SIGTERM flusher telemetri (k8s-vennlig: pod får SIGTERM ved rolling update/evict).

## Deploy

- Image: `ghcr.io/kongebra/status-checker`.
- ConfigMap `status-checker-config` monteres på `CONFIG_PATH`.
- Replika: 1 (stateless; kun en instans proben; ingen HA-krav).
- **Ingen IngressRoute** - kun in-cluster bruk (status.newb.no håndteres som del av dashbord/UI, ikke via Traefik).
- Push til `main` som rører `apps/status-checker/**` -> `.github/workflows/status-checker.yml` -> bygg + deploy.
- Rollback: `gh workflow run status-checker.yml -f image_tag=<tag>`.
