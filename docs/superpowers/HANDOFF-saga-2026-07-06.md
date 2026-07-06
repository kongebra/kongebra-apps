# Saga handoff - 2026-07-06

Context handoff for a fresh session. Saga (personal agent platform) is **LIVE on `saga.kongebra.no`**, prod-only, on the kongebra k3s cluster.

## Repos & access
- App code: `kongebra/kongebra-apps` (`apps/saga-api` Go backend, `apps/saga-web` TanStack Start UI). Everything is merged to `main`.
- GitOps/deploy: `kongebra/kongebra-gitops` (`apps/saga-api` + `apps/saga-web` overlays, prod only; `platform/dns/kongebra-wildcard-cert.yaml`). On `main`.
- Cluster: `export KUBECONFIG=~/.kube/kongebra-config`. saga namespaces: `saga-api-prod`, `saga-web-prod`.
- SDD ledger (full task-by-task history): `kongebra-apps/.superpowers/sdd/progress.md`.
- Specs/plans: `kongebra-apps/docs/superpowers/specs|plans/2026-07-0{4,5}-saga-*`.

## What's live and working
- v1 (backend + web + deploy) and v1.1 shipped. Verified on prod via Playwright.
- Architecture: Postgres-backed job queue + in-process worker + single n=1 Ollama chokepoint; saga-api on the **home node** (`k3s-home-1`, residential IP for yt-dlp) with CNPG `saga-db` on Hetzner; saga-web on Hetzner. Ingress path-split: `/api`->saga-api (priority 20), else->saga-web (10). TLS via reflected `*.kongebra.no` SNI cert.
- v1.1 features: shadcn/ui + tailwind v4 (light/dark theme), live ETA on chunking, YouTube thumbnail/embed + metadata via oEmbed, **English default** + on-demand translate-to-Norwegian (cached), stored `video_title`/`video_description` shown in the job list. DB migrations 0001/0002/0003 apply at startup.
- CI: prod-only workflows (`.github/workflows/saga-api.yml`, `saga-web.yml`) - build once, promote to prod behind the GitHub `production` environment gate. saga-web Docker base is `node:24-slim` (current LTS / npm 11, matches the lockfile toolchain).

## Just fixed (this session, live)
- **429-hardening flag regression**: `--retry-sleep http=exp=3:60` was invalid on yt-dlp 2026.06.09 (exit status 2 - every summary failed). Fixed to plain `--retry-sleep 5` (commit `6f254cc`, deployed + verified: the flag error is gone).

## KNOWN ISSUES / immediate saga follow-ups
1. **Subtitle 429 (residential IP rate-limit).** yt-dlp gets `HTTP 429` on the timedtext (subtitle) endpoint when the home IP has been hammered (heavy testing this session). Summaries DO complete when the IP is not throttled (jobs 1 & 2 on prod are `done`). Let the IP cool (~1h+), then confirm a clean summary.
2. **yt-dlp fetches `no` subs first regardless of summary language.** `apps/saga-api/internal/ytdlp/ytdlp.go` uses a fixed `--sub-langs "no,nb,en"`; an English video with a Norwegian-first request 429s on the `no` attempt. Worth: fetch subs in the requested `lang` first (or just `en` + auto-subs), which also reduces requests -> fewer 429s. GOOD NEXT SAGA TASK.
3. **ffmpeg not in the saga-api image** - a harmless WARNING on every fetch. Optional: add ffmpeg to `apps/saga-api/Dockerfile`.
4. Deferred UI minors: `RetryButton` uses hand-rolled classes instead of shadcn `<Button>`; repo-wide `node:22`->`node:24` bump for the other apps.

## RECOMMENDED NEXT STEP (scoped, NOT started - user asked to hand off first)
**Fix the tronderleikan deploys** (separate project, actively broken - "admin deploy failed"). All 5 service CI runs (admin, web, roster, platform, competition) fail at the gitops-promote step, and `zitadel` is `OutOfSync/Missing` in ArgoCD.

Root cause: the CI workflows promote to **per-service** gitops paths but the gitops repo only has a **single** folder:
- Workflows (`kongebra-apps/.github/workflows/tronderleikan-*.yml`) use `app: tronderleikan-<svc>` -> gitops-promote does `cd apps/tronderleikan-<svc>/overlays/<env>`.
- `app_dir` (build context) is inconsistent: `apps/tronderleikan/admin` + `apps/tronderleikan/web` (per-service), but `apps/tronderleikan` for roster/platform/competition.
- GitOps has only `apps/tronderleikan/` (base + overlays/dev+prod, with `cluster.yaml` + `databases.yaml` + one ingressroute) - no per-service `apps/tronderleikan-<svc>/` folders. So `cd apps/tronderleikan-admin/overlays/dev` -> "No such file or directory".
- kongebra-apps service dirs: `apps/tronderleikan/{admin,web,roster,platform,competition,apphost,docs,pkg,zitadel-seed}` (a Go workspace, `go.work`).

**Decision needed before implementing** (the layout choice):
- **Option A (golden path):** create per-service gitops folders `apps/tronderleikan-<svc>/base+overlays/{dev,prod}` (each = an ApplicationSet app in namespace `tronderleikan-<svc>-<env>`), matching the CI's per-service `app:` name. Also fix the inconsistent `app_dir` values (roster/platform/competition point at `apps/tronderleikan` root, not their service subdir - likely need `dockerfile:` or per-service dirs). Most work, most correct.
- **Option B:** keep the single `apps/tronderleikan/` app and change the CI to promote all services into it (one overlay pinning multiple images). Less standard for build-once-promote.
- Reconcile with the existing `apps/tronderleikan/` (databases/cluster/ingress) - it may be the shared infra (NATS, CNPG, zitadel) that stays, with per-service app folders added alongside.

Recommend brainstorming the tronderleikan gitops layout (spec) before building, since it's a multi-service structural decision. Zitadel `Missing` likely needs its own look (`bootstrap/zitadel.yaml` + `platform/zitadel/`).

## How to resume
1. Read this file + `kongebra-apps/.superpowers/sdd/progress.md`.
2. `export KUBECONFIG=~/.kube/kongebra-config` for cluster access.
3. For saga: pick follow-up #2 (sub-lang) or confirm a clean summary after IP cooldown.
4. For tronderleikan: get the Option A/B decision, then brainstorm -> spec -> plan -> SDD (same flow used for saga plans 1-3 + v1.1).
