# TronderLeikan Phase 2a - backends live (platform / roster / competition)

Date: 2026-07-07

## Context

Phase 1 (CI promote to the shared `apps/tronderleikan` overlay) is done: all 5 service images build + pin, pipelines green. No service Deployments exist in gitops yet - the product is not running.

Phase 2 = bring the services live. It splits by dependency:
- **2a (this spec): backends** - `platform`, `roster`, `competition`. Pure gitops manifests + out-of-band secrets. Ships 3 healthy backends + a seeded Zitadel structure.
- **2b (separate cycle): frontends** - `web`, `admin`. Blocked on an OIDC-app provisioning code gap (neither `zitadel-seed` nor `platform` creates OIDC apps) + `SESSION_SECRET`s. Gets its own design.

The frontends are BFFs (`web` calls all 3 backends, `admin` calls `platform`), so the real order is backends first - the reverse of the original Phase 2 sketch, which wrongly assumed the frontends were static.

Prereqs already live: CNPG `tronderleikan-db` (Running, `tronderleikan-db-app` secret exists), NATS (`nats` svc, 3/3), Zitadel (`Synced/Healthy`, `iam-admin-pat` IAM_OWNER PAT in ns `zitadel`).

## Decisions

- **One namespace per env** (`tronderleikan-dev` / `tronderleikan-prod`); all manifests in `apps/tronderleikan/base/`, images pinned per overlay (Option B, Phase 1).
- **Audience flow = manual capture, one-time per env.** `roster`/`competition` build their JWT validator (`pkg/authn`) from a fixed `AUTH_AUDIENCE` = the Zitadel project ID, which is generated when the project is created. `zitadel-seed` creates org/project/roles and logs the project ID; the operator records it into a per-env ConfigMap. No new code / RBAC. Re-seeding a fresh Zitadel means updating the value. (Automating via a seed-written ConfigMap, or a boot-time lookup-by-name in `pkg/authn`, is a deferred option if manual capture becomes painful.)
- **`zitadel-seed` runs manually, not as a k8s Job.** It has no Dockerfile / CI workflow (a `go run` tool built for local Aspire). Containerizing a run-once idempotent bootstrap is not worth a Dockerfile + workflow + Job manifest. Run it once per env from a laptop (`go run .` in `apps/tronderleikan/zitadel-seed`, tailnet reaches `auth.newb.no`, `ZITADEL_PAT` = the `iam-admin-pat` value). Consistent with the manual-capture audience decision. Upgrade path if seeding must be repeatable/automated: add a Dockerfile + CI workflow + run as an Argo PreSync Job.
- **`platform`** self-resolves the project by name (`FindProjectByName`), so it does not need `AUTH_AUDIENCE`; it needs `ZITADEL_API_URL` + `ZITADEL_PAT`.
- **`ZITADEL_API_URL = https://auth.newb.no`** (through Traefik) for platform + seed. Zitadel routes by Host header / ExternalDomain; an internal `zitadel.zitadel.svc:8080` call fails the instance-routing check unless the client sends the external Host, so the edge URL is the simple correct choice.
- **`iam-admin-pat` reflected** from ns `zitadel` into the tronderleikan namespaces (emberstack/reflector, same mechanism as `ghcr-pull`), consumed as `ZITADEL_PAT` by the seed Job + platform.
- **DB role secrets** `tronderleikan-db-{roster,competition}` created out-of-band (`kubernetes.io/basic-auth`, `username`+`password`), like the Zitadel login-client secret. CNPG sets the managed-role password from them; the service builds `DATABASE_URL` via k8s `$(VAR)` env expansion, one secret per role (no password duplicated). platform uses the CNPG-owned `tronderleikan-db-app` directly.
- **`replicas: 1`** everywhere (repo default). HA is a later prod-overlay patch.

## Components / files

New in `apps/tronderleikan/base/` (added to `kustomization.yaml` `resources:`):
- (no seed manifest - `zitadel-seed` runs manually per env, see Decisions)
- `platform-deployment.yaml` + `platform-service.yaml` - port 8080, `runAsUser: 65532`, exec probe `["/app","-health"]`, env: `DATABASE_URL` (from `tronderleikan-db-app`), `NATS_URL=nats://nats:4222`, `ZITADEL_API_URL`, `ZITADEL_PAT`.
- `roster-deployment.yaml` + `roster-service.yaml` - port 8080, `runAsUser: 65532`, exec probe, env: `DATABASE_URL` built from `tronderleikan-db-roster` (`$(DB_USER)`/`$(DB_PASS)` -> `postgresql://...@tronderleikan-db-rw:5432/roster?sslmode=require`), `NATS_URL`, `envFrom` ConfigMap `tronderleikan-auth`.
- `competition-deployment.yaml` + `competition-service.yaml` - as roster, db `competition`, plus `ROSTER_URL=http://roster:8080`.
- ConfigMap `tronderleikan-auth` (per overlay, since `AUTH_AUDIENCE` differs by env; `AUTH_ISSUER` shared): `AUTH_ISSUER=https://auth.newb.no`, `AUTH_AUDIENCE=<project-id>`. Written into the overlay because the value is captured post-seed and differs dev vs prod.

Kustomization changes:
- `base/kustomization.yaml`: add the new resources; add `_components/hardened-workload` (now that Deployments exist for its patch to target) - keep `stateful` + `limitrange`.
- overlays: add `_components/env-{dev,prod}`; uncomment + point the `/api/platform|roster|competition` ingressroute routes (ports match the Services); add the per-env `tronderleikan-auth` ConfigMap.

Reflector annotation: add `tronderleikan-*` namespaces to `iam-admin-pat`'s reflection allowlist (in `platform/zitadel/` or wherever the secret is defined - it is operator-created by the setup job, so the reflection annotation is applied out-of-band or via a small patch; resolve in the plan).

## Security posture

`hardened-workload` carries `runAsNonRoot`, seccomp, `readOnlyRootFilesystem`, drop-ALL caps, `ghcr-pull`. Go services use `gcr.io/distroless/static-debian12` **without `:nonroot`** = runs as root -> MUST pin `runAsUser: 65532` (+ `runAsGroup`/`fsGroup`) in each base Deployment (the component deliberately omits uid). `readOnlyRootFilesystem` is fine - the Go binaries write nothing (no `/tmp` need identified; add an emptyDir only if a service turns out to write).

## Out-of-band steps (per env, documented in gitops SECRETS.md)

1. Create `tronderleikan-db-{roster,competition}` basic-auth secrets (random passwords; copy to 1Password).
2. Annotate `iam-admin-pat` for reflection into `tronderleikan-{dev,prod}` (so platform gets `ZITADEL_PAT` in-cluster).
3. Run `zitadel-seed` once for the env (`go run .` from `apps/tronderleikan/zitadel-seed`, `ZITADEL_API_URL=https://auth.newb.no`, `ZITADEL_PAT=<iam-admin-pat value>`); read the project ID from its output, set `AUTH_AUDIENCE` in the `tronderleikan-auth` ConfigMap for that env.

## Verification

- `zitadel-seed` run prints a project ID; org/project/roles visible in the Zitadel console.
- Each of platform/roster/competition: pod `Running` (not `CreateContainerConfigError` = uid pinned correctly), `/healthz` 200.
- `app-tronderleikan-{dev,prod}` `Synced/Healthy` in ArgoCD.
- `leikan.newb.no/api/platform/...` reachable through Traefik.
- roster/competition accept a Zitadel-issued JWT whose `aud` = the captured project ID, reject one with a wrong audience.

## Out of scope (2b / later)

Frontends (`web`, `admin`), OIDC-app provisioning, `SESSION_SECRET`s, the prod dev-health precheck (tailscale), CNPG HA/backup, DB `bracket/timing/live/rating` roles.
