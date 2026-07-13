# LiteLLM Gateway Design

**Date:** 2026-07-13
**Status:** Approved (brainstorm), pending implementation-plan.
**Spans two repos:** `kongebra-gitops` (deploy) and `kongebra-apps` (saga-api refactor).

## Goal

Put a LiteLLM proxy in the k3s cluster as the lab's single OpenAI-compatible LLM endpoint.
It fronts the two tailnet Ollama GPU boxes (round-robin + failover) for local models and Ollama Cloud for cloud models.
`saga-api` stops talking to Ollama directly and consumes LiteLLM instead, keeping its own model catalog/tiers; endpoint routing moves into LiteLLM.

## Why now

The LiteLLM idea was parked in `kongebra-gitops/docs/home-lab.md` with an explicit trigger: "build when the Saga refactor (which picks local-vs-cloud + model) lands."
That refactor landed (Saga Phase 1 tiers + Phase 2a picker).
A second GPU box (`ollama-thinkpad`, tailnet `100.64.150.37:11434`, RTX 3000 Mobile 6GB) now exists alongside the original (`100.125.242.93:11434`, GTX 1660 Ti), benchmarking within +/-8%, so load-balancing across both is worthwhile.
The lab's stated purpose (learning HA, multi-node, observability, operations) makes a proper in-cluster gateway a fitting workload, and future consumers (idea-bank Saga modules, other apps) reuse the one endpoint.

## Non-goals

- No Redis. LiteLLM's in-memory router is sufficient at one replica; Redis is only needed to share router state across multiple proxy replicas, and the cluster is not ready for multi-replica HA (no `topologySpreadConstraints`/PDB HA components yet, per `kongebra-gitops` AGENTS.md).
- No multi-replica HA for the proxy in this iteration.
- No change to saga-api's model catalog contract (tiers, `norwegian` flag, speed/precision meters, `default` flag, replay, prices stay in saga-api - they are product/UX metadata, not routing).
- No Compare view / eval work (that is Saga Phase 2b, separate).

## Architecture

```
apps (saga-api, future)  ── OpenAI /v1 (virtual key) ─►  LiteLLM proxy (1 replica, ns: litellm)
                                                            ├─ CNPG Postgres (1 instance)  = virtual keys, spend, request logs
                                                            ├─ model_list (ConfigMap):
                                                            │    local model  → 2 entries (one per box), simple-shuffle + cooldown failover
                                                            │    cloud model  → Ollama Cloud (Bearer key)
                                                            └─ admin UI + master key (secret)
```

- **Placement:** `kongebra-gitops/platform/litellm/`, own namespace `litellm`, wired from `bootstrap/litellm.yaml` as its own ArgoCD Application (mirrors Zitadel's shape, the direct precedent for a platform service with its own CNPG cluster - ADR 0004/0005 in that repo).
- **Topology:** 1 LiteLLM replica (matches the repo's "replicas:1 unless HA components exist" rule and Zitadel). CNPG Postgres, 1 instance, `enablePDB:false`, pinned PG image, resources set - same pattern as `saga-db`/`status-db`. No Redis.
- **Version pin (hard requirement):** pin the LiteLLM image to a release that includes PR #17191 (fixes Ollama Cloud/Turbo Bearer auth; before it, cloud calls 401). PR merged 2025-12-05. The concrete tag is resolved in Fase A by picking the first stable release at/after that date and verifying a cloud call succeeds; do not use `latest`.
- **Node:** any Hetzner node (pure proxy, no GPU, no home-node affinity).

## Model contract (LiteLLM `model_list`)

Public `model_name` values are the saga-api catalog IDs, unchanged, so the catalog needs no edits:

- **Local models** (`qwen3.5:2b`, `qwen3.5:0.8b`, `qwen3.5:4b`, `gemma4:e4b`, `minicpm5:fp16`): each appears as **two** `model_list` entries sharing one `model_name`, one per box:
  - `litellm_params.model: ollama_chat/<ollama-model-tag>` (NOT `ollama/` - `ollama_chat` uses `/api/chat` with correct chat templating + streaming; `ollama/` uses `/api/generate` and LiteLLM's own docs warn it mishandles chat).
  - `api_base: http://100.125.242.93:11434` and `http://100.64.150.37:11434` respectively.
  - Per-deployment concurrency guard so a single 6GB box is never asked to run two models at once (e.g. `max_parallel_requests: 1` per deployment, or rpm/tpm). This replaces saga-api's old global n=1 chokepoint (see Concurrency change).
- **Cloud models** (`deepseek-v4-flash:cloud`, `gemini-3-flash-preview:cloud`, `kimi-k2.6:cloud`, `kimi-k2.7-code:cloud`, `glm-5.2:cloud`, `minimax-m3:cloud`): one entry each, `litellm_params.model: ollama_chat/<tag>`, `api_base: https://ollama.com`, `api_key` = the Ollama Cloud key (Bearer). The `:cloud` suffix carries no meaning to LiteLLM - it is just the alias; the routing is entirely `api_base`.
- **Router:** `router_settings.routing_strategy: simple-shuffle` (LiteLLM's recommended default: random pick across same-name deployments, automatic cooldown on a failing deployment = failover between the two boxes).

## Auth + secrets

- **Master key** (admin/UI) in a `litellm` namespace secret.
- **Virtual key per consuming app.** `saga-api` gets its own virtual key. Created out-of-band once via the LiteLLM UI/API (it lives in LiteLLM's Postgres), then stored as a k8s secret and consumed by saga-api - the same manually-created-secret convention already used for the `saga-ollama` key and Zitadel's secrets (documented in `kongebra-gitops/SECRETS.md`). Not GitOps-declarative; document it per the onboarding-app pattern.
- **Cloud API key moves out of saga-api** into a LiteLLM secret. saga-api no longer holds `OLLAMA_API_KEY`.

## Networking

- **LiteLLM egress** (NetworkPolicy): the two tailnet boxes `:11434` (`100.125.242.93/32`, `100.64.150.37/32`), Ollama Cloud `:443` (`ollama.com`), its own CNPG `:5432`, DNS. The existing `apps/_components/ollama-egress` component encodes only `.93` - extend it or add the second ipBlock (real work, not a drop-in).
- **LiteLLM ingress:** from consuming app namespaces (initially `saga-api-prod`) to the LiteLLM ClusterIP. No public/tailnet ingress needed unless the admin UI is exposed (optional, via a `*.kongebra.no` IngressRoute if wanted later - not in scope now).
- **saga-api egress change:** currently allows `.93:11434` + `0.0.0.0/0:443` (cloud). After cutover it allows the LiteLLM ClusterIP instead of direct Ollama. Coordinated `kongebra-apps` (env) + `kongebra-gitops` (netpol) change; keep the direct-Ollama rule until Fase B is verified, then remove.

## saga-api refactor (`kongebra-apps`)

The `llm.Provider` interface (`Chat(ctx, model, prompt, opts, onToken) -> ChatResult`) stays; callers (`ytsummary.go`, `server.go`) need no changes. Only the implementation and wiring change.

- **New client:** one OpenAI-compatible streaming client pointing at LiteLLM (base URL + virtual key). Replaces `llm.New` (native `/api/chat`), `llm.NewCloud`, `Router`, and `isCloudModel`.
- **Streaming:** parse SSE `data: {...}` frames, append `choices[0].delta.content` via `onToken`, terminate on the `[DONE]` sentinel. The old `sawDone` gate ("saw a frame with `done:true`") becomes "saw `[DONE]`", independent of whether the usage frame arrived.
- **Token counts:** send `stream_options: {"include_usage": true}`; read `prompt_tokens`/`completion_tokens` from the trailing usage-only frame (empty `choices`) into `InputTokens`/`OutputTokens`.
- **tok/s:** `EvalDuration` stays 0 (no native Ollama timing over OpenAI) -> wall-clock fallback, which the code already does for cloud. Uniform methodology across all models (decision made).
- **ChatOptions:** `Temperature` -> `temperature`, `Seed` -> `seed`. `Think` and `NumCtx` are currently unused (the module sends only Temperature+Seed) - the client does not send them. Guard against reasoning-token leakage: send `think: false` for local Ollama models via `extra_body`, and extend `summarize.CleanMath` to strip `<think>...</think>` blocks (cheap belt-and-suspenders, since some reasoning models inline think-tags in `content`).
- **Config:** add `LITELLM_URL`, `LITELLM_API_KEY`; remove `OLLAMA_URL`, `OLLAMA_CLOUD_URL`, `OLLAMA_API_KEY`.
- **`cloud_enabled`** (drives the frontend Turbo gate, previously `cfg.OllamaAPIKey != ""`): re-source as a boot-time health check - query LiteLLM `/v1/models` at startup and set `cloud_enabled` true when at least one cloud-tier catalog model is present in the returned list. Honest about actual reachability, not a static env that can drift.
- **Tests:** update the five files that construct the native client directly (`main.go` wiring, `internal/llm/router_test.go`, `internal/llm/llm_test.go`, `internal/module/ytsummary/ytsummary_test.go`, `internal/api/server_test.go`) to a `Provider` fake or the OpenAI wire shape. Delete `internal/llm/tier_routing_test.go` - its premise (Router suffix dispatch agrees with catalog tier) disappears with the Router.

## Concurrency change (updates a prior principle)

The old platform principle (OPINIONS.md) was a global n=1 semaphore around all Ollama traffic - one GPU, one call at a time, worker and interactive `/translate` serialized against each other by the `llm.Client`'s own semaphore.

With two boxes fronted by LiteLLM this changes deliberately:
- **Per-box protection moves to LiteLLM** (`max_parallel_requests: 1` per local deployment): each 6GB box still runs one model at a time.
- **Local capacity becomes 2** (both boxes). saga-api's worker local-tier dispatch cap goes from 1 to 2.
- **Interactive `/translate`** no longer needs its own local serialization; it calls LiteLLM, which queues/cools-down per deployment. The worker/translate race the old semaphore guarded is now handled proxy-side.
- Update the OPINIONS.md chokepoint entry to reflect that LiteLLM owns local concurrency, capacity = number of boxes.

## Error handling

- **Box down:** simple-shuffle cooldown routes to the surviving box; a summarize job continues. Both down -> LiteLLM returns an error, saga-api's existing job retry (MaxAttempts=3) applies.
- **Cloud misconfigured/401:** surfaces as a failed job with the LiteLLM error; the version pin (PR #17191) is the guard against the known Bearer-auth 401. `cloud_enabled` health check keeps the UI from advertising Turbo when LiteLLM has no cloud models.
- **LiteLLM down entirely:** saga-api jobs fail and retry; rollback path is repointing saga-api at direct Ollama (Fase B keeps this reversible).

## Testing

- **Token-count PoC (gates the eval-store cutover):** in Fase A, before saga-api depends on LiteLLM's counts, run an explicit streaming call (curl/script) against LiteLLM for a local model and each cloud model with `stream_options.include_usage`, log the raw usage frame, and confirm the counts are genuine provider numbers, not LiteLLM's tokenizer estimate. This is a real risk: Ollama's `prompt_eval_count` can return 0 on cached-context turns, and LiteLLM silently falls back to an internal estimator if the stream ends before the usage frame. If counts are unreliable for a given model, record that model's counts as untrusted rather than corrupting `job_runs` silently.
- **Fase A cluster verification:** `/v1/models` lists all catalog models; a local call round-robins (observe both boxes via `ollama ps`/metrics); a cloud call returns Norwegian output; a box taken down still serves via the other.
- **Fase B (saga-api):** Go unit tests green with the Provider fake; a live E2E (local + cloud summarize through LiteLLM) verifying streaming tokens reach the UI and `job_runs` records sane token counts + wall-clock tok/s; the Turbo gate reflects the `/v1/models` health check.

## Build sequencing (two plans, sequential)

- **Fase A - `kongebra-gitops` (deploy first, verify in isolation):** LiteLLM Deployment (1 replica, pinned image) + CNPG Postgres + `model_list` ConfigMap + master-key/cloud-key secrets + NetworkPolicy (egress to both boxes + cloud + CNPG) + `bootstrap/litellm.yaml` Application. Verify `/v1/models`, round-robin, cloud, failover. Run the token-count PoC. Create the saga-api virtual key + its secret.
- **Fase B - `kongebra-apps` (saga-api) + `kongebra-gitops` (saga-api netpol):** the client refactor, config swap, `cloud_enabled` health check, think-guard, test updates; then the coordinated egress-netpol cutover (LiteLLM in, direct-Ollama out) with rollback = revert saga-api env to direct Ollama.

Each phase produces working, verifiable software on its own and gets its own implementation plan.

## Open items / risks (carried into the plans)

1. **Token-count accuracy** - the PoC in Fase A is the gate; do not let the eval store trust counts until verified per model.
2. **LiteLLM version tag** - resolve the concrete post-2025-12-05 release in Fase A and verify cloud auth.
3. **Reasoning-token leakage** - `think:false` + CleanMath `<think>` strip; verify no think-tags reach summaries.
4. **Second-box egress component** - extend `apps/_components/ollama-egress` (or LiteLLM's own netpol) for `100.64.150.37`.
5. **Virtual-key lifecycle** - manual/out-of-band, documented per SECRETS.md; not declarative.
