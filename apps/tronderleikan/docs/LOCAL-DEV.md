# TrønderLeikan - lokal utvikling

To måter å kjøre lokalt. Velg etter hva du jobber på.

## Hele produktet: Aspire (arbeidspakke 0.3)

`apps/tronderleikan/apphost/` er en Aspire AppHost i TypeScript som starter alt: Postgres (én database per tjeneste), Redis, NATS (JetStream), Zitadel, `grafana/otel-lgtm` og Go-tjenestene.

```bash
cd apps/tronderleikan/apphost
npm install        # første gang
aspire run         # starter alt + Aspire-dashboard
```

- Tjenestene sender OTLP til den lokale `otel-lgtm`-containeren (Grafana på dashboardets grafana-endpoint), ikke til Aspire-dashboardet - bevisst, for prod-paritet med cluster-stacken.
- Postgres-credentials er faste dev-verdier (`postgres`/`localdev`), ingen data-volumes: lokal state er flyktig, seed (pakke 0.4) er idempotent.
- Zitadel: issuer `http://localhost:8300`, masterkey er en fast 32-tegns dev-verdi. Seed-scriptet (pakke 0.4) hektes på via den kommenterte `zitadel-seed`-blokken i `apphost.mts` når det finnes.
- Go-tjenester deklareres kun hvis `<service>/go.mod` finnes (se `existsSync`-gaten) - så AppHost-en starter uten feil før tjenestene er skrevet.

## Én tjeneste: docker compose + go run (fallback)

Jobber du på én tjeneste trenger du ikke hele flåten. Hver tjeneste har en `compose.yaml` med kun sine avhengigheter:

```bash
cd apps/tronderleikan/platform
docker compose up -d          # postgres + nats for denne tjenesten
export DATABASE_URL=postgres://platform:platform@localhost:5432/platform?sslmode=disable
export NATS_URL=nats://localhost:4222
export AUTH_ISSUER=http://localhost:8300      # peker på Aspire-Zitadel hvis den kjører, ellers mock
go run .
```

Kontraktene (API + events) står i `SPEC.md` - test mot din egen boks, ikke hele produktet.

## Aspire-spike: funn (CLAUDE.md ber om denne)

Aspire v13 er `aspire` CLI 13.4.5, AppHost i TypeScript.

- **Fungerer:** `aspire:lint` (eslint) og `aspire:build` (tsc typecheck) rent på `apphost.mts`. Postgres/Redis/NATS/container-primitiver (`addPostgres`/`addRedis`/`addNats().withJetStream()`/`addContainer`) og `addGoApp` finnes i TS-API-et.
- **Ikke runtime-verifisert ennå:** full `aspire run` (container-boot av Zitadel/otel-lgtm + Go-tjeneste) er ikke kjørt til ende - platform-tjenesten (pakke 1.1) må finnes først for at Go-integrasjonen skal testes reelt. Verifiser ved første tjeneste.
- **Ponytail:** `apphost.mts` er kontrakten for hvordan tjenester wires (addGoApp + DATABASE_URL/NATS_URL/AUTH_ISSUER/OTLP). Utvid samme mønster per ny tjeneste; ikke abstraher før det gjør vondt.
