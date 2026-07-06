# TronderLeikan gitops deploy - design

Date: 2026-07-06

## Problem

All 5 TronderLeikan service CI workflows (admin, web, roster, platform, competition) fail on `main` push at the gitops-promote step.
PR builds pass; only main pushes fail, because promote is the first main-only step.

Root cause: the workflows promote to a **per-service** gitops path but the gitops repo has a **single** product folder.
- `gitops-promote` runs `cd gitops/apps/${APP}/overlays/${ENVIRONMENT}` with `APP = tronderleikan-<svc>`.
- kongebra-gitops has only `apps/tronderleikan/` (base + overlays/dev+prod), holding shared infra (CNPG `cluster.yaml`, `databases.yaml`, a commented-out `ingressroute.yaml`). NATS is a separate Helm app (`bootstrap/tronderleikan-nats.yaml`).
- So `cd apps/tronderleikan-admin/overlays/dev` -> "No such file or directory" -> job fails.

## Layout decision (confirmed)

**Option B** - one gitops app folder for the whole product, one namespace per env (`tronderleikan-dev` / `tronderleikan-prod`).
This matches the design already recorded in `apps/tronderleikan/base/kustomization.yaml` ("ADR 0005", never written as a file): services' Deployments/Services land in the base; CI pins each image via its own `images:` entry in the overlays.
The shared CNPG cluster, per-service DB roles, `pg_hba` rules, and single `leikan.newb.no` ingressroute all assume one namespace.

Option A (per-service folders -> per-service namespaces) was rejected: it would break the in-namespace shared-DB model (services reach `tronderleikan-db-rw` in-namespace; cross-namespace needs FQDNs + the DB/pull secrets reflected into 5 namespaces + a split ingressroute) and throw away the existing design.

Note: the CI `app_dir`/`dockerfile:` split is already correct and is NOT a bug - admin/web are self-contained (`app_dir: apps/tronderleikan/<svc>`), while the Go services build from the workspace root (`app_dir: apps/tronderleikan`, `dockerfile: apps/tronderleikan/<svc>/Dockerfile`) because they consume the shared `pkg` module via `replace ../pkg`.

## Phase 1 - unblock CI (no cluster dependency)

Purely a CI fix; ships independently of any Deployment existing, because `kustomize edit set image` writes an `images:` entry whether or not a resource consumes that image yet.

1. In each of the 5 `.github/workflows/tronderleikan-<svc>.yml`: change `app: tronderleikan-<svc>` -> `app: tronderleikan`.
   Keep `image:`, `app_dir:`, `dockerfile:` unchanged. gitops-promote then does `cd apps/tronderleikan/overlays/<env>` (exists) and `kustomize edit set image ghcr.io/kongebra/tronderleikan-<svc>=...@digest`. Each service's distinct image name accumulates as a distinct `images:` entry in the one overlay.
2. In `.github/workflows/_build-deploy.yml`: change the concurrency group from `gitops-${{ inputs.app }}-<env>` to `gitops-${{ inputs.image }}-<env>` (both deploy-dev and deploy-prod).
   Reason: with `app` collapsed to `tronderleikan`, all 5 services would share one group `gitops-tronderleikan-prod`; a simultaneous merge could cancel a pending promote. `image` is unique per deployable, so this keeps per-service serialization while sharing the promote dir. Safe for every other app (all images are unique). gitops-promote's rebase-retry already guards the shared-file writes.

Result: all 5 pipelines go green; images build once and pin into `apps/tronderleikan/overlays/{dev,prod}/kustomization.yaml`.

## Phase 2 - bring services live (dependency-gated, deferred)

Write Deployment + Service manifests into `apps/tronderleikan/base/`, add `_components/hardened-workload` (its patch now has a Deployment to target), add `env` via `_components/env-{dev,prod}`, activate the ingressroute routes incrementally.

Service facts (from code + Dockerfiles):

| Service | Port | Runtime image | Non-root | Required env |
|---|---|---|---|---|
| platform | 8080 | distroless/static-debian12 (**runs as root - pin runAsUser 65532**) | pin uid | DATABASE_URL, NATS_URL, ZITADEL_API_URL, ZITADEL_PAT(/`_FILE`) |
| roster | 8080 | distroless/static-debian12 (pin uid) | pin uid | DATABASE_URL, NATS_URL, AUTH_ISSUER, AUTH_AUDIENCE |
| competition | 8080 | distroless/static-debian12 (pin uid) | pin uid | above + ROSTER_URL |
| admin | 3000 | distroless/nodejs22-debian12:nonroot | yes | PORT (SSR frontend, basePath /admin) |
| web | 3000 | distroless/nodejs22-debian12:nonroot | yes | PORT (catch-all frontend) |

Health probes: Go services use an exec probe `["/app", "-health"]` (distroless, no shell); frontends use httpGet `/healthz`.

Blocking dependencies (verified in-cluster 2026-07-06):
1. **Zitadel is `Missing`/`OutOfSync`** in ArgoCD (manifests exist: `bootstrap/zitadel.yaml`, `platform/zitadel/`). platform/roster/competition all need it (AUTH_ISSUER/AUDIENCE, ZITADEL_PAT). Needs its own investigation.
2. **DB role secrets absent**: `tronderleikan-db-roster` / `tronderleikan-db-competition` do not exist -> those CNPG managed roles have no password -> login impossible (fails safe). Create out-of-band per env (documented in `cluster.yaml` / SECRETS.md). platform uses the auto-generated `tronderleikan-db-app`.
3. platform's `ZITADEL_PAT` secret only exists after Zitadel is up + seeded (machine user, IAM_OWNER).

Sequence: Zitadel healthy -> create DB/auth/PAT secrets -> land Deployments (frontends first, no deps; then Go services) -> uncomment ingressroute routes as each backend Service appears (Traefik errors on routes with a missing backend - the overlay comments already prescribe this activation order).

## Scope

Implement Phase 1 now. Phase 2 stays specced + sequenced but deferred until the Zitadel `Missing` rabbit hole is tackled.
