# status.newb.no - design (fase 1)

Dato: 2026-06-28.
Status: design godkjent (etter 5-ekspert fan-out review, ship-with-changes), ingen kode skrevet.

## Hva phase 1 IKKE forteller deg (ærlighet)

Phase 1 måler **at en tjeneste svarer 2xx på sin health-path over den offentlige domene-veien, sett fra checker-poden, ett målepunkt, hvert 30s**. Det er tilnærmet en ekstern liveness-probe via ingress+TLS - ikke ekte brukervendt tilgjengelighet (ingen flere vantage points), og responstiden er én probe per intervall (en ping, ikke representativ UX-latens). Det holder rikelig som lab-status + mal-validering; ikke les det som SLA-måling.

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
| 5 - Auto-discovery | k8s-operator utleder targets fra annoterte IngressRoutes (erstatter statisk ConfigMap) | k8s operator/controller, CRD/RBAC, reconcile |

Hver fase = egen spec → plan → bygg, deployes og verifiseres live før neste.
Fase 5 er en parkert idé - se `docs/ideas/status-operator.md`.

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

### Probe-mål: kun domene-baserte URL-er

Checker prober **kun offentlige `https://<app>.newb.no`-URL-er** (full brukervei: tailnet-DNS → node → Traefik → TLS → app), aldri in-cluster service-DNS. Dette tester ingress+TLS+routing, ikke bare pod-liveness.

**Verifiser-først-avhengighet (KRITISK):** checker-poden bruker cluster-DNS (CoreDNS), ikke tailnet MagicDNS-resolveren. `*.newb.no` resolver derfor ikke nødvendigvis inni en pod. Før build: verifiser at en pod kan resolve + nå `https://<app>.newb.no`, ev. konfigurer CoreDNS til å forwarde `newb.no` til tailnet-resolveren. Hvis dette ikke løses, har checkeren ingenting å probe. (Web→checker-hoppet er en separat sak: det går over in-cluster service-DNS, se status-web.)

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
- Ticker (30s, hardkodet konstant med `// ponytail:`-note - én operatør, ingen env-fleksibilitet trengs) sjekker alle targets **parallelt**. Sjekk #1 kjører umiddelbart ved boot (ikke vent ett intervall).
- En sjekk: `GET url+health_path` via en **delt** `*http.Client` (se under), per-sjekk `context.WithTimeout(rootCtx, 5s)` der `rootCtx` cancelles på SIGTERM. **Følger redirects** (default); endelig 2xx = `up`, alt annet/timeout/feil = `down`. Record **endelig** `http_code` etter redirects. Mål responstid (wall-clock rundt `client.Do`).
- På `down`: sett `reason` (`timeout|conn_refused|tls_error|dns_error|http_4xx|http_5xx|other`) for diagnostikk (pre-stager phase-3 incidents). `latency_ms` = `null` ved feil (ikke `0` - 0ms down lyver).

### Concurrency-kontrakt (must-fix - data race ellers)

Go-maps er **ikke** trygge for samtidig skriving, selv til ulike nøkler. Mønster per tick:
1. Fan ut N goroutiner (`sync.WaitGroup` + results-kanal, eller `errgroup`), hver returnerer sin `Result`.
2. Bygg et ferskt snapshot (`[]Result`) når alle er ferdige.
3. Bytt snapshot atomisk: `atomic.Pointer[[]Result]`. API-handleren leser lock-fritt via `.Load()` → konsistent snapshot, ingen mutex på hot read-path.

En treg/død target blokkerer ikke de andre (per-sjekk context-timeout). Checker er selv alltid `up` så lenge prosessen lever; en target nede er ikke en checker-feil.

### Delt HTTP-klient (must-fix - poller-footgun)

ÉN `*http.Client` bygget ved oppstart (aldri `http.Get`/`DefaultClient` per sjekk), med eksplisitt `http.Transport`: sett `MaxIdleConnsPerHost`/`MaxConnsPerHost` bevisst (default `MaxIdleConnsPerHost=2` serialiserer gjenbruk og undergraver parallell-garantien) + `IdleConnTimeout`. Alltid `io.Copy(io.Discard, resp.Body)` + `Close()` (ellers brytes connection-reuse). Timeout via context, ikke `Client.Timeout` (ikke halvkonfigurer begge).

### API

- `GET /api/status` → `{"checked_at": "<rfc3339>", "services": [Service...]}`
- Service-kontrakt (eksakt, dette er cross-service-kontrakten med status-web sin TS-loader):
  | felt | type | note |
  |------|------|------|
  | `name` | string | |
  | `url` | string | |
  | `status` | enum | `"up"` \| `"down"` \| `"unknown"` (ikke-sjekket-enda, mellom boot og første resultat) |
  | `latency_ms` | integer \| null | null ved down/unknown |
  | `http_code` | integer \| null | endelig kode etter redirects; null ved unknown |
  | `reason` | string \| null | satt kun ved down |
  | `last_checked` | string \| null | RFC3339; null ved unknown |
- `GET /health` → `{"status":"ok"}` (for k8s-probes; alltid up så lenge prosessen lever)
- OTEL: gjenbruk go-hello-world-mønsteret EKSAKT - `semconv/v1.26.0`, no-op når `OTEL_EXPORTER_OTLP_ENDPOINT` er tom, SIGTERM-flush, otelhttp auto-spans, k8s-semconv resource (`k8s.pod.name`/`k8s.node.name`/`k8s.namespace.name` via Downward API).
- **Per-target metrics (near-free, gjør appen verdt sin plass i otel-lgtm):** gauge `target_up{name}` + histogram `target_latency_ms{name}` via samme periodic-reader-mønster (ikke et eget `/metrics`-endepunkt). Gir Grafana-paneler nå + phase-2 uptime-% starter med ekte historikk.

### Env

| Var | Effekt |
|-----|--------|
| `PORT` | lytteport (default 8080) |
| `CONFIG_PATH` | sti til targets.yaml (montert ConfigMap) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP-mål; tom = OTEL av |
| `NODE_NAME` / `POD_NAMESPACE` | Downward API → k8s-attributter |

(Intervall/timeout er hardkodede konstanter, ikke env - `// ponytail:` med oppgraderingssti hvis det noen gang trengs.)

### ConfigMap-reload (must-fix - "legg til linje" funker ikke uten dette)

Config parses kun ved boot. En data-only ConfigMap-endring restarter **ikke** poden, så nye targets ville aldri dukke opp. Løsning: gitops bruker kustomize `configMapGenerator` (hash-suffiks på ConfigMap-navnet) → endring i `targets.yaml` gir nytt navn → ArgoCD ruller deployment automatisk. Restart-to-reload er et bevisst phase-1-valg (matcher fail-fast + GitOps); `fsnotify`/SIGHUP er senere.

## status-web (TanStack Start, SSR)

- **Én `createServerFn`** (must-fix - ikke loader + separat proxy = duplisert fetch som drifter). Server-fn-en kaller checker over in-cluster DNS (`${CHECKER_URL}/api/status`) og er den ENESTE som kjenner `CHECKER_URL` + gjør fetch/parse/feilhåndtering. Den kjører server-side under SSR (første paint har ferdig status) OG eksponeres som auto-generert RPC-endepunkt klienten kaller ved refresh. Nettleseren når aldri checker direkte (in-cluster DNS er uansett uresolverbart fra browser på tailnet).
- **Client auto-refresh:** `setInterval` (eller `router.invalidate()` på intervall) kaller samme server-fn hvert 30s. Én refresh-pattern, ikke to. (TanStack Query er roadmap-oppgradering når et andre data-behov dukker opp, ikke phase 1.)
- **Grid:** ett kort per tjeneste - navn, grønn/rød-badge, responstid (ms), "sjekket for X sek siden". Funksjonell, ren, responsiv. Ingen designsystem i fase 1.
- **Hydration:** render absolutt `last_checked` server-side, humaniser ("for X sek siden") FØRST etter mount - ellers `Date.now()`-mismatch mellom SSR og klient.
- **Feil:** server-fn kan ikke nå checker → render feilbanner ("kan ikke nå checker"), ikke hvit skjerm.
- **`GET /health`:** egen SSR-server-route som returnerer 200 uavhengig av checker (for k8s-probe). Web sin readiness er **kun selv-avhengig** - ALDRI gated på checker-nåbarhet (ellers tar en checker-nedetid ned status-siden, stikk motsatt av poenget).

### SSR build + Docker (must-fix - mal-presisjon, alle fremtidige apper kopierer dette)

- Kjør framework-ets SSR-server-output (Nitro/`node-server`-preset, f.eks. `.output/server/index.mjs`), **ikke** `vite preview`. Pin TanStack Start-versjon.
- Multi-stage Dockerfile: build-stage med full `node_modules`, runtime-stage med kun `.output` + prod-deps på en slank Node-base (web er eneste tailnet-nåbare tjeneste). Ikke distroless (Node SSR-runtime kreves) - bevisst avvik fra distroless-regelen, kun for frontend-tjenester.
- Probe: `httpGet /health` (ikke shell - gjelder selv på Node-base for konsistens).

### Env

| Var | Effekt |
|-----|--------|
| `CHECKER_URL` | in-cluster service-DNS til checker, **env-suffikset namespace**: `http://status-checker.status-checker-<env>.svc.cluster.local:8080`. Injiseres ulikt i dev/prod-overlay (cross-namespace `status-web-<env>` → `status-checker-<env>`). |
| `PORT` | SSR-server lytteport |

### Mønster-merknad

Proxy-route-mønsteret er nøkkelen til to-tjeneste-SSR: server-side (loader + proxy) snakker in-cluster DNS, klienten snakker bare sin egen origin. Checker eksponeres aldri på internett. Dette blir **templaten** for alle fremtidige TanStack + Go-apper på clusteret.

## Testing (per-app, i `<app>-pr.yml`)

- **checker:** unit-test sjekk-logikk mot `httptest.Server`: 2xx=up, 4xx/5xx=down m/riktig `reason`, timeout=down, redirect-følging (301→200=up, endelig http_code), latency satt på up / null på down, `unknown`-state før første sjekk. Test config-parsing (gyldig/ugyldig/tom). Kjør med `-race` (fanger concurrency-kontrakten). `go vet ./...` + `go test -race ./...`.
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

- `apps/status-checker/{base,overlays/dev,overlays/prod}`. ConfigMap via **`configMapGenerator`** (hash-suffiks → endring ruller deployment automatisk).
- `apps/status-web/{base,overlays/dev,overlays/prod}` med IngressRoute for `status.newb.no` (prod) / dev-variant.
- Per-app-per-env namespace per gitops-konvensjon. `CHECKER_URL` injiseres **ulikt per env** i web-deployment (env-suffikset namespace, se Env-tabell).
- Følger build-once-promote: CI bygger immutabel tag, pinner digest i overlay, dev auto + prod gated.

### Manifest-kontrakt (mandatert her, ikke overlatt implisitt - "production quality" på små noder)

- **Probes:** begge tjenester `httpGet /health` (distroless/Node har ingen shell - AGENTS.md-felle). Web readiness = selv-avhengig, aldri checker-gated.
- **Resources (start):** checker `requests 25m/32Mi, limits 250m/64Mi`; web `requests 50m/128Mi, limits 500m/256Mi`. CX33-noder, juster etter måling.
- **Checker replicas = 1** i phase 1 (in-memory state; flere replicas = divergerende resultater + flap avhengig av hvilken pod web treffer). AGENTS.md "replicas != HA" - phase 2 (Postgres) muliggjør skalering.
- **Self-monitoring:** legg `status-web` (`https://status.newb.no/health`) inn som target i ConfigMap så siden vokter seg selv. Checker har ingen domene-URL (in-cluster only), så den voktes via **OTEL-heartbeat**: Grafana varsler når `target_up`-metrics slutter å strømme (= checker død), selv om status-web fortsatt svarer. Dekker blind flekken at status-web er eneste tailnet-vendte komponent.
- **NetworkPolicy (intent):** default-deny + allow-from-`status-web` til checker:8080. `// ponytail:` - aspirasjonell til Cilium lander (k3s default flannel håndhever ikke NetworkPolicy).

## Verifisering (definition of done, fase 1)

- **Forhåndssjekk (blokkerende):** verifisert at en pod kan resolve + nå `https://<app>.newb.no` (CoreDNS→tailnet), ellers har checker ingenting å probe.
- Push til `apps/status-checker/**` og `apps/status-web/**` bygger + deployer via eksisterende CI (digest-pinnet, prod gated).
- `https://status.newb.no` viser grid med alle konfigurerte tjenester, korrekt grønn/rød + responstid (probet via domene-URL).
- Ta en target ned → grid viser den `down` m/`reason` innen ett intervall; ta den opp → `up` igjen.
- Client auto-refresh oppdaterer uten full page-reload (samme server-fn som SSR).
- ConfigMap-endring (`configMapGenerator`) ruller checker-deployment → nytt target dukker opp etter sync.
- checker eksponeres IKKE på internett (ingen IngressRoute); kun web er nåbar.
- status-web + status-checker overvåker hverandre (i ConfigMap); Grafana har `target_up`/`target_latency_ms` per target + traces fra begge tjenester (`service.name=status-checker`/`status-web`).

## Referanser

- Repo-konvensjoner: `AGENTS.md` (CI/CD, PR-sikkerhetsregler, "lag ny app").
- ADR-0001 build-once-promote; go-hello-world som OTEL/k8s-mønster.
- gitops: `kongebra-gitops` (base/overlays, per-app-per-env namespace).
