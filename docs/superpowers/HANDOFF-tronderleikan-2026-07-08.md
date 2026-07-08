# Handoff - 2026-07-08 (tronderleikan Phase 2a + Zitadel + saga)

Context handoff for a fresh session. Covers three threads worked since the 2026-07-06 saga handoff: Zitadel brought live, TronderLeikan backends (Phase 2a) deployed dev+prod, and a saga sub-lang fix (built, not yet in prod).

## Repos & access
- App code: `kongebra/kongebra-apps` (Go services + TanStack frontends under `apps/tronderleikan/`; `apps/saga-api`).
- GitOps/deploy: `kongebra/kongebra-gitops` (`apps/tronderleikan/`, `platform/zitadel/`, `bootstrap/zitadel.yaml`).
- Cluster: `export KUBECONFIG=~/.kube/kongebra-config`.
- Specs/plans: `kongebra-apps/docs/superpowers/{specs,plans}/2026-07-0{6,7}-tronderleikan-*`.
- Secrets doc: `kongebra-gitops/SECRETS.md`. All live passwords + the Zitadel admin + `SEED_TEST_PASSWORD` + the 4 DB role passwords are in Svein's 1Password.

## LIVE and verified
- **Zitadel** (`auth.newb.no`): `Synced/Healthy`. Root-caused + fixed the `Missing` state - the out-of-band `zitadel-login-client` secret (the `IAM_LOGIN_CLIENT` SystemAPIUser keypair) was never created (peer to `zitadel-masterkey`); created it, synced, login-v2 UI + console both 200. Human admin `zitadel-admin@zitadel.auth.newb.no` (password rotated -> 1Password). Machine admin `iam-admin` (PAT in `zitadel/iam-admin-pat`).
- **TronderLeikan Phase 2a backends** (`platform`, `roster`, `competition`): **live dev AND prod**, both `app-tronderleikan-{dev,prod}` `Synced/Healthy`, pods stable, `/api/{platform,roster,competition}` route through Traefik (`leikan-dev.newb.no` / `leikan.newb.no`), shared NATS `tl` stream with roster+competition consumers attached. Manifests in `apps/tronderleikan/base/` (Deployment+Service each, `runAsUser: 65532`, exec probe `/app -health`, hardened-workload). Auth via `tronderleikan-auth` ConfigMap (`AUTH_ISSUER=https://auth.newb.no`, `AUTH_AUDIENCE=380729371201110362` = seeded project ID; ONE Zitadel/project shared dev+prod).
- **CI (Phase 1)**: all 5 tronderleikan services promote to the shared `apps/tronderleikan` overlay. Fixed a shared-overlay write race in `gitops-promote` (re-derive instead of rebase; identity-inside-repo).

## Bugs fixed during the Phase 2a deploy (all resolved)
1. platform also needs `AUTH_ISSUER`/`AUTH_AUDIENCE` (builds an authn.Validator) -> `envFrom tronderleikan-auth`.
2. **CNPG managed-role reconcile ordering** (gotcha): create the `passwordSecret`s before/around cluster creation, else roles stay `pending-reconciliation` ("secret not found", stale) and the DBs never get created. Fix used: restart the `cnpg-cloudnative-pg` operator to force reconcile.
3. **NATS stream overlap**: platform created its own `tl-platform` (`tl.platform.>`), overlapping the shared `tl` (`tl.>`) that roster/competition ensure. Fixed in kongebra-apps (`platform/main.go`, commit d313e4e: platform joins the shared `tl`). Convergence needed a manual `nats stream rm tl-platform`.

## Ops notes learned
- **Alert noise during a known-broken deploy:** use an Alertmanager silence per namespace (port-forward `kube-prometheus-stack-alertmanager`, POST `/api/v2/silences`). Do NOT fight ArgoCD - the ApplicationSet restores `selfHeal`, so `kubectl scale --replicas=0` / disabling automated sync gets reverted.
- Direct pushes to `main` (both repos) are blocked for the agent by the auto-mode classifier - Svein pushes.

## OPEN / next steps
1. **Push the doc commit**: `kongebra-apps` has 1 unpushed commit `aa987cc` (Phase 2a deploy outcome doc). `cd ~/github/kongebra/kongebra-apps && git push origin main`.
2. **saga sub-lang 429 fix NOT in prod.** The fix (commit 6292159, `apps/saga-api/internal/ytdlp` - fetch `[lang,en]` instead of always `no,nb,en`) built but its prod deploy was never approved; `saga-api-prod` still runs the old image (`sha256:e32d0109...`). Approve saga-api's `production` gate to ship it, then do saga follow-up #1: run a real summary and confirm the subtitle 429 is gone (after residential-IP cooldown).
3. **Prod-gate policy still undecided.** The `production` GitHub environment has manual `required_reviewers`. Recommendation was: keep manual approval through Phase 2, build a tailscale-connected dev-health precheck as the real gate later, then swap. Options + the removal command are in the Phase 2a spec's "Prod deploy gate" section.
4. **Phase 2b - frontends (`web`, `admin`)**: blocked on a code gap - neither `zitadel-seed` nor `platform` creates the OIDC apps the frontends authenticate through (`AUTH_CLIENT_ID` for `tronderleikan-web`/`-admin`). Needs a design decision (who creates the OIDC apps: extend seed / manual console / provider) + likely kongebra-apps code, then Deployments + `SESSION_SECRET`s. Its own brainstorm -> spec -> plan cycle. web is a BFF (needs all 3 backend URLs + auth); admin needs platform + auth.
5. **Demo data in the LIVE Zitadel**: the seed created a "TronderLeikan Demo Tenant" org + 3 test users (`platform-admin@tronderleikan.local`, `tenant-admin@demo.tronderleikan.local`, `player@demo.tronderleikan.local`). Delete them if a clean prod IdP is wanted - the backends don't need them.
6. Deferred saga minors (original handoff): ffmpeg warning (cosmetic, skipped on purpose), `node:22->24` bump for the other apps, `RetryButton` -> shadcn `<Button>`.

## How to resume
1. `export KUBECONFIG=~/.kube/kongebra-config`.
2. Read this file + `docs/superpowers/specs|plans/2026-07-07-tronderleikan-phase2a-*` + `kongebra-gitops/SECRETS.md`.
3. Verify state: `kubectl -n argocd get app | grep tronder` (expect Synced/Healthy); `kubectl -n tronderleikan-prod get pods`.
4. Pick a thread: push the doc commit (#1), ship saga (#2), or start Phase 2b (#4, the big one - brainstorm the OIDC-app provisioning first).

---

## UPDATE 2026-07-08 evening - Phase 2b DONE + saga fixes shipped

Session outcome. Resolves OPEN items #1 (was already pushed), #2, #4 above.

### saga (both shipped + verified in prod)
- **sub-lang 429 fix** (#2): shipped first (build `eaae17d` -> digest `1d299801`). Verified: re-queued the previously-failing job 4 -> completed first attempt, `error: null`, no 429.
- **LaTeX-in-output fix** (new, commit `6b07947`): small local model (`gemma`) emitted `$\rightarrow$` etc. that the web renderer showed raw. Added `summarize.CleanMath` (deterministic regex: LaTeX commands -> Unicode, unwrap `$...$` around single symbols; prices safe) applied to both summary + translate paths, plus a prompt guard. Shipped on top of the 429 fix; **current `saga-api-prod` = `sha256:1ef51ea2`** (contains both). Verified: translated job 4 -> clean `->` arrows, zero LaTeX.

### Phase 2b frontends - LIVE dev + prod, tailnet-only
Spec: `docs/superpowers/specs/2026-07-08-tronderleikan-phase2b-frontends-design.md`. Plan: `docs/superpowers/plans/2026-07-08-tronderleikan-phase2b-frontends.md`.

Both `web` + `admin` (already-built TanStack Start OIDC public-PKCE BFFs) deployed to `tronderleikan-{dev,prod}`, tailnet-only. Login verified (player on web-dev). admin login + logout fixed (see gotchas).

- **OIDC provisioning:** extended `zitadel-seed` to idempotently find-or-create two OIDC apps in the `tronderleikan` project and emit their `client_id`s (mirrors the project-grant converge pattern). Public PKCE web clients (AppType WEB, AuthMethod NONE, Code + refresh, JWT access token + role assertion). Ran locally against `auth.newb.no` with the IAM PAT.
- **client_ids** (public, non-secret; in gitops base ConfigMaps `tronderleikan-{web,admin}-oidc`): web `380895887334900058`, admin `380895887569715546`. Same in dev + prod (one app per frontend spans both envs). Re-seeding a fresh Zitadel changes these -> update the ConfigMaps.
- **Hosts** (single-label under `*.newb.no`, free wildcard TLS/DNS): web `leikan.newb.no` / `leikan-dev.newb.no`; admin `leikan-admin.newb.no` / `leikan-admin-dev.newb.no`. admin serves under basePath `/admin`, so it lives at `leikan-admin[-dev].newb.no/admin`.
- **Routing:** web is the bare-host catch-all in the existing per-host IngressRoute (longest-match keeps `/api/*` ahead); admin is a SEPARATE IngressRoute/host so web can later go public while admin stays tailnet.
- **SESSION_SECRET:** 4 out-of-band k8s Secrets `tronderleikan-{web,admin}-session` in `tronderleikan-{dev,prod}` (key `SESSION_SECRET`, 64-hex). Created by hand; mirror into 1Password + `SECRETS.md`.
- **Manifests:** `apps/tronderleikan/base/{web,admin}-{deployment,service}.yaml` + `-oidc-config.yaml`; overlays add IngressRoutes + `WEB_BASE_URL`/`ADMIN_BASE_URL` patch. distroless nodejs22 `:nonroot`, `runAsUser 65532`, port 3000, `/healthz` (web) / `/admin/healthz` (admin) probes, writable `/tmp` emptyDir.

### Phase 2b gotchas (learned the hard way)
- **admin `ADMIN_BASE_URL` must be ORIGIN-ONLY** (`https://leikan-admin-dev.newb.no`), NOT `.../admin`. The BFF builds `redirect_uri = resolveOrigin + withBase('/auth/callback')` = origin + `/admin/auth/callback`; a `/admin` suffix on the base double-prefixed it to `/admin/admin/auth/callback` -> Zitadel "redirect_uri missing in client configuration". web's `WEB_BASE_URL` is fine (web has no basePath).
- **admin post-logout needs the trailing slash** `.../admin/` (the app produces `resolveOrigin + withBase('/')` = origin + `/admin/`). Seed originally registered `.../admin` (no slash) -> logout "post_logout_redirect_uri invalid".
- **seed convergence originally keyed on redirect URIs only**, so re-seeding did NOT fix the post-logout drift. Fixed: `FindOIDCApp` now also returns current post-logout, and `ensureOIDCApp` converges on redirect OR post-logout diff (commit `fb51b0d`, closes the Phase A final-review gap). Re-ran seed -> converged live.
- **prod admin had no image digest** (auto-promoted to dev but never prod-gated) -> `ImagePullBackOff` on `:latest`. Fixed by running `tronderleikan-admin` CI + approving the prod gate (digest `48c3f08b` now in both overlays).

### Remaining Phase 2b follow-ups (non-blocking)
- **web-public** (deferred fast-follow): add `_components/expose-public` to web's overlay AND expose Zitadel's OIDC/login endpoints (currently tailnet-only). Needs the Zitadel-exposure decision (public login paths, `/console` stays tailnet, or Cloudflare Access). One line + the Zitadel change; zero rework.
- **admin root -> /admin redirect** (cosmetic): `leikan-admin[-dev].newb.no/` (no path) 404s; only `/admin` works.
- **Gatus** status-page annotation for the frontends (dev key on dev, plain on prod).
- **Future OIDC provisioning** (architecture note in the spec): stays in the seed now; fold runtime tenant-provisioning into `platform`'s tenant-create flow (already talks to Zitadel via `platform/zitadel_client.go`) when multi-tenant lands - not a separate operator.

### Still open from the morning handoff
- #3 prod-gate policy (undecided). #5 demo data in LIVE Zitadel (delete if a clean prod IdP is wanted; the 3 test users still exist and are used for login verification). #6 saga minors.
