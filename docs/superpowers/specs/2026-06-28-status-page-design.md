# status.newb.no - design (fase 1)

Dato: 2026-06-28.
Status: design godkjent, ingen kode skrevet.

En status-side for kongebra-laben: viser opp/ned + responstid for hver tjeneste i clusteret, nådd på `status.newb.no` (tailnet-only).
Dette er **app #2** på k3s-laben og setter **templaten for Go + TanStack Start-apper** på clusteret (to tjenester, ekte SSR).

## Mål (fase 1)

- Live opp/ned-oversikt over et konfigurert sett tjenester, med responstid og "sist sjekket".
- Validere full Go + TanStack Start-stack ende-til-ende på clusteret (to-tjeneste-mønster, SSR, GitOps, CI).
- Holde produksjonskvalitet (OTEL, probes, distroless der det går, tester i PR).

## Ikke-mål (senere faser, se Roadmap)

- DB/uptime-historikk, incident-tidslinje, varsling, k8s-auto-discovery, auth (tailnet er gaten).

## Roadmap

| Fase | Innhold | Lærer |
|------|---------|-------|
| **1 - Basics** | Go-checker poller `/health` på ticker, holder siste status i minne; TanStack Start SSR-grid | App-skjelett: Go-API + TanStack Start + GitOps for 2-komponent-app |
| 2 - State | Postgres, lagre sjekk-historikk, uptime-% (24t/7d/30d) + historikk-strip | Stateful workload: PVC/StatefulSet/backup |
| 3 - Incidents | Utled nedetid-perioder fra historikk, tidslinje | Domenelogikk + UI |
| 4 - Varsling | Telegram-push ved statusendring | Outbound + delt notify-modul |

Hver fase = egen spec → plan → bygg, deployes og verifiseres live før neste.

## Arkitektur (to tjenester)

```
ConfigMap (targets: navn,url,health-path)
        │
        ▼
┌─────────────────┐   in-cluster    ┌──────────────────────┐
│ status-checker  │◄────────────────│ status-web           │
│ (Go, distroless)│   GET /api/...  │ (TanStack Start, SSR)│
│ ticker → poll   │                 │ SSR-render grid      │
│ targets/health  │                 │ + client auto-refresh│
│ siste status    │                 └──────────┬───────────┘
│ i minne         │                            │ websecure
└─────────────────┘                   status.newb.no (Traefik+TLS)
```

- **status-checker** (Go, distroless): leser ConfigMap, poller hver target sin health-path på en ticker, holder siste resultat i minne, eksponerer JSON-API. Ingen IngressRoute (kun in-cluster).
- **status-web** (TanStack Start, Node SSR): SSR-rendrer grid ved å kalle checker over in-cluster DNS; eksponerer en tynn proxy-route for client auto-refresh. Eneste tjeneste med IngressRoute (`status.newb.no`).
- ConfigMap mountes i checker. Ny tjeneste = legg til linje + ArgoCD-sync.

## status-checker (Go)

### ConfigMap-format (`targets.yaml`)

```yaml
targets:
  - name: go-hello-world
    url: https://go-hello-world.newb.no
    health_path: /health
  - name: argocd
    url: https://argocd.newb.no
    health_path: /healthz
```

### Logikk

- Parse config ved oppstart. Ugyldig/manglende config → fail fast (`log.Fatal`). Tom target-liste = tomt API-svar (ikke crash).
- Ticker (default 30s, `CHECK_INTERVAL` env) sjekker alle targets **parallelt**. Sjekk #1 kjører umiddelbart ved boot (ikke vent ett intervall).
- En sjekk: `GET url+health_path`, timeout 5s (`CHECK_TIMEOUT` env). 2xx = `up`, alt annet/timeout/feil = `down`. Mål responstid.
- Hold `map[name]Result{status, latency_ms, last_checked, http_code}` bak en mutex. En treg/død target blokkerer ikke de andre (per-sjekk timeout, hver skriver sitt eget felt).
- Checker er selv alltid `up` så lenge prosessen lever; en target nede er ikke en checker-feil.

### API

- `GET /api/status` → `{"checked_at": "<rfc3339>", "services": [{name, url, status, latency_ms, http_code, last_checked}]}`
- `GET /health` → `{"status":"ok"}` (for k8s-probes)
- OTEL: gjenbruk go-hello-world-mønsteret (otelhttp auto-spans + k8s-semconv resource: `k8s.pod.name`/`k8s.node.name`/`k8s.namespace.name` via Downward API).

### Env

| Var | Effekt |
|-----|--------|
| `PORT` | lytteport (default 8080) |
| `CONFIG_PATH` | sti til targets.yaml (montert ConfigMap) |
| `CHECK_INTERVAL` | poll-intervall (default 30s) |
| `CHECK_TIMEOUT` | per-sjekk timeout (default 5s) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP-mål; tom = OTEL av |
| `NODE_NAME` / `POD_NAMESPACE` | Downward API → k8s-attributter |

## status-web (TanStack Start, SSR)

- **SSR loader** kaller checker over in-cluster DNS (`${CHECKER_URL}/api/status`) ved hver page-render → første paint har ferdig status.
- **Client auto-refresh:** hvert 30s re-fetcher klienten. Klienten kan ikke nå in-cluster DNS, så web eksponerer en tynn **proxy-route** (`/api/status`) som videresender til checker server-side. Klienten kaller sin egen origin.
- **Grid:** ett kort per tjeneste - navn, grønn/rød-badge, responstid (ms), "sjekket for X sek siden". Funksjonell, ren, responsiv. Ingen designsystem i fase 1.
- **Feil:** loader/proxy kan ikke nå checker → render feilbanner ("kan ikke nå checker"), ikke hvit skjerm; proxy returnerer 5xx ved checker-feil.

### Env

| Var | Effekt |
|-----|--------|
| `CHECKER_URL` | in-cluster DNS til checker (f.eks. `http://status-checker.status-checker:8080`) |
| `PORT` | SSR-server lytteport |

### Mønster-merknad

Proxy-route-mønsteret er nøkkelen til to-tjeneste-SSR: server-side (loader + proxy) snakker in-cluster DNS, klienten snakker bare sin egen origin. Checker eksponeres aldri på internett. Dette blir **templaten** for alle fremtidige TanStack + Go-apper på clusteret.

## Testing (per-app, i `<app>-pr.yml`)

- **checker:** unit-test sjekk-logikk (2xx=up, timeout=down, latency) mot `httptest.Server`; test config-parsing (gyldig/ugyldig/tom). `go vet ./...` + `go test ./...`.
- **web:** bygg + typecheck i PR. Komponenttester droppes i fase 1 (YAGNI).

## Repo-layout

```
apps/status-checker/      # Go, eget go.mod, Dockerfile (distroless)
apps/status-web/          # TanStack Start, package.json, Dockerfile (node SSR)
.github/workflows/
  status-checker.yml + status-checker-pr.yml
  status-web.yml + status-web-pr.yml
```

- To apper = to caller-par (følger AGENTS.md-mønsteret). `docker-build` / `gitops-promote` / `_build-deploy.yml` gjenbrukes uendret.
- `status-web` bygger ikke distroless (Node SSR-runtime kreves); checker er distroless. Health checks: checker via innebygd `/health`; web via SSR-server-endpoint.

## GitOps (kongebra-gitops, eget repo)

- `apps/status-checker/{base,overlays/dev,overlays/prod}` + ConfigMap i base.
- `apps/status-web/{base,overlays/dev,overlays/prod}` med IngressRoute for `status.newb.no` (prod) / dev-variant.
- Per-app-per-env namespace per gitops-konvensjon. `CHECKER_URL` injiseres i web-deployment.
- Følger build-once-promote: CI bygger immutabel tag, pinner digest i overlay, dev auto + prod gated.

## Verifisering (definition of done, fase 1)

- Push til `apps/status-checker/**` og `apps/status-web/**` bygger + deployer via eksisterende CI (digest-pinnet, prod gated).
- `https://status.newb.no` viser grid med alle konfigurerte tjenester, korrekt grønn/rød + responstid.
- Ta en target ned → grid viser den `down` innen ett intervall; ta den opp → `up` igjen.
- Client auto-refresh oppdaterer uten full page-reload.
- checker eksponeres IKKE på internett (ingen IngressRoute); kun web er nåbar.
- Telemetri fra begge tjenester i Grafana/Tempo (`service.name=status-checker`/`status-web`).

## Referanser

- Repo-konvensjoner: `AGENTS.md` (CI/CD, PR-sikkerhetsregler, "lag ny app").
- ADR-0001 build-once-promote; go-hello-world som OTEL/k8s-mønster.
- gitops: `kongebra-gitops` (base/overlays, per-app-per-env namespace).
