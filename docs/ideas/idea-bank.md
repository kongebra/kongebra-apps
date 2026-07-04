# Idébank: apper for kongebra-plattformen

Status: **ÅPEN** - idébank, ingen beslutning tatt.
Dato: 2026-07-02 (brainstorm-sesjon).

Rangeringskriterium besluttet: **portefølje/moro først**, inntekt er bonus, ikke krav.
Form: **mange små apper** - hver app skal øve minst én plattform-muskel clusteret ikke har brukt ennå (cron, kø, SSE, object storage, OAuth, PostGIS, LLM-integrasjon).
Prinsipp: velg apper baklengs - ikke "hva vil jeg bygge" men "hvilken kapabilitet mangler trening".

## Gratis poeng - ting som allerede finnes

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Ratatoskr på kongebra** | MAR art.19 innsidehandel-feed. Spec ferdig (26 beslutninger, SPEC.md i ratatoskr-repo), null kode. Var planlagt Railway - deploy hjemme i stedet | Ingest-pipeline, CNPG Postgres, goose-migrasjoner, cron-polling | M |
| **DrinkJakt på kongebra** | M0-M6 ferdig, kun M7 deploy igjen. Railway+R2 var planen - kongebra + object storage i stedet | SSR i k8s, object storage (R2 eller MinIO), sitemap/SEO bak ingress | S |

## Dev-verktøy

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Shiplog** | Lytter på ArgoCD-webhooks, lagrer hver sync i Postgres, viser "hva deployet når" per env. Dashboard over egen plattform | Webhooks, events, tidslinje-UI | S |
| **Heartbeat** (dead man's snitch) | Cron-jobber pinger inn; mangler ping gir ntfy/Telegram-varsel. Nyttig for andre, kan monetiseres senere | Cron, alerting, tidsvinduer | S |
| **Webhook-inbox** (requestbin-klone) | Fang, inspiser, replay webhooks | SSE/websockets, TTL-opprydding | S |
| **Go-links** (`go/standup`) | Intern URL-shortener. Liten, kjedelig, brukes daglig | DB, latency, metrics | XS |

## Data / sanntid

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Politikart** 💎 | Kart-app over Politiloggen. Se egen seksjon under | PostGIS, geokoding, SSE, kart-frontend | M |
| **Poljakt** | Vinmonopolet-tracker koblet mot DrinkJakt: pris/lager på ingredienser via Vinmonopolets API, "hvilke cocktails fra Mitt barskap kan jeg fullføre med ett kjøp på nærmeste pol" | Scraping/ingest, prishistorikk, diffing | M |
| **Norsk sanntidsboard** | Én dash: Entur-avganger, Yr, strømpris (hvakosterstrommen.no), NVE vannstand | Pollere, cache, SSE, rate limits | M |
| **Strømpris-varsler** | Spot under terskel gir push via ntfy | Cron, notifikasjoner | XS |
| **Konkursradar** | BRREG kunngjøringer: nye konkurser/tvangsavviklinger i valgt bransje/region som digest. B2B-monetiserbar senere | Cron-ingest, diffing, e-post | S |
| **Smilefjes-kart** | Mattilsynet smilefjes: "sjekk før du booker"-kart, varsel når favorittrestaurant får anmerkning | Bulk-ingest, diffing | S |
| **Badetemp-bot** | MET Frost + Havvarsel: "Korsvika 14 grader, sol fra 12" hver sommermorgen | Cron, API-kombinering | XS |
| **Pendlerdash** | Vegvesen trafikkdata + webkamera: kjøretid, kamera, hendelser på din strekning | Datex-ingest, bildeproxy | M |
| **Kommune-sammenligner** | SSB PxWeb: din kommune mot nabo på 20 nøkkeltall | Statistikk-API, dataviz | S |
| **Kraft-Norge-dash** | NVE magasinfylling + spotpris + Statnett-flyt i én graf | Tidsserier, dataviz | S |
| **Skipskart** | BarentsWatch AIS: live skipstrafikk Trondheimsfjorden, varsler på spesialskip | Websocket-stream, OAuth client-credentials, kart | M |

## Hobby / kultur

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Scrobble-stats** | Spotify-lytting til egen DB, "wrapped" hele året | OAuth-flow, cron-ingest, aggregering | S |
| **Klatre-/treningslogg** | PWA, offline-first, kun personlig bruk | PWA, service worker | S |

## AI / agenter

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Stortingsradar** 💎 | Klarspråk-lag over Stortingets data med små modeller. Se egen seksjon under | Batch-LLM, kø, embeddings, digest-infra | L (flaggskip) |
| **Feed-digest** | RSS/HN/nyheter som LLM-oppsummert dagsdigest til e-post/ntfy | Kø (NATS), LLM-API, cron | M |
| **GitHub-triage-bot** | Repo-events som LLM oppsummerer/labler til Slack/ntfy | Webhooks, LLM, GitHub App | M |

## Plattform-modenhet / drift

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Loke** (chaos-agent) | Planlagte "game days" mot eget cluster: dreper pods, kutter nettverk (netpol/toxiproxy), fyller disk. Måler blast radius via Grafana-stacken, rapporterer "overlevde vi?". Beviser at 3-node HA faktisk holder | Chaos engineering, destruktiv k8s-API, resilience-verifisering | S-M |
| **Draugen** (restore-drill) | CronJob som jevnlig restorer CNPG-backup inn i scratch-namespace, kjører checksums/row-counts mot prod, sletter, rapporterer. Backup uten testet restore er bare håp | Backup/restore-drill, k8s Jobs, CNPG PITR | S |

## Sanntid / compute

| Idé | Hva | Muskel | Innsats |
|---|---|---|---|
| **Kappleik** (party-quiz) | Kahoot-aktig sanntidsquiz: host lager rom, folk joiner på mobil med kode, server-autoritativ state. Websocket-state over flere replicas (Redis/NATS pub/sub eller sticky sessions) er den ekte leksjonen | Server-autoritativ multiplayer, rom-state, websocket scale-out | M |
| **Smie** (media-pipeline) | Last opp video -> ffmpeg-workers klipper/transkoder/gif-er -> object storage -> delbar lenke. Workers er k8s Jobs skalert av KEDA på kølengde | KEDA, k8s Jobs, CPU-tunge workers, opplasting | M |
| **Brønn** (SSH-app) | Cluster-dashboard servert over SSH (charmbracelet wish/bubbletea): `ssh status.newb.no` gir TUI med deploys (Shiplog-data), pods, alerts. Tvinger Traefik TCP-ruting, host-keys som secrets, ikke-HTTP health | TCP ingress, SSH-server i k8s, TUI | S-M |

Synergi: Brønn forutsetter Shiplog. Draugen forutsetter CNPG (infra-handoff B).
Bonus hvis noen faller: ActivityPub-bot som publiserer Politiloggen/strømpris til Fediverse (HTTP signatures + federation-muskel).

## Flaggskip-kandidat: TrønderLeikan (inmeta-games)

Multi-tenant event-/turneringsplattform (brackets, tidtagning/lap-times, arrangementer).
Bevisst "nesten unødvendig" microservice-arkitektur - målet er å øve distribuert systemdesign skikkelig: Zitadel (auth/tenants), RBAC (gjest/spiller/arrangør/moderator/admin), Postgres (én DB, logisk tenant-skille), Redis, event-basert async (broker TBD), flere frontends (app + admin-plane på tvers av tenants).
Go backend, TanStack Start frontend.
Status: **SPEC VEDTATT 2026-07-04** - se `apps/tronderleikan/docs/SPEC.md` + `PLAN.md` (4 faser, agent-arbeidspakker).

## Parkert

- **DiscJakt** (prisjakt.no for discgolf mot norske e-com): domenet dødt (mistet interessen for discgolf), men mønsteret (ingest, normaliser, historikk, diff/varsel) gjenbrukes i Poljakt.
  Bygges Poljakt først, er DiscJakt senere bare en ny adapter.

## Deep-dive: Politikart (M)

Kart-app over Politiloggen (politiets operative meldinger, NLOD 2.0, `api.politiloggen.politiet.no`).

Datastruktur i API-et: tråd-basert (hendelse = MessageThread med oppdateringer).
Felter: fritekst, kategori, distrikt (12 politidistrikt), kommune, område (stedsnavn-fritekst), tidsstempler, isActive, ev. bilde.
Ferdige RSS/Atom-feeds finnes også.
**Ingen koordinater** - geokoding er vår jobb.
Spec sier "under utvikling, format kan endres uten forvarsel" (mai 2026): lagre rå JSON, parse i eget steg.

To-nivå geo-løsning:

1. **Nivå 1 (trygg, dag 1)**: kommune-choropleth. Kommunepolygoner gratis fra Geonorge/Kartverket, tell hendelser per kommune x kategori x tidsvindu.
2. **Nivå 2**: geokod `area` mot Kartverkets stedsnavn-API scopet til kommunen. Cache alt, confidence-flagg, fallback til kommune-sentroide. Gir punkt-heatmap.

Features:

- Live-modus: nye hendelser popper med puls-markør (SSE), aktive tråder lyser
- Tidsslider + replay ("spol gjennom helga")
- Heatmap per kategori som togglebare lag, filter på tid og sted
- Normalisering per innbygger (SSB) - ellers vinner Oslo alltid
- "Min kommune": feed + døgnmønster-stats + varsel-abo (gjenbruker varsel-infra fra jakt-mønsteret)
- Mønster-stats: hendelser per ukedag/time, sesongvariasjon
- **Eget arkiv = egen verdi**: ukjent retention hos politiet. Rå-lagring fra dag 1 gir etter et år et datasett ingen andre har. Journalist-appell.
- LLM-berikelse senere (Haiku): strukturer fritekst til alvorlighet/involverte/utfall for skarpere filtre

Teknisk: MapLibre GL + Kartverket-tiles (gratis), Go-ingest, CNPG + **PostGIS** (geo-muskel plattformen mangler).
NLOD krever kildeangivelse.

## Deep-dive: Stortingsradar (L, flaggskip-kandidat)

Klarspråk- og påvirknings-lag over `data.stortinget.no` (saker, voteringer per representant, spørretimen, innstillinger, referater).
Rådata er komplett men uleselig for folk flest - LLM-laget er verdien.

Prior art: **Holder de ord** gjorde voteringssporing/løftebrudd, men er nedlagt (siden død per juli 2026).
Koden ligger åpen på github.com/holderdeord - gruv datamodell-erfaring derfra.
Rommet står ledig, og LLM-klarspråk-laget hadde de aldri.

MVP-trapp:

1. **Klarspråk-lista**: ingest saker/voteringer, Haiku gir 3 setninger + "hva betyr dette" + temakategori, filtrerbar web-liste. Alene verdt å shippe.
2. **Tema-digest**: abonner på helse/samferdsel/..., ukesmail "dette skjedde, dette kommer"
3. **Representant-profiler**: stemmehistorikk, avvik fra partilinje, oppmøte - rene API-tall, ingen LLM
4. **Stemmematch** 💎: svar på 20 faktiske voteringer, få "du stemmer 78% som SV". Valgomat på ekte stemmer i stedet for valgløfter - ingen har laget dette, viralt ved valg.
5. **Varsel før votering**: "i morgen behandles sak du følger" - påvirkning før votering, ikke etterpå

Guardrails (ufravikelige):

- Harde fakta (stemmetall, navn, datoer) går alltid rett fra API til UI, aldri gjennom modellen
- LLM får kun oppgaver der "omtrent riktig" er ok: sammendrag, kategorisering
- Alt LLM-generert merkes "maskinoppsummert" + kildelenke til stortinget.no

Kostnad: Haiku på dette volumet er kroner per måned, ikke hundrelapper.

## Delt skjelett (arkitektur-observasjon)

Politikart, Stortingsradar, Poljakt, Konkursradar og Smilefjes-kart deler samme skjelett:
cron-ingest -> rå-lagring -> berikelse (geokoding/LLM/diff) -> abonnement/varsel -> web.
Bygg varsel-infra (ntfy/e-post + per-bruker subscriptions) én gang, så er hver ny kilde bare en adapter.
Rekkefølgen small-to-flagship er dermed også en teknisk trapp: Politikart først gjør Stortingsradar til ~60% gjenbruk.

## Anbefalt startrekkefølge (fra brainstorm)

1. **Shiplog** - liten, viser frem plattformen selv
2. **Ratatoskr** - spec klar, mest "ekte system"
3. **Strømpris-varsler** - XS, cron+push-muskel på en kveld
4. Deretter: Politikart som første "ordentlige" offentlig-data-app, Stortingsradar som flaggskip når infra-gjenbruket er på plass

## Ressurser: hvor finne offentlige API-er

- **data.norge.no/data-services** - Digdirs Felles datakatalog, offisiell API-oversikt
- **data-norge.nystad.io** - community-vedlikeholdt kompakt register (bra, men "vis alt på en side" er tung)
- Etatportaler: Entur, Kartverket/Geonorge, BRREG, MET/Frost, SSB PxWeb, Vegvesen, BarentsWatch, Helsedirektoratet, NAV/arbeidsplassen, Vinmonopolet
- ~~Tadata~~ - nedlagt
- github.com/holderdeord - prior art for Stortingsradar
