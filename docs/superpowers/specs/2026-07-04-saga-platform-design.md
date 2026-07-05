# Saga - personlig agent-plattform (v1: skall + YT-transcript-oppsummering)

Dato: 2026-07-04.
Status: godkjent design, venter implementasjonsplan.
Domene: `saga.kongebra.no`.

## Mål

Personlig agent-plattform på kongebra-clusteret som bruker den lokale Ollama-boksen (hjemmelab, GPU) som motor.
Moduler er spesialiserte assistenter.
v1 leverer plattform-skallet og én modul: YouTube-transcript-oppsummering (lim inn URL, få sammendrag, historikk).
Fremtidige moduler (dokumentopplasting, teach-me, feed-digest, plen/hus/bil-hjelpere) designes rundt men bygges ikke.

## Beslutninger

| Tema | Beslutning | Hvorfor |
|---|---|---|
| Navn | **Saga** (`saga.kongebra.no`) | Norrøn gudinne for fortelling/historie. Kort, ingen navnekollisjon (Huginn = OSS-verktøy, Mimir = Grafana-produkt) |
| Scope v1 | Skall + yt-summary-modulen | Plattformen er for stor for én spec. Dekomponert; resten er backlog i idea-bank |
| Stack | Go-API (`saga-api`) + TanStack Start-UI (`saga-web`) | To deployables. Go for worker/SSE/kø, TanStack Start for UI etter Sveins valg |
| Kø/concurrency | Arkitektur A: Postgres-kø + in-process worker + global semafor (se under) | Persistens/retry/historikk gratis, ingen ny infra (NATS/Redis droppet) |
| Push-varsler | Ikke v1 | Uavklart behov, tas senere |
| Output-språk | Sammendrag på norsk default, EN/NO-toggle i UI | Personlig bruk. Kode/URL-paths engelsk (standing convention) |

## Arkitektur

```
browser (tailnet)
  │
  ├── saga.kongebra.no        → saga-web (TanStack Start, SSR)
  └── saga.kongebra.no/api    → saga-api (Go)  ← SSE direkte til browser
                                   │
                                   ├── CNPG Postgres `saga-db` (jobber + historikk)
                                   └── Ollama http://100.125.242.93:11434/v1 (tailnet)
```

- `apps/saga-web` + `apps/saga-api` i kongebra-apps, golden path i kongebra-gitops (base + dev/prod overlays, hardened-workload, env-komponenter).
- IngressRoute splitter på path: `/api` (og `/api/events` for SSE) til saga-api, resten til saga-web.
- CNPG `saga-db` etter `apps/status/base/cluster.yaml`-mønsteret, med `_components/stateful` (prune-guard).

### Plassering: hjemmenoden (krav, ikke preferanse)

`saga-api` kjører på `k3s-home-1` (`nodeSelector home: "true"` + toleration `home=true:NoSchedule`).
YouTube rate-limiter/blokkerer datacenter-ASN aggressivt; transcript-henting trenger residential IP.
DB-podene blir på Hetzner-nodene (hcloud-volumes kan ikke attache hjemme); saga-api når `saga-db-rw` over cluster-nettet (flannel over tailscale0).
`saga-web` kan kjøre på Hetzner-nodene (ingen YT-trafikk derfra).
Konsekvens akseptert: hjemmenode nede = Saga nede (personlig verktøy, 24h-varsling finnes).

### Egress NetworkPolicy (saga-api)

- `_components/ollama-egress` (Ollama + DNS).
- 443 ut mot internett (YouTube). Husk gotcha: egress-policy = default-deny, så alt må whitelistes eksplisitt.
- Intra-cluster mot `saga-db` (CNPG-portene).

## Modul-abstraksjon (bevisst tynn)

Modul = Go-interface:

```go
type Module interface {
    Name() string                 // "yt-summary"
    InputKind() InputKind         // URL, tekst, fil (senere)
    Run(ctx context.Context, job Job, llm *LLMClient, emit func(Event)) (Result, error)
}
```

Registry = map i koden.
Ny modul = ny Go-fil + registry-entry.
Ingen plugin-system, ingen dynamisk config.
Dokumentoppsummering senere = samme summarizer-pipeline, annen input-adapter.

## Kø og concurrency (arkitektur A)

Problemet: én GPU, 6GB VRAM, én modell lastet.
Ollama har innebygd kø (`OLLAMA_NUM_PARALLEL`, `OLLAMA_MAX_QUEUE` default 512), så mange samtidige requests kollapser ikke throughput - de serialiseres.
Reell risiko er head-of-line blocking: batch-jobb med 40 chunk-kall foran et interaktivt kall.

Design:

1. **Postgres-kø**: `jobs`-tabell, worker claimer med `SELECT ... FOR UPDATE SKIP LOCKED`. Én worker-goroutine i v1. Køen og historikken er samme tabell.
2. **Global semafor**: ALL Ollama-trafikk går gjennom én Go-klient (`LLMClient`) med semafor n=1. Regelen som gjør dette globalt sant: **kun saga-api snakker med Ollama**.
3. **Preemption-punkter**: worker slipper semaforen mellom hver chunk i map-reduce. Fremtidig chat-modul kaller `LLMClient` direkte (streaming) og sniker i køen ved chunk-grensene; verste ventetid = ett chunk-kall (10-30s), ikke hele jobben.
4. **Backstop på LXC-en**: `OLLAMA_NUM_PARALLEL=1` (hver parallell slot dupliserer KV-cache i VRAM) + `OLLAMA_KEEP_ALIVE=30m` (unngå modell-reload mellom chunks).

Upgrade path (`# ponytail`): når app nr. 2 vil konsumere Ollama, løft `LLMClient` ut til egen gateway-service (tynn Go-proxy eller LiteLLM) med global concurrency/prioritet/metrics.
Ikke bygg den nå.

## Dataflyt: yt-summary

1. UI: lim inn YT-URL, velg språk (default NO) og kvalitet (default `gemma4:e4b`; opt-in "ta tiden du trenger" = `qwen3.5:9b`, batch-hastighet ~15 tok/s).
2. `POST /api/jobs` oppretter jobb-rad (status `queued`), returnerer jobb-id.
3. Worker claimer jobben, henter transcript: shell-out til `yt-dlp` standalone-binary (`--skip-download --write-auto-subs --sub-format vtt`), temp-filer til emptyDir (`readOnlyRootFilesystem` krever det).
4. VTT parses og renses (dedup av auto-caption-overlapp, timestamps av).
5. Chunking: transcript over ~kontekstvindu-terskel deles i chunks med overlapp.
   Map: sammendrag per chunk.
   Reduce: syntese av chunk-sammendragene til strukturert markdown (nøkkelpunkter, konsepter, konklusjon).
   Kort video = single-pass, ingen chunking.
6. Progress-events (`fetching`, `chunk 3/12`, token-stream fra reduce-steget) via SSE `GET /api/events?job=<id>`.
7. Ferdig sammendrag lagres som markdown i `results`, vises rendret i UI, havner i historikk-lista.

## Feilhåndtering

- Jobb-rader har `attempts` (maks 2 retries) + `error`-kolonne synlig i UI.
- Timeout per Ollama-kall (per chunk, ikke per jobb).
- `yt-dlp`-exit-kode og stderr fanges inn i jobb-feilen (typisk: ingen captions på videoen, geo-blokkert, YT-blokk).
- Pod-restart midt i jobb: claim har lease-timestamp; stale `running`-jobber requeues ved oppstart.
- Ingen egen dead-letter-kø: feilede jobber blir liggende i historikken med status `failed`, kan re-submittes fra UI.

## Testing

- Unit: VTT-parsing/rensing, chunking-grenser, prompt-bygging, kø-claim-logikk (SKIP LOCKED + lease).
- Integrasjon: ekte Ollama over tailnet (kjørbar fra dev-maskin), liten fixture-transcript.
- yt-dlp mockes med fixture-VTT i tester; ekte YT-kall kun manuelt/smoke.

## Plattform-forutsetning (gitops, egen jobb før deploy)

`*.kongebra.no` finnes ikke i clusteret i dag - alt er `*.newb.no`.
Må wires i kongebra-gitops før saga.kongebra.no lever: DNS (wildcard eller external-dns mot kongebra.no-sonen), ClusterIssuer/wildcard-cert, ev. TLSStore-oppføring.
Egen liten spec/PR i gitops-repoet; denne specen avhenger av den men eier den ikke.

## Fremtidige moduler (backlog, ikke design)

- Dokumentoppsummering: filopplasting → samme summarizer-pipeline.
- Teach-me: interaktiv lærings-dialog over et emne (chat-modulen som driver preemption-designet).
- Feed-digest, plen/hus/bil-hjelpere: se idea-bank.

## Utenfor scope v1

- Push-varsler (ntfy/Telegram).
- Auth utover tailnet (tailnet ER gaten, som resten av plattformen).
- Ollama-gateway-service (upgrade path dokumentert over).
- Chat-modul (men concurrency-designet tar høyde for den).
