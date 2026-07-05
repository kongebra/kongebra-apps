# zitadel-seed

Idempotent provisjonering av TrønderLeikans Zitadel-grunntilstand (arbeidspakke 0.4, SPEC §5, §6, §12).

## Hva den lager

Mot en Zitadel-instans sikrer seeden - hver del sjekk-før-opprett:

- **Plattform-org** (eier prosjektet).
- **Project `tronderleikan`** i plattform-orgen, med `projectRoleAssertion` + `projectRoleCheck` på (så JWT bærer roller og tilgang krever eksplisitt grant).
- **De 4 project-rollene** (SPEC §6): `player`, `organizer`, `tenant_admin`, `platform_admin`. Rollenøklene kommer fra `pkg/authn` - samme kilde som `authn.Validator` leser ut av JWT-en, så seed og validering aldri kan drifte fra hverandre.
- **Én test-tenant-org** med **project-grant** på `tronderleikan`. Grantet gir rollene `player`/`organizer`/`tenant_admin` - `platform_admin` er bevisst utelatt ("kun tildelbar i plattform-orgen", SPEC §6).
- **Testbrukere** med rolletildelinger:
  - `platform-admin@tronderleikan.local` -> `platform_admin` (i plattform-orgen)
  - `tenant-admin@demo.tronderleikan.local` -> `tenant_admin` (i test-tenant-orgen)
  - `player@demo.tronderleikan.local` -> `player` (i test-tenant-orgen)

## Konfig (env)

Issuer/domenet leses ALLTID fra env (SPEC §5, ufravikelig) - aldri hardkodet.

| Env | Påkrevd | Beskrivelse |
|---|---|---|
| `ZITADEL_API_URL` | ja | Full URL til instansen, f.eks. `http://localhost:8300` (lokal) eller `https://auth.newb.no` (cluster). `http` -> insecure gRPC, `https` -> TLS. |
| `ZITADEL_PAT` | en av* | Personal Access Token for en machine-user med `IAM_OWNER`, inline. |
| `ZITADEL_PAT_FILE` | en av* | Sti til fil som inneholder PAT-en (Zitadel FirstInstance skriver den hit). |
| `SEED_TEST_PASSWORD` | ja | Passord for testbrukerne. Settes eksternt (lokalt av apphost), aldri hardkodet i Go. |
| `SEED_PLATFORM_ORG_NAME` | nei | Default `TronderLeikan Platform`. |
| `SEED_TENANT_ORG_NAME` | nei | Default `TronderLeikan Demo Tenant`. |
| `SEED_PROJECT_NAME` | nei | Default `tronderleikan`. |

*Nøyaktig én av `ZITADEL_PAT` / `ZITADEL_PAT_FILE` må være satt.

## Kjøre lokalt (Aspire)

Seeden er wiret inn i `apphost/apphost.mts` som en `addExecutable` med `waitFor(zitadel)`. Aspire:

1. Starter Zitadel med `FirstInstance` som oppretter en machine-user (`IAM_OWNER`) og skriver PAT-en til en bind-mountet fil (`apphost/.zitadel/pat.txt`).
2. Kjører seeden med `ZITADEL_API_URL` (fra zitadel-endpointet), `ZITADEL_PAT_FILE` (host-stien til PAT-fila) og `SEED_TEST_PASSWORD` (lokal dev-parameter).

```bash
cd apps/tronderleikan/apphost
aspire run
```

Lokal state er flyktig (ingen data-volumes): hver oppstart kjører `FirstInstance` på nytt, og den idempotente seeden bygger tilstanden opp igjen.

### Manuelt (uten Aspire)

```bash
cd apps/tronderleikan/zitadel-seed
export ZITADEL_API_URL="http://localhost:8300"
export ZITADEL_PAT_FILE="/sti/til/pat.txt"   # eller: export ZITADEL_PAT="..."
export SEED_TEST_PASSWORD="Password1!"
go run .
```

## Idempotens-garanti

To kjøringer på rad gir samme sluttilstand. Orgs, project og project-grant slås opp på navn/granted-org før de eventuelt opprettes; roller og user-grants tåler "finnes allerede"-feil (gRPC `AlreadyExists`) grasiøst; brukere slås opp på e-post i sin org.

Bevist ende-til-ende mot Zitadel `v4.15.3`: to kjøringer -> identiske org/project/grant/user-ID-er, ingen duplikater (verifisert via REST-gatewayen: 1 grant i plattform-org, 2 i tenant-org, riktige roller). Beslutningslogikken er dessuten enhetstestet mot en streng fake som feiler hvis noe opprettes to ganger (`seed_test.go`).

## Bruk mot cluster (senere)

Samme binary, annen konfig (arbeidspakke 0.5, infra-handoff):

- `ZITADEL_API_URL=https://auth.newb.no`.
- En machine-user med `IAM_OWNER` opprettes én gang; PAT-en legges i en k8s-secret og eksponeres som `ZITADEL_PAT` (eller monteres som fil -> `ZITADEL_PAT_FILE`).
- `SEED_TEST_PASSWORD` settes fra secret (eller testbrukerne droppes i prod ved å tømme bruker-lista - egen justering når det trengs).
- Kjøres som et Kubernetes `Job`. En distroless-image legges til når cluster-jobben wires (`// ponytail:` - ingen Dockerfile ennå; unødvendig før 0.5).

## Struktur

- `config.go` - env-lasting + URL-parsing (rene, testbare funksjoner).
- `seed.go` - `Directory`-grensesnittet, `Seeder` med sjekk-før-opprett-logikken, rolle-definisjonene.
- `directory_zitadel.go` - tynn adapter mot zitadel-go v3 (proto-marshaling, org-kontekst-header, `AlreadyExists`-toleranse). Ikke enhetstestet (krever levende Zitadel).
- `*_test.go` - config-parsing + idempotens/tilstand mot fake `Directory`.
