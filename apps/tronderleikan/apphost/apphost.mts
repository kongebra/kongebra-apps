// TrønderLeikan - lokal utvikling via Aspire (SPEC §12, arbeidspakke 0.3).
// Hele produktet orkestreres her: Postgres (én DB per tjeneste), Redis,
// NATS (JetStream), Zitadel, otel-lgtm og Go-tjenestene.
// Fallback per tjeneste: <service>/compose.yaml + `go run` (se docs/LOCAL-DEV.md).

import { existsSync } from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createBuilder } from './.aspire/modules/aspire.mjs';

const apphostDir = path.dirname(fileURLToPath(import.meta.url));
const serviceDir = (name: string) => path.join(apphostDir, '..', name);

const builder = await createBuilder();

// ---------------------------------------------------------------------------
// Postgres: én server, én database per tjeneste (SPEC §8).
// Faste dev-credentials (localhost-only): lett å inspisere med psql, og
// deterministisk på tvers av restarts. Ingen data-volumes: lokal state er
// bevisst flyktig, seed-script (pakke 0.4) er idempotent og kjøres på nytt.
// ---------------------------------------------------------------------------
const pgUser = builder.addParameter('pg-username', { value: 'postgres' });
const pgPassword = builder.addParameter('pg-password', { value: 'localdev', secret: true });

const postgres = builder.addPostgres('postgres', { userName: pgUser, password: pgPassword });

const platformDb = postgres.addDatabase('platform');
// Fase 1-tjenestene har hver sin database fra dag 1:
const rosterDb = postgres.addDatabase('roster');
const competitionDb = postgres.addDatabase('competition');
// bracket/timing/rating (fase 2-3) legges til her når tjenestene finnes.

// ---------------------------------------------------------------------------
// Redis: én instans, kun cache/ephemeral (SPEC §8).
// ---------------------------------------------------------------------------
const redis = builder.addRedis('redis');
void redis; // wires inn i tjenestene når første konsument finnes

// ---------------------------------------------------------------------------
// NATS med JetStream (SPEC §9).
// ---------------------------------------------------------------------------
const nats = builder.addNats('nats').withJetStream();

// ---------------------------------------------------------------------------
// otel-lgtm: samme stack som i clusteret -> Grafana/Tempo/Prometheus-paritet
// lokalt. Tjenestene sender OTLP hit, ikke til Aspire-dashboardet
// (withOtlpExporter), nettopp for å beholde prod-paritet.
// ---------------------------------------------------------------------------
const otelLgtm = builder
  .addContainer('otel-lgtm', 'grafana/otel-lgtm:0.28.0')
  .withHttpEndpoint({ targetPort: 3000, name: 'grafana' })
  .withEndpoint({ targetPort: 4318, name: 'otlp-http', scheme: 'http' })
  .withEndpoint({ targetPort: 4317, name: 'otlp-grpc', scheme: 'http' });
const otlpEndpoint = otelLgtm.getEndpoint('otlp-http');

// ---------------------------------------------------------------------------
// Zitadel (SPEC §5): offisielt image, Postgres-backend i egen database.
// `start-from-init` oppretter database `zitadel` + egen db-bruker selv via
// admin-credentials. Host-port er pinnet (8300) fordi Zitadel må kjenne sin
// eksterne adresse ved oppstart (issuer). Issuer: http://localhost:8300
// Masterkey må være NØYAKTIG 32 tegn. Fast dev-verdi - lokal, aldri prod.
// ---------------------------------------------------------------------------
const zitadelMasterkey = builder.addParameter('zitadel-masterkey', {
  value: 'insecure-local-dev-masterkey-32b', // 32 tegn
  secret: true,
});

// Seed-scriptet (pakke 0.4) trenger API-credentials. Zitadel FirstInstance
// oppretter en machine-user (IAM_OWNER) og skriver dens PAT til en fil ved init.
// Vi bind-mounter en host-mappe inn i containeren slik at seed-executablen (som
// kjører på host, ikke i container) kan lese PAT-en. Mappen er flyktig (som resten
// av lokal state) og gitignore-t. Ingen data-volume -> FirstInstance kjører på
// nytt hver oppstart, og den idempotente seeden bygger tilstanden opp igjen.
const zitadelPatDir = path.join(apphostDir, '.zitadel');
const zitadelPatFile = path.join(zitadelPatDir, 'pat.txt');
// PAT-utløp: now+30d. Kort levetid selv om det bare er en lokal dev-token -
// state er flyktig (ny PAT hver aspire run), og 30d gir buffer for langlevde
// dev-sesjoner uten å etterlate en de-facto evig token.
const zitadelPatExpiry = new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString();

const zitadel = builder
  .addContainer('zitadel', 'ghcr.io/zitadel/zitadel:v4.15.3')
  .withArgs(['start-from-init', '--masterkeyFromEnv', '--tlsMode', 'disabled'])
  .withEnvironment('ZITADEL_MASTERKEY', zitadelMasterkey)
  // FirstInstance: machine-user (IAM_OWNER) + PAT skrevet til bind-mountet fil.
  .withEnvironment('ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_USERNAME', 'zitadel-admin-sa')
  .withEnvironment('ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_NAME', 'Seed Admin')
  .withEnvironment('ZITADEL_FIRSTINSTANCE_ORG_MACHINE_PAT_EXPIRATIONDATE', zitadelPatExpiry)
  .withEnvironment('ZITADEL_FIRSTINSTANCE_PATPATH', '/machinekey/pat.txt')
  .withBindMount(zitadelPatDir, '/machinekey')
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_HOST', postgres.host())
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_PORT', postgres.port())
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_DATABASE', 'zitadel')
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_USER_USERNAME', 'zitadel')
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_USER_PASSWORD', 'zitadel-localdev')
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE', 'disable')
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_ADMIN_USERNAME', pgUser)
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD', pgPassword)
  .withEnvironment('ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE', 'disable')
  .withEnvironment('ZITADEL_EXTERNALDOMAIN', 'localhost')
  .withEnvironment('ZITADEL_EXTERNALPORT', '8300')
  .withEnvironment('ZITADEL_EXTERNALSECURE', 'false')
  // v4 krever login-v2-container by default; lokalt bruker vi innebygd login.
  .withEnvironment('ZITADEL_DEFAULTINSTANCE_FEATURES_LOGINV2_REQUIRED', 'false')
  .withHttpEndpoint({ port: 8300, targetPort: 8080, isProxied: false })
  .withHttpHealthCheck({ path: '/debug/healthz' })
  .waitFor(postgres);

// ---------------------------------------------------------------------------
// Zitadel-seed (arbeidspakke 0.4): idempotent provisjonering av plattform-org,
// project `tronderleikan`, de 4 rollene, test-tenant-org m/grant og testbrukere
// (SPEC §5, §6, §12). Issuer/domenet leses fra ZITADEL_API_URL (aldri hardkodet).
// Credentials: PAT-fila FirstInstance skrev (bind-mount over). Testbruker-passord
// er en lokal dev-verdi (parameter, ikke i Go-koden).
// ---------------------------------------------------------------------------
const seedTestPassword = builder.addParameter('seed-test-password', {
  value: 'Password1!', // lokal dev, oppfyller Zitadels default kompleksitet
  secret: true,
});

const zitadelSeed = builder
  .addExecutable('zitadel-seed', 'go', serviceDir('zitadel-seed'), ['run', '.'])
  .withEnvironment('ZITADEL_API_URL', zitadel.getEndpoint('http'))
  .withEnvironment('ZITADEL_PAT_FILE', zitadelPatFile)
  .withEnvironment('SEED_TEST_PASSWORD', seedTestPassword)
  .waitFor(zitadel);

// ---------------------------------------------------------------------------
// Go-tjenestene. Kun platform er planlagt i fase 1 (pakke 1.1); mappen finnes
// ikke på main ennå, så deklarasjonen er gated på at go.mod eksisterer.
// Mønsteret under ER kontrakten for hvordan hver tjeneste wires inn.
// ---------------------------------------------------------------------------
type GoAppEndpoint = ReturnType<ReturnType<typeof builder.addGoApp>['getEndpoint']>;
let platformEndpoint: GoAppEndpoint | undefined;
if (existsSync(path.join(serviceDir('platform'), 'go.mod'))) {
  const platformApp = builder
    .addGoApp('platform', serviceDir('platform'))
    .withHttpEndpoint({ env: 'PORT' })
    .withEnvironment('DATABASE_URL', await platformDb.uriExpression())
    .withEnvironment('NATS_URL', await nats.uriExpression())
    .withEnvironment('AUTH_ISSUER', zitadel.getEndpoint('http'))
    // Zitadel-provisjonering: samme instans + PAT-fil som seeden. Platform
    // utleder plattform-org/project (og JWT-audience) fra den seedede tilstanden,
    // derfor waitFor(zitadelSeed) - project/roller må finnes ved oppstart.
    .withEnvironment('ZITADEL_API_URL', zitadel.getEndpoint('http'))
    .withEnvironment('ZITADEL_PAT_FILE', zitadelPatFile)
    .withEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', otlpEndpoint)
    .withEnvironment('OTEL_SERVICE_NAME', 'platform')
    .waitFor(postgres)
    .waitFor(nats)
    .waitFor(zitadel)
    .waitFor(zitadelSeed);
  platformEndpoint = platformApp.getEndpoint('http');
  await platformApp;
}
// roster (arbeidspakke 1.2, SPEC §4/§7): Person-roster + account-kobling +
// person-events. Egen DB (rosterDb). Samme wiring-mønster som platform.
// ponytail: AUTH_AUDIENCE settes til project-navnet som placeholder. Ekte
// token-audience er Zitadel-project-/client-ID-en som seeden genererer ved
// kjøretid (ikke statisk kjent). Oppgraderingssti: la zitadel-seed skrive ut
// project-ID-en (eller registrer en API-app) og mat den inn her - egen
// auth-integrasjonspakke sammen med platform 1.1.
let rosterEndpoint: ReturnType<ReturnType<typeof builder.addGoApp>['getEndpoint']> | undefined;
if (existsSync(path.join(serviceDir('roster'), 'go.mod'))) {
  const rosterApp = builder
    .addGoApp('roster', serviceDir('roster'))
    .withHttpEndpoint({ env: 'PORT' })
    .withEnvironment('DATABASE_URL', await rosterDb.uriExpression())
    .withEnvironment('NATS_URL', await nats.uriExpression())
    .withEnvironment('AUTH_ISSUER', zitadel.getEndpoint('http'))
    .withEnvironment('AUTH_AUDIENCE', 'tronderleikan')
    .withEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', otlpEndpoint)
    .withEnvironment('OTEL_SERVICE_NAME', 'roster')
    .waitFor(postgres)
    .waitFor(nats)
    .waitFor(zitadel);
  rosterEndpoint = rosterApp.getEndpoint('http');
  await rosterApp;
}
// competition (arbeidspakke 1.3, SPEC §2/§3/§7): Tournaments/Games/påmelding/
// lag/plasseringsresultater + outbox-events fra dag 1. Egen DB (competitionDb).
// Validerer person-refs synkront mot roster ved skriving (SPEC §7), derfor
// ROSTER_URL + waitFor(roster). AUTH_AUDIENCE-placeholderen deler samme
// oppgraderingssti som roster (ekte project-ID fra seeden - egen auth-pakke).
let competitionEndpoint: GoAppEndpoint | undefined;
if (rosterEndpoint && existsSync(path.join(serviceDir('competition'), 'go.mod'))) {
  const competitionApp = builder
    .addGoApp('competition', serviceDir('competition'))
    .withHttpEndpoint({ env: 'PORT' })
    .withEnvironment('DATABASE_URL', await competitionDb.uriExpression())
    .withEnvironment('NATS_URL', await nats.uriExpression())
    .withEnvironment('AUTH_ISSUER', zitadel.getEndpoint('http'))
    .withEnvironment('AUTH_AUDIENCE', 'tronderleikan')
    .withEnvironment('ROSTER_URL', rosterEndpoint)
    .withEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', otlpEndpoint)
    .withEnvironment('OTEL_SERVICE_NAME', 'competition')
    .waitFor(postgres)
    .waitFor(nats)
    .waitFor(zitadel);
  competitionEndpoint = competitionApp.getEndpoint('http');
  await competitionApp;
}
// bracket/timing/live/rating (fase 2-3) følger samme mønster:
// addGoApp + DATABASE_URL/NATS_URL/AUTH_ISSUER/OTLP.

// ---------------------------------------------------------------------------
// web (arbeidspakke 1.4, SPEC §6/§10): TanStack Start-frontenden. BFF-en kaller
// Go-tjenestene på deres interne endepunkter (PLATFORM_URL/ROSTER_URL/
// COMPETITION_URL), og gjør OIDC (Authorization Code + PKCE) mot Zitadel
// (AUTH_ISSUER, aldri hardkodet). Kjøres som npm-executable lokalt; PORT
// injiseres av Aspire (vite leser process.env.PORT).
// ponytail: node-deps må installeres (npm ci) før `aspire run`, og Aspire har
// ingen Node-hosting-integrasjon i aspire.config.json ennå - full `aspire run`-
// runtime for web valideres i 1.6 (samme deferral som platform-runtime i 0.3).
// Prod kjører uansett via Docker/nitro (.output), ikke via Aspire.
// AUTH_CLIENT_ID/SESSION_SECRET er dev-parametere; ekte web-OIDC-klient
// registreres av zitadel-seed (samme oppgraderingssti som AUTH_AUDIENCE over).
// ---------------------------------------------------------------------------
const webSessionSecret = builder.addParameter('web-session-secret', {
  value: 'insecure-local-dev-session-secret-32b', // >= 32 tegn, kun lokalt
  secret: true,
});
if (
  platformEndpoint &&
  rosterEndpoint &&
  competitionEndpoint &&
  existsSync(path.join(serviceDir('web'), 'package.json'))
) {
  const webApp = builder
    .addExecutable('web', 'npm', serviceDir('web'), ['run', 'dev'])
    .withHttpEndpoint({ env: 'PORT' })
    .withEnvironment('PLATFORM_URL', platformEndpoint)
    .withEnvironment('ROSTER_URL', rosterEndpoint)
    .withEnvironment('COMPETITION_URL', competitionEndpoint)
    .withEnvironment('AUTH_ISSUER', zitadel.getEndpoint('http'))
    .withEnvironment('AUTH_CLIENT_ID', 'tronderleikan-web')
    .withEnvironment('SESSION_SECRET', webSessionSecret)
    .withEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', otlpEndpoint)
    .withEnvironment('OTEL_SERVICE_NAME', 'web')
    .waitFor(zitadel)
    .waitFor(zitadelSeed);
  await webApp;
} else {
  void webSessionSecret;
}

// ---------------------------------------------------------------------------
// admin (arbeidspakke 1.5, SPEC §6/§10): admin-planet, TanStack Start med
// basePath /admin. Snakker KUN med platform (tenant-registry + provisjonering)
// og gates 100% på platform_admin. EGEN Zitadel-app (egen AUTH_CLIENT_ID),
// separat fra web. Egen SESSION_SECRET og eget cookie-navn (delt origin i prod).
// Samme node-hosting-deferral som web: full `aspire run`-runtime valideres i 1.6;
// prod kjører via Docker/nitro (.output). AUTH_CLIENT_ID er en dev-plassholder -
// ekte admin-OIDC-klient registreres av zitadel-seed (samme oppgraderingssti som
// web sin AUTH_CLIENT_ID/AUTH_AUDIENCE).
// ---------------------------------------------------------------------------
const adminSessionSecret = builder.addParameter('admin-session-secret', {
  value: 'insecure-local-dev-admin-session-32b', // >= 32 tegn, kun lokalt
  secret: true,
});
if (
  platformEndpoint &&
  existsSync(path.join(serviceDir('admin'), 'package.json'))
) {
  const adminApp = builder
    .addExecutable('admin', 'npm', serviceDir('admin'), ['run', 'dev'])
    .withHttpEndpoint({ env: 'PORT' })
    .withEnvironment('PLATFORM_URL', platformEndpoint)
    .withEnvironment('AUTH_ISSUER', zitadel.getEndpoint('http'))
    .withEnvironment('AUTH_CLIENT_ID', 'tronderleikan-admin')
    .withEnvironment('SESSION_SECRET', adminSessionSecret)
    .withEnvironment('OTEL_EXPORTER_OTLP_ENDPOINT', otlpEndpoint)
    .withEnvironment('OTEL_SERVICE_NAME', 'admin')
    .waitFor(zitadel)
    .waitFor(zitadelSeed);
  await adminApp;
} else {
  void adminSessionSecret;
}

await builder.build().run();
