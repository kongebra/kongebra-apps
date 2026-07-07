# TronderLeikan Phase 2a - backends live - Implementation Plan

> **STATUS 2026-07-07: DEV DONE + VERIFIED.** All 3 backends Running in `tronderleikan-dev`, `app-tronderleikan-dev` Synced/Healthy, `/api/{platform,roster,competition}` route through Traefik. PROD pending: needs the platform/roster/competition images promoted through the `production` gate (prod overlay currently pins only `web`); a 6h Alertmanager silence is on `tronderleikan-prod` until it deploys.
>
> **Three bugs surfaced during deploy (all fixed):**
> 1. **platform missing `AUTH_ISSUER`** - platform also builds an authn.Validator, not just roster/competition. Fixed: `envFrom tronderleikan-auth` on platform-deployment (gitops).
> 2. **CNPG managed-role reconcile ordering** - the `roster`/`competition` `passwordSecret`s were created AFTER the cluster last reconciled, so the operator left the roles `pending-reconciliation` ("secret not found", stale) and never created the roles or their databases. Fix: restart the `cnpg-cloudnative-pg` operator to force reconcile. **Gotcha: create managed-role passwordSecrets before/around cluster creation, or nudge the operator afterwards.**
> 3. **NATS JetStream stream overlap** - platform created its own `tl-platform` (`tl.platform.>`) stream, overlapping the shared `tl` (`tl.>`) stream that roster/competition ensure; JetStream rejects overlapping subjects, so roster/competition crashed. Fixed in kongebra-apps: platform now joins the shared `tl` stream (commit d313e4e). Convergence needed a manual `nats stream rm tl-platform` after the old image stopped running.
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy `platform`, `roster`, `competition` into `tronderleikan-{dev,prod}` so all three run healthy and serve `leikan.newb.no/api/{platform,roster,competition}`.

**Architecture:** All manifests live in the single `apps/tronderleikan/base/` (Option B, one namespace per env). Each service is a `Deployment` + `Service`. Auth config (`AUTH_ISSUER`, `AUTH_AUDIENCE`) comes from a per-env ConfigMap `tronderleikan-auth`; the audience (Zitadel project ID) is captured by running `zitadel-seed` manually once per env. DB access: platform uses the CNPG-owned `tronderleikan-db-app`; roster/competition use out-of-band `basic-auth` role secrets, DSN assembled via k8s `$(VAR)` env expansion.

**Tech Stack:** Kubernetes, Kustomize (v5.4.3), ArgoCD, CNPG, Traefik IngressRoute, distroless Go services.

## Global Constraints

- Repo language: English for all committed artifacts (manifests, comments, docs).
- Pin everything; images are pinned per overlay by CI (`images:` digest) - never `latest`. Base uses the image name without a tag.
- `replicas: 1` everywhere (repo default); no PDB on single replicas.
- Security posture inherited from `_components/hardened-workload` (container named `app`). Go services use `gcr.io/distroless/static-debian12` WITHOUT `:nonroot` = runs as root -> the base MUST pin `runAsUser`/`runAsGroup` `65532` (component deliberately omits uid).
- Mark deliberate simplifications with a `# ponytail:` comment.
- No `Co-Authored-By` trailer; no em-dash.
- `kubectl` needs `KUBECONFIG=~/.kube/kongebra-config`.

## File structure

- Create: `apps/tronderleikan/base/platform-deployment.yaml`, `platform-service.yaml`
- Create: `apps/tronderleikan/base/roster-deployment.yaml`, `roster-service.yaml`
- Create: `apps/tronderleikan/base/competition-deployment.yaml`, `competition-service.yaml`
- Modify: `apps/tronderleikan/base/kustomization.yaml` (resources + hardened-workload component)
- Create: `apps/tronderleikan/overlays/{dev,prod}/auth-config.yaml` (ConfigMap `tronderleikan-auth`)
- Modify: `apps/tronderleikan/overlays/{dev,prod}/kustomization.yaml` (env component, auth-config, ingressroute)
- Modify: `apps/tronderleikan/overlays/{dev,prod}/ingressroute.yaml` (activate `/api/*` routes)
- Modify: `SECRETS.md` (document the out-of-band secrets + seed run)

All work is in the **kongebra-gitops** repo except the seed run + docs pointer.

---

### Task 1: Out-of-band prerequisites (per env) + document them

This task is manual cluster ops + a docs commit. It MUST land before the manifests (Task 6 deploy) or the pods stay pending on missing secrets. Do it for **dev first** (verify end-to-end), then repeat for prod.

**Files:**
- Modify: `SECRETS.md`

- [ ] **Step 1: Create the DB role secrets (dev)**

Passwords MUST be URL-safe (alphanumeric) - they go into a DSN string, and `/ @ : ?` would break it.

```bash
export KUBECONFIG=~/.kube/kongebra-config
for svc in roster competition; do
  pw="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32)"
  kubectl -n tronderleikan-dev create secret generic "tronderleikan-db-$svc" \
    --type=kubernetes.io/basic-auth \
    --from-literal=username="$svc" --from-literal=password="$pw"
  echo "$svc dev password (store in 1Password): $pw"
done
```

Store both passwords in 1Password. CNPG reads these to set the managed-role passwords (per `base/cluster.yaml` `managed.roles[].passwordSecret`).

- [ ] **Step 2: Reflect `iam-admin-pat` into the tronderleikan namespace (dev)**

platform consumes it as `ZITADEL_PAT`. It is operator-created in ns `zitadel`; annotate it for reflector (same mechanism as `ghcr-pull`).

```bash
kubectl -n zitadel annotate secret iam-admin-pat \
  reflector.v1.k8s.emberstack.com/reflection-allowed="true" \
  reflector.v1.k8s.emberstack.com/reflection-allowed-namespaces="tronderleikan-dev,tronderleikan-prod" \
  reflector.v1.k8s.emberstack.com/reflection-auto-enabled="true" \
  reflector.v1.k8s.emberstack.com/reflection-auto-namespaces="tronderleikan-dev,tronderleikan-prod" --overwrite
```

Verify it lands: `kubectl -n tronderleikan-dev get secret iam-admin-pat` (may take a few seconds).
`# ponytail:` a re-seed of Zitadel recreates `iam-admin-pat` WITHOUT this annotation - re-run this step after any Zitadel re-init.

- [ ] **Step 3: Run zitadel-seed once (dev) and capture the project ID**

```bash
cd ~/github/kongebra/kongebra-apps/apps/tronderleikan/zitadel-seed
ZITADEL_API_URL=https://auth.newb.no \
ZITADEL_PAT="$(KUBECONFIG=~/.kube/kongebra-config kubectl -n zitadel get secret iam-admin-pat -o jsonpath='{.data.pat}' | base64 -d)" \
  go run .
```

Expected: it prints/logs the created project ID (org "TronderLeikan Platform", project "tronderleikan", roles). Record the **project ID** - it is `AUTH_AUDIENCE` for this env. Idempotent: safe to re-run.

- [ ] **Step 4: Document the out-of-band steps in SECRETS.md and commit**

Add a `## TronderLeikan backend secrets` section covering: the two `tronderleikan-db-<svc>` basic-auth secrets (URL-safe passwords, per env, in 1Password), the `iam-admin-pat` reflection annotation (+ the re-seed caveat), and the manual `zitadel-seed` run to capture `AUTH_AUDIENCE`.

```bash
cd ~/github/kongebra/kongebra-gitops
git add SECRETS.md
git commit -m "docs(tronderleikan): backend out-of-band secrets + seed capture (Phase 2a)"
```

---

### Task 2: platform Deployment + Service

**Files:**
- Create: `apps/tronderleikan/base/platform-deployment.yaml`
- Create: `apps/tronderleikan/base/platform-service.yaml`
- Modify: `apps/tronderleikan/base/kustomization.yaml`

**Interfaces:**
- Produces: Service `platform` (port 8080) - consumed by admin (2b) and as `/api/platform` backend.
- Consumes: secret `tronderleikan-db-app` (key `uri`), secret `iam-admin-pat` (key `pat`), service `nats` (4222), Zitadel at `https://auth.newb.no`.

- [ ] **Step 1: Write `platform-deployment.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: platform
spec:
  replicas: 1
  selector:
    matchLabels:
      app: platform
  template:
    metadata:
      labels:
        app: platform
    spec:
      # distroless/static-debian12 has no USER (runs as root) -> pin uid (hardened-workload
      # sets runAsNonRoot but deliberately not the uid). 65532 = distroless convention.
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
      containers:
        - name: app
          image: ghcr.io/kongebra/tronderleikan-platform
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: PORT
              value: "8080"
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-db-app
                  key: uri
            - name: NATS_URL
              value: nats://nats:4222
            - name: ZITADEL_API_URL
              value: https://auth.newb.no
            - name: ZITADEL_PAT
              valueFrom:
                secretKeyRef:
                  name: iam-admin-pat
                  key: pat
          livenessProbe:
            exec:
              command: ["/app", "-health"]
            initialDelaySeconds: 10
            periodSeconds: 15
          readinessProbe:
            exec:
              command: ["/app", "-health"]
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              memory: 192Mi
```

- [ ] **Step 2: Write `platform-service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: platform
spec:
  selector:
    app: platform
  ports:
    - name: http
      port: 8080
      targetPort: http
```

- [ ] **Step 3: Add to `base/kustomization.yaml`**

Add the two files to `resources:` and add the hardened-workload component. The base `components:` currently has `stateful` + `limitrange` with a comment "ADD hardened-workload TOGETHER WITH THE FIRST Deployment" - now is that time.

```yaml
resources:
  - cluster.yaml
  - databases.yaml
  - platform-deployment.yaml
  - platform-service.yaml
components:
  - ../../_components/hardened-workload
  - ../../_components/stateful
  - ../../_components/limitrange
```

- [ ] **Step 4: Verify the overlay still builds**

```bash
cd ~/github/kongebra/kongebra-gitops
kustomize build apps/tronderleikan/overlays/dev >/dev/null && echo "dev build OK"
kustomize build apps/tronderleikan/overlays/prod >/dev/null && echo "prod build OK"
```
Expected: both print OK. (hardened-workload's patch now has the `platform` Deployment to target - the build no longer fails on an empty patch match.)

- [ ] **Step 5: Commit**

```bash
git add apps/tronderleikan/base/platform-deployment.yaml apps/tronderleikan/base/platform-service.yaml apps/tronderleikan/base/kustomization.yaml
git commit -m "feat(tronderleikan): platform Deployment + Service (Phase 2a)"
```

---

### Task 3: roster Deployment + Service

**Files:**
- Create: `apps/tronderleikan/base/roster-deployment.yaml`
- Create: `apps/tronderleikan/base/roster-service.yaml`
- Modify: `apps/tronderleikan/base/kustomization.yaml`

**Interfaces:**
- Produces: Service `roster` (port 8080) - consumed by competition (`ROSTER_URL`) and web (2b).
- Consumes: secret `tronderleikan-db-roster` (keys `username`/`password`), ConfigMap `tronderleikan-auth` (`AUTH_ISSUER`, `AUTH_AUDIENCE`), service `nats`.

- [ ] **Step 1: Write `roster-deployment.yaml`**

`DATABASE_URL` is assembled from the role secret via k8s `$(VAR)` expansion - `DB_USER`/`DB_PASS` are declared before `DATABASE_URL` so kustomize/kubelet expands them. `AUTH_ISSUER`/`AUTH_AUDIENCE` arrive via `envFrom` the `tronderleikan-auth` ConfigMap.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: roster
spec:
  replicas: 1
  selector:
    matchLabels:
      app: roster
  template:
    metadata:
      labels:
        app: roster
    spec:
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
      containers:
        - name: app
          image: ghcr.io/kongebra/tronderleikan-roster
          ports:
            - name: http
              containerPort: 8080
          envFrom:
            - configMapRef:
                name: tronderleikan-auth
          env:
            - name: PORT
              value: "8080"
            - name: DB_USER
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-db-roster
                  key: username
            - name: DB_PASS
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-db-roster
                  key: password
            - name: DATABASE_URL
              value: "postgresql://$(DB_USER):$(DB_PASS)@tronderleikan-db-rw:5432/roster?sslmode=require"
            - name: NATS_URL
              value: nats://nats:4222
          livenessProbe:
            exec:
              command: ["/app", "-health"]
            initialDelaySeconds: 10
            periodSeconds: 15
          readinessProbe:
            exec:
              command: ["/app", "-health"]
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              memory: 192Mi
```

- [ ] **Step 2: Write `roster-service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: roster
spec:
  selector:
    app: roster
  ports:
    - name: http
      port: 8080
      targetPort: http
```

- [ ] **Step 3: Add both files to `base/kustomization.yaml` `resources:`** (after the platform entries).

- [ ] **Step 4: Verify build**

```bash
kustomize build apps/tronderleikan/overlays/dev >/dev/null && echo OK
```
Expected: OK. (The `tronderleikan-auth` ConfigMap does not exist yet in the base; `envFrom` a not-yet-defined ConfigMap is valid at build time - it is added per overlay in Task 5. kustomize does not fail on a dangling configMapRef.)

- [ ] **Step 5: Commit**

```bash
git add apps/tronderleikan/base/roster-deployment.yaml apps/tronderleikan/base/roster-service.yaml apps/tronderleikan/base/kustomization.yaml
git commit -m "feat(tronderleikan): roster Deployment + Service (Phase 2a)"
```

---

### Task 4: competition Deployment + Service

**Files:**
- Create: `apps/tronderleikan/base/competition-deployment.yaml`
- Create: `apps/tronderleikan/base/competition-service.yaml`
- Modify: `apps/tronderleikan/base/kustomization.yaml`

**Interfaces:**
- Produces: Service `competition` (port 8080) - `/api/competition` backend.
- Consumes: secret `tronderleikan-db-competition`, ConfigMap `tronderleikan-auth`, service `nats`, service `roster` (`ROSTER_URL`).

- [ ] **Step 1: Write `competition-deployment.yaml`** - identical to roster except `name: competition`, `app: competition`, db secret `tronderleikan-db-competition`, db name `competition`, and one extra env `ROSTER_URL`.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: competition
spec:
  replicas: 1
  selector:
    matchLabels:
      app: competition
  template:
    metadata:
      labels:
        app: competition
    spec:
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
      containers:
        - name: app
          image: ghcr.io/kongebra/tronderleikan-competition
          ports:
            - name: http
              containerPort: 8080
          envFrom:
            - configMapRef:
                name: tronderleikan-auth
          env:
            - name: PORT
              value: "8080"
            - name: DB_USER
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-db-competition
                  key: username
            - name: DB_PASS
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-db-competition
                  key: password
            - name: DATABASE_URL
              value: "postgresql://$(DB_USER):$(DB_PASS)@tronderleikan-db-rw:5432/competition?sslmode=require"
            - name: NATS_URL
              value: nats://nats:4222
            - name: ROSTER_URL
              value: http://roster:8080
          livenessProbe:
            exec:
              command: ["/app", "-health"]
            initialDelaySeconds: 10
            periodSeconds: 15
          readinessProbe:
            exec:
              command: ["/app", "-health"]
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              memory: 192Mi
```

- [ ] **Step 2: Write `competition-service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: competition
spec:
  selector:
    app: competition
  ports:
    - name: http
      port: 8080
      targetPort: http
```

- [ ] **Step 3: Add both files to `base/kustomization.yaml` `resources:`.**

- [ ] **Step 4: Verify build** - `kustomize build apps/tronderleikan/overlays/dev >/dev/null && echo OK`

- [ ] **Step 5: Commit**

```bash
git add apps/tronderleikan/base/competition-deployment.yaml apps/tronderleikan/base/competition-service.yaml apps/tronderleikan/base/kustomization.yaml
git commit -m "feat(tronderleikan): competition Deployment + Service (Phase 2a)"
```

---

### Task 5: Overlay wiring - auth ConfigMap, env component, ingressroute

Do dev first, then mirror to prod (with the prod project ID). `AUTH_AUDIENCE` values differ per env (separate Zitadel projects from separate seed runs), so the ConfigMap lives in each overlay, not the base.

**Files:**
- Create: `apps/tronderleikan/overlays/dev/auth-config.yaml` (and `prod/`)
- Modify: `apps/tronderleikan/overlays/dev/kustomization.yaml` (and `prod/`)
- Modify: `apps/tronderleikan/overlays/dev/ingressroute.yaml` (and `prod/`)

**Interfaces:**
- Produces: ConfigMap `tronderleikan-auth` (`AUTH_ISSUER`, `AUTH_AUDIENCE`) consumed by roster/competition `envFrom`.

- [ ] **Step 1: Write `overlays/dev/auth-config.yaml`** - use the dev project ID captured in Task 1 Step 3.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tronderleikan-auth
data:
  AUTH_ISSUER: https://auth.newb.no
  # AUTH_AUDIENCE = Zitadel project ID captured from the dev zitadel-seed run (Task 1).
  # Re-seeding a fresh Zitadel changes this - update here + re-sync.
  AUTH_AUDIENCE: "REPLACE_WITH_DEV_PROJECT_ID"
```
(The executor substitutes the real captured ID. This is the ONE value that is env-specific runtime data, not a design placeholder - it cannot be known until the seed runs.)

- [ ] **Step 2: Activate the `/api/*` routes in `overlays/dev/ingressroute.yaml`**

Replace the commented block so the file has exactly these three routes (the web catch-all stays commented - web ships in 2b; a route to a missing `web` service makes Traefik log errors):

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: tronderleikan
  annotations:
    gatus.home-operations.com/enabled-dev: "true"
spec:
  entryPoints: [websecure]
  routes:
    - match: Host(`leikan-dev.newb.no`) && PathPrefix(`/api/platform`)
      kind: Rule
      services:
        - name: platform
          port: 8080
    - match: Host(`leikan-dev.newb.no`) && PathPrefix(`/api/roster`)
      kind: Rule
      services:
        - name: roster
          port: 8080
    - match: Host(`leikan-dev.newb.no`) && PathPrefix(`/api/competition`)
      kind: Rule
      services:
        - name: competition
          port: 8080
  tls: {}
```
(dev host = `leikan-dev.newb.no`; the gatus key is the dev variant `enabled-dev`. prod uses `leikan.newb.no` + the plain `gatus.home-operations.com/enabled`.)

- [ ] **Step 3: Wire `overlays/dev/kustomization.yaml`**

Add `auth-config.yaml` and `ingressroute.yaml` to `resources:`, and the `env-dev` component:

```yaml
resources:
  - ../../base
  - auth-config.yaml
  - ingressroute.yaml
labels:
  - pairs:
      env: dev
components:
  - ../../../_components/env-dev
```
`# ponytail:` `env-dev` appends `OTEL_RESOURCE_ATTRIBUTES` to `containers/0/env` - all three base Deployments define an `env:` array, so the JSON-patch path resolves. If a future service omits `env:`, that patch fails.

- [ ] **Step 4: Verify build shows the wired env + configmap**

```bash
kustomize build apps/tronderleikan/overlays/dev | grep -E "AUTH_AUDIENCE|OTEL_RESOURCE|name: platform|name: roster|name: competition" | head
```
Expected: shows the ConfigMap `AUTH_AUDIENCE`, the appended `OTEL_RESOURCE_ATTRIBUTES`, and the three Services.

- [ ] **Step 5: Commit dev overlay**

```bash
git add apps/tronderleikan/overlays/dev/
git commit -m "feat(tronderleikan): dev overlay - auth config, env tag, /api routes (Phase 2a)"
```

- [ ] **Step 6: Mirror to prod** - repeat Steps 1-5 for `overlays/prod/` using the **prod** project ID, host `leikan.newb.no`, gatus key `gatus.home-operations.com/enabled`, and `env-prod`. Requires Task 1 to have been run for prod first. Commit: `feat(tronderleikan): prod overlay - auth config, env tag, /api routes (Phase 2a)`.

---

### Task 6: Deploy + verify (dev, then prod)

**Files:** none (push + observe).

- [ ] **Step 1: Push (Svein pushes - classifier blocks agent main-push)**

```bash
cd ~/github/kongebra/kongebra-gitops && git push origin main
```
ArgoCD picks up `app-tronderleikan-dev` within ~3 min (or force: `kubectl -n argocd annotate app app-tronderleikan-dev argocd.argoproj.io/refresh=hard --overwrite`).

- [ ] **Step 2: Verify pods reach Running (dev)**

```bash
export KUBECONFIG=~/.kube/kongebra-config
kubectl -n tronderleikan-dev get pods -l 'app in (platform,roster,competition)'
```
Expected: 3 pods `Running` 1/1. If `CreateContainerConfigError` -> uid not pinned or a secret missing (Task 1). If `CrashLoopBackOff` -> check logs: `kubectl -n tronderleikan-dev logs deploy/<svc>` (likely a missing/invalid env - DB DSN, AUTH_AUDIENCE, or issuer unreachable).

- [ ] **Step 3: Verify ArgoCD health + probes**

```bash
kubectl -n argocd get app app-tronderleikan-dev -o jsonpath='sync={.status.sync.status} health={.status.health.status}{"\n"}'
```
Expected: `Synced/Healthy`.

- [ ] **Step 4: Verify the API is routable**

```bash
curl -sS -o /dev/null -w "platform=%{http_code}\n" --max-time 8 https://leikan-dev.newb.no/api/platform/healthz || true
```
Expected: a response from platform (200, or whatever `/api/platform/healthz` returns - a non-connection-refused HTTP code proves the route + service + pod path works).

- [ ] **Step 5: Verify JWT audience validation**

Mint a token for the seeded project (or reuse one from a login) and confirm roster accepts the correct `aud` and rejects a wrong one. Minimal check: an unauthenticated request to a protected roster route returns 401 (proves the validator booted, which itself proves `AUTH_ISSUER`+`AUTH_AUDIENCE` resolved at startup).

```bash
curl -sS -o /dev/null -w "roster-noauth=%{http_code}\n" --max-time 8 https://leikan-dev.newb.no/api/roster/ || true
```
Expected: `401` (or `403`) - NOT `502`/`503` (which would mean the pod never became ready).

- [ ] **Step 6: Repeat Steps 1-5 against prod** once dev is green (prod overlay from Task 5 Step 6, prod secrets from Task 1). Prod deploy is behind the `production` environment gate only for the CI image promote - these manifest changes sync via ArgoCD directly on push, no gate.

---

## Self-review notes

- **Spec coverage:** platform/roster/competition Deployments+Services (Tasks 2-4), hardened-workload+uid pin (all), audience ConfigMap + manual seed capture (Tasks 1,5), reflected `iam-admin-pat` (Task 1), DB role secrets + `$(VAR)` DSN (Tasks 1,3,4), ingressroute activation minus web catch-all (Task 5), verification incl. audience (Task 6). All spec sections mapped.
- **The one `REPLACE_WITH_*_PROJECT_ID`** is runtime data (the seed-generated ID), not a design gap - it is impossible to know before Task 1 runs, and Task 1 Step 3 captures it.
- **Deferred (2b / later):** web/admin, OIDC apps, SESSION_SECRET, prod dev-health precheck, CNPG HA/backup.
