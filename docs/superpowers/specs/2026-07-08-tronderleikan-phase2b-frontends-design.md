# TrønderLeikan Phase 2b - frontends (web + admin)

Design/spec, 2026-07-08.
Ruller ut de to ferdigbygde TanStack Start-frontendene (`web`, `admin`) til dev + prod, tailnet-only.
Frontend-koden og CI eksisterer allerede fra Phase 1.
Arbeidet her er: provisjonere OIDC-apper i Zitadel, wire config/secrets, og legge til gitops-manifester.

## Bakgrunn og funn

Begge frontends er allerede ferdige OIDC public-client BFF-er:
`openid-client` med Authorization Code + PKCE, `client.None()` (ingen client secret), forseglet iron-session-cookie, `/auth/login` + `/auth/callback`, og rolleutledning fra Zitadels prosjektrolle-claim (`urn:zitadel:iam:org:project:roles`).
CI eksisterer også (`tronderleikan-web.yml` / `tronderleikan-admin.yml`, build-once-promote), og skriver image-digest inn i den delte `apps/tronderleikan`-overlayen.

Den faktiske mangelen er tre ting:

1. To Zitadel OIDC-apper som gir `AUTH_CLIENT_ID` for henholdsvis web og admin.
   Seeden lager prosjekt/roller/grant/brukere, men ingen app-klienter.
2. `SESSION_SECRET` per frontend (den eneste ekte hemmeligheten - `client_id` er en public PKCE-klient og er ikke hemmelig).
3. gitops-manifester for web + admin (Deployment/Service/IngressRoute/ConfigMap), samme golden path som backendene.

De to backend-verdiene de trenger (`AUTH_ISSUER`, `AUTH_AUDIENCE=380729371201110362`) finnes allerede som den delte `tronderleikan-auth` ConfigMap.

## Beslutninger

- Eksponering: begge frontends er tailnet-only i Phase 2b.
  `web-public` er bevisst utsatt (se Deferred), fordi det forutsetter at Zitadel (`auth.newb.no`) også eksponeres - login redirecter nettleseren til IdP-en, og Zitadel er i dag tailnet-only.
- Vertsnavn (alle single-label under `*.newb.no`, gratis wildcard-TLS + DNS):
  - web: `leikan.newb.no` (prod) / `leikan-dev.newb.no` (dev).
  - admin: `leikan-admin.newb.no` (prod) / `leikan-admin-dev.newb.no` (dev).
  - Merk: nestet `admin.leikan.newb.no` ble vurdert men forkastet - single-label wildcard `*.newb.no` dekker det ikke, og det ville krevd egen `*.leikan.newb.no` cert + DNS-record.
- OIDC-provisjonering: utvid `zitadel-seed` (seeden eier allerede prosjektet).
  Terraform-provider og custom operator ble vurdert og forkastet for nå (se Deferred).
- Én OIDC-app per frontend, som spenner begge miljøer (konsistent med at ett Zitadel-prosjekt deles dev + prod).
- `SESSION_SECRET`: out-of-band k8s Secret, samme mønster som `zitadel-masterkey`, dokumentert i `SECRETS.md` + 1Password.
- Ingen Gatus-annotering enn så lenge (lett å legge til senere).

## 1. OIDC-app-provisjonering - utvid `zitadel-seed`

Legg til idempotent find-or-create av to OIDC-apper i det eksisterende `tronderleikan`-prosjektet (plattform-org).
Mønsteret speiler project-grant-konvergeringen som allerede finnes i `seed.go`: finn, og hvis den finnes med feil redirect-sett, konvergér i stedet for bare å akseptere.

| App | Klienttype | Redirect-URIer (én app, begge miljøer) | Post-logout |
|---|---|---|---|
| `tronderleikan-web` | OIDC, PKCE, public (auth none), `code` + `refresh_token` | `https://leikan.newb.no/auth/callback`, `https://leikan-dev.newb.no/auth/callback` | `https://leikan.newb.no/`, `https://leikan-dev.newb.no/` |
| `tronderleikan-admin` | samme | `https://leikan-admin.newb.no/admin/auth/callback`, `https://leikan-admin-dev.newb.no/admin/auth/callback` | `https://leikan-admin.newb.no/admin`, dev-ekvivalent |

admin-stiene bærer `/admin` fordi admin-appen monterer hele rute-treet under basePath `/admin` (bekreftet i `vite.config.ts` `base: '/admin'` + `router.basepath`).

Endringer i seed:

- `Directory`-interfacet får `FindOIDCApp` / `CreateOIDCApp` / `UpdateOIDCApp` (konvergér redirect-URIer slik grants konvergerer roller).
- `Result` får `WebClientID` og `AdminClientID`; seeden logger dem.
- Enhetstester: table-test de nye OIDC-byggeklossene mot fake-`Directory` (eksisterende `seed_test.go`-mønster).

Leveranse av client_id:
`client_id` er public (PKCE, ikke hemmelig).
Kjør seed, les de to id-ene fra Job-loggen, lim dem inn i en gitops-ConfigMap én gang.
Idempotent og stabil på tvers av re-kjøringer.

## 2. Secrets og config

- `SESSION_SECRET` (eneste ekte hemmelighet): 4 out-of-band k8s Secrets (`web`/`admin` × `dev`/`prod`), håndlaget, dokumentert i `SECRETS.md` + 1Password. Per-miljø-isolasjon.
- Client-id-er: `tronderleikan-web-oidc` / `tronderleikan-admin-oidc` ConfigMaps (`AUTH_CLIENT_ID`), i `base` (samme id på tvers av miljøer).
- Gjenbruk `tronderleikan-auth` ConfigMap for `AUTH_ISSUER` + `AUTH_AUDIENCE`.
- Service-URLer (base, namespace-lokale, identiske i begge miljøer): `http://platform:8080`, `http://roster:8080`, `http://competition:8080` (bekreftet: alle backend-Services heter dette og eksponerer port 8080).

Env per frontend:

- web: `AUTH_ISSUER`, `AUTH_CLIENT_ID`, `AUTH_AUDIENCE`, `AUTH_SCOPES` (valgfri), `SESSION_SECRET`, `PLATFORM_URL`, `ROSTER_URL`, `COMPETITION_URL`, `WEB_BASE_URL` (per miljø).
- admin: `AUTH_ISSUER`, `AUTH_CLIENT_ID`, `AUTH_AUDIENCE`, `SESSION_SECRET`, `PLATFORM_URL`, admin base-URL (per miljø).

## 3. gitops-manifester (`apps/tronderleikan/`)

Legg til i `base/`: `web-deployment.yaml` + `web-service.yaml`, `admin-deployment.yaml` + `admin-service.yaml`.

- Container `app`, image `ghcr.io/kongebra/tronderleikan-{web,admin}` (CI promoter allerede digest til den delte overlayen).
- `runAsUser: 65532` pinnet (distroless `nodejs22-debian12:nonroot`), port `3000`, `httpGet /healthz`-probe.
- Skrivbar `/tmp` emptyDir hvis node-serveren trenger det (verifiseres ved impl - `readOnlyRootFilesystem` arves fra hardened-workload).
- `components:` `hardened-workload` + `limitrange`.
- `overlays/{dev,prod}`: image-digest, IngressRoute, env-label, `WEB_BASE_URL`/admin-base per miljø, `SESSION_SECRET` secretRef.
- IngressRoutes (alle `entryPoints: [websecure]`, default TLSStore, ingen expose-public):
  web `Host(\`leikan[-dev].newb.no\`)`; admin `Host(\`leikan-admin[-dev].newb.no\`)`.

## 4. Bootstrap-rekkefølge (chicken-and-egg)

1. Utvid seed -> deploy -> kjør Job -> les `WebClientID`/`AdminClientID` fra loggen.
2. Lag de 4 `SESSION_SECRET`-ene, dokumentér.
3. Commit client-id-ConfigMaps.
4. Commit web + admin-manifester -> ArgoCD synker.
5. E2E-verifiser login på tailnet (dev først, så prod).

## 5. Testing

- Seed: table-test de nye OIDC find-or-create/konvergér-byggeklossene mot fake-`Directory`.
- E2E: på tailnet, treff `leikan-dev.newb.no` -> login -> Zitadel -> callback -> session-cookie -> rolle-gated view rendrer; samme for `leikan-admin-dev.newb.no/admin`.

## Deferred / future

- web-public (fast-follow, null rework): én `expose-public`-linje på web prod-overlay + eksponer Zitadels OIDC/login-endepunkt.
  Egen liten oppgave når Zitadel-eksponeringsbeslutningen er tatt (f.eks. public login-stier, `/console` forblir tailnet, eller Cloudflare Access foran).
- Gatus-status-annotering av frontendene (dev-nøkkel på dev-overlay, plain nøkkel på prod).
- Root -> `/admin`-redirect på admin-hosten (så bare `leikan-admin.newb.no` uten sti lander riktig). Kosmetisk.

### OIDC-provisjonering som operator / IaC (arkitektur-notat)

Mønsteret "opprett en klient av gitt type, populer riktige env-varer, konvergér redirect-URIer" er operator-formet (jf. Keycloaks `KeycloakClient` CR).
Det ville fjernet den ene wartet i planen over: manuell "les client_id fra Job-logg, lim inn i ConfigMap".
Det bygges likevel ikke nå:

- Ingen moden hyllevare-Zitadel-operator finnes; å bygge egen (kubebuilder/controller-runtime, RBAC, reconcile, drift) er et reelt prosjekt for to statiske apper som endres nesten aldri.
- Seeden ER allerede en poor-man's operator: en idempotent run-to-converge-reconciler, bare on-demand i stedet for kontinuerlig watch. For statisk bootstrap er det 90 % av verdien til 5 % av kostnaden.
- En operator kjøper bare to ting over seeden: kontinuerlig drift-korreksjon, og auto-skriving av client_id til en Secret. Ingen av delene betyr noe når settet er to apper.

Ladder cheapest -> heaviest: (1) seed [valgt nå], (2) Terraform Zitadel-provider [drar TF + state inn i et kustomize-repo], (3) custom operator [`kubectl apply` CR-ergonomi, nice-to-have].

Den ekte triggeren for å revurdere er ikke mer polish, men runtime-provisjonering:
TrønderLeikan er multi-tenant, og den dagen tenant-onboarding må opprette Zitadel-ressurser i runtime (ny org + config når en tenant registrerer seg), passer ikke en build-time-seed lenger.
Riktig hjem for det er `platform`-tjenestens tenant-create-flyt, som allerede snakker med Zitadel (`platform/zitadel_client.go`), ikke en separat operator - tjenesten eier tenant-livssyklusen, så å la den også minte Zitadel-ressurser er kohesivt.
Bygg en operator kun hvis den deklarative CR-ergonomien i seg selv er ønsket.
