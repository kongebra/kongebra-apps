# TrønderLeikan - langtlevende arbeidsplan

Kontrakt: `SPEC.md` er baseline for alle arbeidspakker.
En agent som tar en pakke leser SPEC-seksjonene pakken refererer, ikke chatlogger.
Issues opprettes fase for fase (denne fila er sannheten, GitHub-issues er arbeidskøen).
Statusverdier: `TODO` / `IN PROGRESS` / `DONE` / `BLOCKED`.

## Fase 0 - fundament

Exit-kriterium: tom-men-ekte skjelett - pkg-lib bygger, én dummy-tjeneste går CI-løypa til dev, Aspire starter avhengighetene, Zitadel svarer på `auth.newb.no`.

| # | Pakke | Innhold | Spec | Avhenger av | Status |
|---|---|---|---|---|---|
| 0.1 | Mappestruktur + pkg-lib | `apps/tronderleikan/`-tre, `go.work`, `pkg/` med event-envelope, outbox-writer/publisher, JWT-validering (JWKS fra env-issuer), tenant-context (inkl. `SET LOCAL app.tenant_id`), otel-oppsett. Enhetstester | §5, §8, §9, §11 | - | DONE (PR #7) |
| 0.2 | CI-workflows | `tronderleikan-<service>.yml` + `-pr.yml`-mal (path-filter inkl. `pkg/**`), instansiert for platform. Gjenbruker `_build-deploy.yml` | §11 | 0.1 | DONE (PR #8) |
| 0.3 | Aspire AppHost + compose-fallback | TS AppHost: Postgres (DB per tjeneste), Redis, NATS JetStream, Zitadel, otel-lgtm, tjenestene. Per-tjeneste compose-fil. | §12 | 0.1 | DONE (PR #9) - full `aspire run` runtime-boot gjenstår til 1.1 |
| 0.4 | Zitadel-seed | Idempotent seed-script (API): plattform-org, project `tronderleikan`, 4 roller, test-tenant-org m/grant, testbrukere. Kjøres lokalt (Aspire) og mot cluster | §5, §6, §12 | 0.3 | DONE (PR #12) - verifisert mot ekte Zitadel v4.15.3, grant-rolle-konvergens bevist |
| 0.5 | Infra-handoff (kongebra-gitops) | Namespace-par, NATS JetStream (3 replica), Zitadel helm + CNPG-db + `auth.newb.no`, CNPG-databaser + brukere per tjeneste, Traefik-ruter for `leikan.newb.no` | §5, §8, §9, §10 | - | PR åpen (gitops #10) - venter out-of-band secrets + DNS før merge/sync |

## Fase 1 - kjernen

Exit-kriterium: ekte quiz kjørt ende-til-ende på `leikan.newb.no` - tenant provisjonert, roster fylt, Game opprettet, plasseringer punchet, offentlig scoreboard synlig anonymt.

| # | Pakke | Innhold | Spec | Avhenger av | Status |
|---|---|---|---|---|---|
| 1.1 | platform-tjenesten | Tenant-registry (CRUD, `public_visibility`), Zitadel-provisjonering (org + grant + første admin), slug-oppslag, `tenant.provisioned`-event | §5, §7, §8, §9 | 0.x | TODO |
| 1.2 | roster-tjenesten | Person CRUD, manuell account-kobling, person-events | §4, §7, §8 | 0.x | PR åpen (feat/tronderleikan-roster) |
| 1.3 | competition-tjenesten | Tournament/Game CRUD (kategori, `requires_approval`), Participant (person/team), lag, plasseringsresultater m/ties, outbox-events fra dag 1 | §2, §3, §7, §9 | 1.1, 1.2 | TODO |
| 1.4 | web (minimal) | TanStack Start: OIDC PKCE, tenant-slug-routes, offentlig scoreboard/game-liste (anonym), arrangør-flater for roster + game + plassering-punching | §6, §10 | 1.1-1.3 | TODO |
| 1.5 | admin (minimal) | TanStack Start `basePath: /admin`: tenant-liste, provisjonering, kun `platform_admin` | §6, §10 | 1.1 | TODO |
| 1.6 | E2E-verifisering fase 1 | Quiz-scenarioet kjørt i dev + prod, dokumentert. Playwright-røyk på web | §3 | 1.1-1.5 | TODO |

## Fase 2 - spesialitetene

Exit-kriterium: dart-turnering (bracket, inkl. lower bracket) og sim-racing-uke (kiosk open-entry + live-tavle) kjørt live.

| # | Pakke | Innhold | Spec | Avhenger av | Status |
|---|---|---|---|---|---|
| 2.1 | bracket-tjenesten | Motor som ren funksjon: seeding (manuell/tilfeldig/elo m/fallback), single/double elim, lower bracket, match-progresjon, events. Grundig enhetstestet | §3, §7, §13 | fase 1 | TODO |
| 2.2 | timing-tjenesten | Time-trial-økter, lap-times (ms), moduser `organizer-only`/`open-entry`, audit, `requires_approval`-flyt, events | §3, §7 | fase 1 | TODO |
| 2.3 | live-tjenesten | SSE-fanout fra core NATS: scoreboard-, bracket- og laptime-oppdateringer | §7, §9 | 2.1, 2.2 | TODO |
| 2.4 | web: bracket + kiosk + storskjerm | Bracket-visning, kiosk-skjema (open-entry), `/live/<game>` storskjerm-route (SSE) | §3, §10 | 2.1-2.3 | TODO |
| 2.5 | E2E-verifisering fase 2 | Dart + sim-racing-scenarioene kjørt live, dokumentert | §3 | 2.1-2.4 | TODO |

## Fase 3 - krydder

| # | Pakke | Innhold | Spec | Avhenger av | Status |
|---|---|---|---|---|---|
| 3.1 | rating-tjenesten | Pairwise Elo per tenant/kategori, replay fra JetStream offset 0, `rating.updated`-events, seed-API for bracket | §13 | fase 2 | TODO |
| 3.2 | Claim-din-profil | Player claimer Person ("dette er meg"), godkjenning av roster-admin | §4 | fase 1 | TODO |
| 3.3 | Autentisert self-service | Innlogget player leverer egen tid, samme endepunkt som open-entry | §3 | 2.2, 3.2 | TODO |
| 3.4 | web: profiler + rating | Spillerprofil m/historikk + ELO-visning per kategori | §13 | 3.1 | TODO |
| 3.5 | auth.klyngo.no-migrering | Trusted domain, klient-flytt, config-bytte | §5 | domenet finnes | BLOCKED |

## Stående regler for agenter

- Les SPEC-seksjonene pakken refererer FØR koding. Uklarheter -> spørsmål i issuet, ikke gjetting.
- Avvik fra SPEC krever ADR i `docs/adr/` (kort: kontekst, beslutning, konsekvens).
- Hver tjeneste: egen go.mod, distroless-image, health som binær-flagg, OTLP, goose-migrasjoner, RLS fra første tabell.
- Events er kontrakt: nye events inn i §9-katalogen (via ADR hvis de endrer semantikk).
- Definition of done per pakke: tester grønne, PR-sjekk grønn, deployet til dev, manuelt verifisert, dokumentert i pakkens issue.
