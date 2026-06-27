# kongebra-apps monorepo

Self-hosted apps for the kongebra lab.
Apps deploy to a private 3-node k3s cluster (Hetzner, behind Tailscale) via GitOps.
This is a lab for learning HA, multi-node, observability and operations - but the code must hold production quality.

Deploy manifests live in the **kongebra-gitops** repo (ArgoCD app-of-apps).
This repo holds app code + CI that builds images to GHCR; ArgoCD pulls and deploys them.

## Structure

```
apps/<name>/            # one app per folder, own go.mod (multi-module monorepo)
.github/
  actions/
    docker-build/       # composite: build image + optional GHCR push (docker-generic)
    gitops-promote/     # composite: write image tag into kongebra-gitops overlay
  workflows/
    _build-deploy.yml   # reusable: build -> push GHCR -> promote dev -> promote prod (gated)
    <app>.yml           # per-app caller (push to main), path-filtered, calls _build-deploy
    <app>-pr.yml        # per-app PR check (pull_request): tests + build-only, no push
```

New app: create `apps/<name>/`, copy an existing `<app>.yml` + `<app>-pr.yml` and change `app`/`app_dir`/`image` (+ path filters).
Then add the deploy manifests in `kongebra-gitops/apps/<name>/` (see that repo's AGENTS.md).

## Tech choices

- **Backend: Go primarily.** Python only when a library is clearly much better for the task (microservice then).
- **Frontend: TanStack Start**, or plain Vite + React if the need is trivial. **ALWAYS confirm the frontend framework with Svein before choosing** - do not assume.
- **Image: distroless** (`gcr.io/distroless/static-debian12`) for Go. No shell - health checks must be built-in binary flags, not `curl`/`CMD-SHELL`.
- Each Go app is its own module. No shared root `go.mod` unless we deliberately introduce shared packages.

## CI/CD (build once, deploy via GitOps)

- Build happens in **GitHub Actions**, not on a deploy server. Images are pushed to **GHCR**.
- Two tags per image: `:<full-sha>`, `:<YYYY-MM-DD-shortsha>` (readable). No `:latest` (the overlay pins the tag; `:latest` would break build-once-promote traceability).
- Deploy is **CI-push**: on push to main, CI builds one immutable tag, writes it into the dev overlay in `kongebra-gitops` (auto), then promotes the *same* tag into the prod overlay behind a **GitHub Environment `production` required-reviewer gate**. ArgoCD syncs. (Not Image Updater - CI owns the tag now.)
- **Rollback/promote:** `workflow_dispatch` with input `image_tag=<existing-tag>` re-promotes that tag without rebuilding (instant; image already in GHCR).
- Pin all third-party actions to a full SHA (comment with version).
- **build-once-promote** (dev->prod): the same image is promoted between environments, never rebuilt per environment. ADR-0001.
- **Composable actions:** `docker-build` (build + optional push, docker-generic) and `gitops-promote` (tag -> overlay) are the reusable blocks; `_build-deploy.yml` orchestrates them.

### PR checks (`<app>-pr.yml`) - security rules every app MUST follow

- Trigger on **`pull_request`** only - **NEVER `pull_request_target`** (it runs with repo secrets in the base context; a forked PR could exfiltrate them).
- `permissions: contents: read` only. **No `secrets: inherit`**, no GHCR/gitops credentials.
- Build with `docker-build` `push: false` (verify the Dockerfile builds; never pushes).
- **Tests are per-app and language-specific** -> they live in `<app>-pr.yml`, NOT in `docker-build` (which stays docker-generic, must not know Go/TS/Python).

## Conventions

- Code identifiers in English. URL paths in English (eases i18n/deploy). Comments may be Norwegian.
- No `Co-Authored-By` trailer in commits.
- No em-dash in text; use a plain hyphen.
- Mark deliberate simplifications with a `// ponytail:` comment (what + upgrade path).

## Local development

- **Aspire (v13, polyglot)** for local orchestration. AppHost authored in TypeScript (not C#). `aspire run` gives containerized local dev + a built-in OTEL dashboard. The AppHost declares app + deps (Postgres/Redis) + relations.
- Status: **spike phase** - Go is Aspire's least mature integration (community-toolkit/code-gen), so validate the pattern before it becomes standard for all apps.
- Aspire owns local dev, not prod deploy (prod goes via CI -> GHCR -> ArgoCD). Optionally emit a compose artifact as a bridge.
- **Local Grafana mirroring:** the AppHost includes a `grafana/otel-lgtm` container locally (same stack as the in-cluster `otel-lgtm`), so Grafana/Tempo/Prometheus skills are learned locally with full prod parity. The app sends OTLP there (`OTEL_EXPORTER_OTLP_ENDPOINT` -> local otel-lgtm).

## Infrastructure (context)

- Deploy target: 3-node k3s HA cluster (3x Hetzner CX33, all control-plane+etcd, Tailscale tailnet `tail63f312.ts.net`). Manifests + full state in **kongebra-gitops** (`HANDOFF.md`).
- Apps are served on `*.newb.no` (wildcard -> cluster node tailnet IPs, tailnet-only). TLS via cert-manager wildcard cert + Traefik default TLSStore.
- Observability: in-cluster `grafana/otel-lgtm` (`grafana.newb.no`). Apps send OTLP to `otel-lgtm.observability:4318`.
- Hetzner Cloud Firewall `tailnet-only` (deny-all inbound except ICMP+UDP41641).

## Gotchas (learned the hard way)

- **Let's Encrypt HTTP-01 will not work on a tailnet-hidden box** - LE needs public port 80 (firewall-blocked). Use DNS-01 (Cloudflare) for certs.
- **Fine-grained PATs do not support GHCR** - use a classic PAT with `read:packages` for image pulls.
- **Replicas != HA** without controlled placement - on k8s rely on the 3-node etcd quorum.

## Agent skills

### Issue tracker

Issues are tracked as GitHub issues (`kongebra/kongebra-apps`) via the `gh` CLI. External PRs are not a triage surface. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root (neither exists yet; created lazily). See `docs/agents/domain.md`.
