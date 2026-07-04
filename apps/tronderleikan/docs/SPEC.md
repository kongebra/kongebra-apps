# TrønderLeikan - produktspesifikasjon

Status: **BASELINE** - vedtatt etter grilling 2026-07-02 til 2026-07-04.
Avvik fra denne spec-en dokumenteres som ADR i `docs/adr/`, ikke ved å omskrive historikk her.
Arbeidsplan: se `PLAN.md`.

## 1. Hva dette er

Multi-tenant event- og turneringsplattform (arbeidsnavn TrønderLeikan / inmeta-games).
Et miljø (bedrift, kontor, vennegjeng) arrangerer turneringer med konkurranser av ulik form: quiz, rebus, dart, bordtennis, sim-racing.
Plattformen holder roster, påmelding, resultater, brackets, tidtagning, live-scoreboards og etter hvert ELO-rating.

**Arkitektur-ambisjonen er eksplisitt "nesten unødvendig" microservices.**
Målet er læring: distribuert systemdesign, event-drevet arkitektur, multi-tenancy og IdP-integrasjon gjort skikkelig.
Kvalitet, robusthet og langsiktig vedlikeholdbarhet prioriteres over utviklingshastighet.

## 2. Domenemodell

```
Tenant (= miljø/organisasjon, 1:1 med Zitadel Organization)
  └── Tournament (årlig ramme, f.eks. "TrønderLeikan 2026")
        └── Game (konkurranse: quiz, dart, sim-racing, ...)
              ├── kategori (fritt definerbar per tenant: darts, sim-racing, quiz, ...)
              ├── Participant (deltaker i ETT game: enten Person eller Team)
              │     └── Team -> TeamMember -> Person
              ├── Resultat (én av tre former, se §3)
              └── arrangører (Person-refs), tilskuere (Person-refs)
Person (tenant-eid roster-entitet, IKKE en brukerkonto)
```

Nøkkelregler:

- Et arrangement kan vare en kveld, en dag eller en uke (sim-racing: simmen står på kontoret man-fre).
- Et Game har N arrangører, N deltakere, N tilskuere.
- `Participant` er egen entitet fra dag 1 (aldri Person direkte i resultat-tabeller), slik at lag og individ behandles likt.
- Ties er lov i plasseringslister.

## 3. Konkurranseformer og resultat-innmating

| Form | Eksempel | Resultat | Innmating v1 | Trust-modell |
|---|---|---|---|---|
| Plasseringsliste | quiz, rebus | 1./2./3. plass (+flere ved behov) | Kun arrangør puncher | Full tillit |
| Time-trial | sim-racing | beste lap-time, flere forsøk lov | To moduser per Game (arrangør velger): `organizer-only` eller `open-entry` | Tillit + revisjon |
| Bracket | dart, bordtennis | match-vinnere, sluttplassering | Arrangør registrerer match-vinner, motoren avanserer | Full tillit |

`open-entry` (kiosk-modus, arvet fra gammel sim-racing-app):

- Åpent skjema uten auth: velg Person fra roster, punch tid `MM:SS.mmm`.
- Hver innsending auditeres (valgt person, tidspunkt, økt/kilde).
- Arrangør kan slette/korrigere alt.
- `requires_approval`-flagg per Game (default av): på = innsendinger vises som "innmeldt, ikke verifisert" til arrangør bekrefter.
- `submission.evidence_url` (nullable) reservert for bevis-opplasting via object storage senere. Ikke i v1.
- Autentisert self-service (innlogget player leverer egen tid) er v2, bygges oppå samme endepunkt (innsending har kilde + status fra dag 1).

Lap-times lagres som heltall millisekunder. Input-format `MM:SS.mmm`.

## 4. Identitet: Person vs Zitadel-konto (hard splitt)

- **`Person`**: domeneentitet eid av tenant. Navn, ev. avdeling/avatar. Roster-en (erstatter tidligere Sanity-tabell). Opprettes av arrangør uten at personen involveres.
- **Zitadel-konto**: kun autentisering og roller (hvem får gjøre hva i UI/API).
- ALT av deltakelse, plasseringer, lap-times, lag-medlemskap og ELO peker på Person. Aldri på konto.
- Kobling: `person.account_id` (nullable, unik per tenant). V1: roster-admin kobler manuelt. "Claim din profil"-flyt er v2.
- Konsekvens: en Person uten konto kan vinne dart-turneringen. Konto kreves kun for arrangør-roller og oppover, samt spillere som vil claime profilen sin.

## 5. Multi-tenancy og Zitadel

- **Tenant = organisasjon/miljø.** Eier Tournaments, Games, Person-roster, ELO. Ingenting deles på tvers (unntatt admin-planet).
- **1:1-mapping mot Zitadel Organization.**
- **Zitadel-modell:** ett Project `tronderleikan` eid av vår plattform-org. Project-grant til hver tenant-org. Roller defineres én gang på prosjektet, hver tenant-org tildeler til sine brukere.
- **Tenant-provisjonering** (fra admin-planet, manuelt i v1): opprett Zitadel-org + project grant + første org-admin via Zitadel API. Lagre `tenant.zitadel_org_id`.
- Self-service tenant-signup: ikke i scope.
- **Zitadel hosting:** self-host i clusteret (offisiell helm-chart, eget namespace `zitadel`, database i CNPG). Hostname `auth.newb.no`. 1 replica først, HA som eget drift-prosjekt senere.
- **Issuer-regel (ufravikelig):** issuer/domenet hardkodes ALDRI - hver app/tjeneste leser det fra én env-var. Fremtidig migrering til `auth.klyngo.no` = legg til trusted domain, flytt klienter én og én, oppdater config. Egen arbeidspakke når domenet finnes.

## 6. RBAC

4 project-roles + anonym. "Gjest" er ikke en rolle - det er fravær av token.

| Rolle | Hvem | Kan |
|---|---|---|
| *(anonym)* | Ingen konto | Se offentlige scoreboards/brackets for tenanten (les-only). Per-tenant flagg `public_visibility` (default på) skrur dette av/på |
| `player` | Claimet Person | Se alt i tenanten, melde seg på åpne Games, levere egne resultater der arrangør har åpnet for det (v2) |
| `organizer` | Arrangør | Opprette/administrere Games, roster, lag, brackets, punche resultater/tider, godkjenne innsendinger |
| `tenant_admin` | Org-eier | Alt organizer kan + Tournaments, tenant-innstillinger, invitere brukere, dele ut roller (via Zitadel org-admin) |
| `platform_admin` | Plattform-eier | Admin-planet: alle tenants, provisjonering, moderering på tvers. Kun tildelbar i plattform-orgen |

Moderator er bevisst kuttet fra v1 (ingen brukergenerert innhold å moderere ennå). Kan legges til som ny project-role senere uten migrering.

## 7. Tjenester (7 stk)

| Tjeneste | Eier | Fase |
|---|---|---|
| **platform** | Tenants, Zitadel-provisjonering, admin-plane-API | 1 |
| **roster** | Persons, account-kobling (claim), avdeling/avatar | 1 |
| **competition** | Tournaments, Games, påmelding/deltakelse, lag, plasseringsresultater | 1 |
| **bracket** | Bracket-motor: seeding (manuell/tilfeldig/ELO), single/double elimination, lower bracket, match-progresjon | 2 |
| **timing** | Time-trial-økter, lap-times, forsøksregler, open-entry + godkjenning | 2 |
| **live** | SSE/websocket-fanout: scoreboards, bracket-oppdateringer, "ny beste tid". Stateless, ren event-konsument | 2 |
| **rating** | ELO per Person per tenant per kategori. Ren event-konsument, replay-bar | 3 |

Prinsipper:

- bracket og timing er spesialitetene med ekte isolerbar domenelogikk: ren funksjon (state + resultat inn, ny state ut), klare kontrakter, perfekte agent-angrepsflater.
- rating og live er rene konsumenter: ingen synkrone kall inn, alt via broker.
- Synkron tjeneste-til-tjeneste holdes på minimum: refs valideres ved skriving, ellers events.
- **Ingen egen API-gateway.** Traefik ruter, TanStack Start-serveren er BFF.
- **competition publiserer domene-events fra dag 1** selv om ingen lytter ennå. Rating/live blir ren addisjon senere.

## 8. Datalag

- **Én CNPG Postgres-cluster, én database per tjeneste** (`platform`, `roster`, `competition`, ...). Hver tjeneste har egen DB-bruker som kun ser sin database.
- **Ingen cross-service joins, ingen FK-er på tvers.** competition lagrer `person_id` som verdi. Integritet håndheves ved skriving (API-kall) + events (person slettet -> konsumenter rydder/anonymiserer).
- **Tenant-isolasjon:** `tenant_id UUID NOT NULL` på hver rad + Postgres row-level security som sikkerhetsnett. Tjenesten setter `SET LOCAL app.tenant_id = ...` per request/tx, RLS-policy filtrerer på `current_setting('app.tenant_id')`. App-laget filtrerer også. Unntak: platform sin tenant-registry er per definisjon ikke tenant-scopet.
- **Migrasjoner:** goose per tjeneste, kjøres ved deploy.
- **ID-er:** UUIDv7 overalt, generert i app.
- **Redis:** én instans, kun cache/ephemeral, aldri sannhetskilde. Key-prefix `{service}:{tenant}:...`.

## 9. Events: NATS JetStream + transactional outbox

- **Broker: NATS JetStream**, 3-replika raft-cluster i k8s (offisiell helm-chart). Valgt over RabbitMQ pga. replay (durable log), footprint og Go-støtte. Kafka avvist som feil skala.
- **Replay er en feature:** rating kan re-konsumere hele resultat-historikken fra offset 0 (ELO-rebuild gratis).
- Core NATS pub/sub (ikke-durable) brukes for live-fanout der tap er ok.
- **Transactional outbox i hver publiserende tjeneste:** domene-event skrives til `outbox`-tabell i SAMME tx som domene-endringen. Publisher-goroutine leser outbox -> publiserer -> markerer sendt. Konsumenter er idempotente (dedup på event-id).
- **Envelope** (delt Go-lib, CloudEvents-inspirert JSON): `event_id` (UUIDv7), `tenant_id`, `type`, `occurred_at`, `data`.
- **Subjects:** `tl.<service>.<entity>.<event>`, tenant i header/payload.

Event-katalog (baseline, utvides i implementasjon):

```
tl.platform.tenant.provisioned
tl.roster.person.created | updated | deleted | account_claimed
tl.competition.tournament.created
tl.competition.game.created | updated | finalized
tl.competition.participation.registered
tl.competition.result.recorded
tl.bracket.match.completed
tl.bracket.bracket.completed
tl.timing.laptime.submitted | approved | rejected | deleted
tl.timing.session.completed
tl.rating.rating.updated
```

## 10. Frontends og ruting

To TanStack Start-apper:

| App | Innhold | Auth |
|---|---|---|
| **web** | Alt tenant-vendt: offentlige scoreboards/brackets, spillerprofiler, påmelding, arrangør-verktøy (rollegatet). Storskjerm = route `/live/<game>` i kiosk-modus (SSE, Top Gear-tavle-stil) | Zitadel OIDC PKCE, anonym tilgang for offentlige sider |
| **admin** | Admin-planet: tenants, provisjonering, tverr-tenant innsyn. Bygges med `basePath: /admin` | Zitadel, kun `platform_admin`, egen Zitadel-app |

Single-host-ruting via Traefik (ingen Caddy - Traefik ER proxyen):

```
Host(`leikan.newb.no`) && PathPrefix(`/api`)    -> tjenestene (/api/<service>/*)
Host(`leikan.newb.no`) && PathPrefix(`/admin`)  -> admin-appen
Host(`leikan.newb.no`)                          -> web (catch-all)
```

Akseptert tradeoff: web og admin deler origin (cookie-scope). Akseptabelt fordi alt er tailnet-only og admin-makt gates av Zitadel-roller, ikke av cookien.
Tenant-kontekst i URL-er: tenant-slug i web-routes (`/t/<slug>/...`), tenant-UUID i API-paths. Anonym-tilgang krever at tenant kan utledes uten token.
URL-paths på engelsk.

## 11. Repo, CI og deploy

```
apps/tronderleikan/
  platform/ roster/ competition/     # fase 1, Go, egen go.mod hver
  bracket/ timing/ live/ rating/     # fase 2-3
  web/ admin/                        # TanStack Start
  pkg/                               # delt Go-lib: jwt/tenant-ctx/outbox/event-envelope/otel. Egen go.mod
  docs/                              # SPEC.md (denne), PLAN.md, adr/
```

- Dokumentert unntak fra "én app per mappe / ingen delte pakker": produkt-suite-mønster. `go.work` lokalt, modulsti `github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg`.
- CI: én workflow per tjeneste (`tronderleikan-<service>.yml` + `-pr.yml`), path-filter `apps/tronderleikan/<service>/**` + `apps/tronderleikan/pkg/**` (pkg-endring bygger alle). Samme build-once-promote som resten av repoet. Ett image per tjeneste, distroless, health som binær-flagg.
- Deploy: kongebra-gitops `apps/tronderleikan-<service>/`, alle deployables i samme namespace-par `tronderleikan-dev` / `tronderleikan-prod`.
- Observability: standard for repoet - OTLP til cluster-stacken (alloy), RED-dashboards per tjeneste.

## 12. Lokal utvikling

- **Aspire AppHost (TypeScript)** for hele produktet: Postgres (én DB per tjeneste), Redis, NATS (JetStream), Zitadel-container med idempotent seed (plattform-org, project, roller, test-tenant, testbrukere), Go-tjenestene, begge frontends, otel-lgtm.
- Dette er samtidig Aspire-spiken fra CLAUDE.md - TrønderLeikan er stresstesten.
- **Fallback-krav:** hver tjeneste kan kjøres alene med `docker compose up` (egen DB + NATS) + `go run`. Agenter på én arbeidspakke trenger ikke hele flåten - kontraktene i denne spec-en er grensesnittet.
- Zitadel-seed er egen arbeidspakke (scriptet, idempotent, via Zitadel API).

## 13. Rating (ELO) - fase 3

- Egen tjeneste, ren event-konsument, bygger state via replay.
- **Per tenant + per kategori** (dart-ELO og sim-racing-ELO er adskilt). Kategori er felt på Game, fritt definerbar per tenant.
- **Kun individer.** Lag-rating er parkert (roterende lagsammensetning er et reelt forskningsfelt). Lag-resultater lagres, men rates ikke.
- **Algoritme: pairwise Elo over plasseringer.** Plasseringsliste [A, B, C] dekomponeres til duellene A>B, A>C, B>C. Bracket-matcher er naturlige dueller. Time-trial rangerer via tidene. Én mekanisme for alle tre formene. Glicko-2/TrueSkill bevisst avvist (Elo er forklarbart på pub).
- Seeding-integrasjon: bracket spør rating om kategori-rating når seeding-modus er `elo`. Finnes ikke rating (ennå), fall tilbake til tilfeldig.

## 14. Bevisst utsatt (ikke glemt)

| Hva | Til |
|---|---|
| Claim-din-profil-flyt | v2 (fase 3) |
| Autentisert self-service-innlevering | v2, samme endepunkt som open-entry |
| Bevis-opplasting (object storage) | senere, `evidence_url` reservert |
| Lag-ELO | parkert |
| Moderator-rolle | når brukergenerert innhold finnes |
| Self-service tenant-signup | ikke i scope |
| Zitadel HA | eget drift-prosjekt |
| `auth.klyngo.no`-migrering | når domenet finnes, egen arbeidspakke |
| Sammenlagt-poeng på tvers av Games ("kongepokal") | v2+ |
