# Kongebra k3s-lab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stå opp et 3-node k3s HA-cluster på eksisterende Hetzner Ubuntu-bokser med ArgoCD GitOps, OTEL-eksport til eksisterende otel-lgtm, og en mappe-basert flyt for å deploye nye apper fra GHCR.

**Architecture:** k3s 3x server (embedded etcd HA) på Ubuntu, nådd via Tailscale. ArgoCD app-of-apps + ApplicationSet (Git directory-generator) deployer alt deklarativt fra `kongebra-gitops`. Apper bygges i `kongebra-apps` → GHCR, deployes via gitops-mappe + Image Updater. Telemetri går via OTel Collector til eksisterende otel-lgtm over tailnet.

**Tech Stack:** k3s, Tailscale, Traefik (k3s-bundled), cert-manager + Cloudflare DNS-01, ArgoCD + ApplicationSet + Image Updater, OpenTelemetry Collector, GHCR, kustomize.

## Global Constraints

- Maskinvare: 3x Hetzner CX23 (2 vCPU / 4GB / 40GB, x86, Helsinki). `kongebra-k8s-1`=89.167.125.132, `kongebra-k8s-2`=204.168.243.124, `kongebra-k8s-3`=204.168.206.78.
- 4GB/node er hardt tak. `GOMEMLIMIT` + eviction-thresholds på k3s er obligatorisk, ikke valgfritt.
- GHCR-pull krever **classic PAT med `read:packages`** (fine-grained PAT funker ikke mot GHCR).
- TLS via **Cloudflare DNS-01**, aldri HTTP-01 (noder er bak Tailscale/firewall).
- kube-apiserver eksponeres aldri offentlig. All tilgang via tailnet (`tag:k8s`).
- Kode-identifikatorer og URL-paths på engelsk. Kommentarer kan være norsk. Ingen em-dash. Ingen `Co-Authored-By`-trailer.
- Pin alle tredjeparts-versjoner (Helm charts, manifest-URLer) til eksakt versjon, ikke `latest`/`stable`.
- Cloudflare-nameservere ble byttet 2026-06-26 for newb.no + kongebra.net. DNS-01 virker først når propagering er fullført (verifiser før Task 5).

---

### Task 1: Bootstrap repos (kongebra-apps rename + kongebra-gitops skjelett)

**Files:**
- Rename: GitHub-repo `kongebra/newb.no` → `kongebra/kongebra-apps`
- Create: `kongebra-gitops/README.md`
- Create: `kongebra-gitops/bootstrap/root-app.yaml`
- Create: `kongebra-gitops/apps/.gitkeep`
- Create: `kongebra-gitops/platform/.gitkeep`

**Interfaces:**
- Produces: gitops-repo med to scanbare mapper - `platform/` (infra-komponenter som Argo Applications) og `apps/` (workloads, plukket av ApplicationSet i Task 8).

- [ ] **Step 1: Rename apps-monorepoet**

```bash
gh repo rename kongebra-apps --repo kongebra/newb.no
# oppdater lokal remote
git -C /Users/svedanie/newb.no remote set-url origin https://github.com/kongebra/kongebra-apps.git
```

- [ ] **Step 2: Verifiser rename + redirect**

Run: `gh repo view kongebra/kongebra-apps --json nameWithOwner -q .nameWithOwner`
Expected: `kongebra/kongebra-apps`

- [ ] **Step 3: Opprett gitops-repoet lokalt**

```bash
mkdir -p /Users/svedanie/kongebra-gitops/{bootstrap,apps,platform}
cd /Users/svedanie/kongebra-gitops
git init -b main
touch apps/.gitkeep platform/.gitkeep
printf '# kongebra-gitops\n\nArgoCD app-of-apps for kongebra k3s-lab. `platform/` = infra-komponenter, `apps/` = workloads.\n' > README.md
```

- [ ] **Step 4: Skriv root app-of-apps (peker på platform/ og apps/ via egne ApplicationSets, fylles i Task 6/8)**

`kongebra-gitops/bootstrap/root-app.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: root
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/kongebra/kongebra-gitops.git
    targetRevision: main
    path: bootstrap
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated: { prune: true, selfHeal: true }
```

- [ ] **Step 5: Push gitops-repoet til GitHub**

```bash
cd /Users/svedanie/kongebra-gitops
gh repo create kongebra/kongebra-gitops --private --source=. --remote=origin --push
```

Run: `gh repo view kongebra/kongebra-gitops --json nameWithOwner -q .nameWithOwner`
Expected: `kongebra/kongebra-gitops`

---

### Task 2: Tailscale på alle 3 noder

**Files:**
- Modify: Tailscale ACL (admin-konsoll) - legg til `tag:k8s`

**Interfaces:**
- Produces: alle 3 noder på tailnet med MagicDNS-navn `kongebra-k8s-1/2/3`, tagget `tag:k8s`. Senere tasks når nodene via disse navnene.

- [ ] **Step 1: Lag en reusable, pre-authorized auth key med tag:k8s**

I Tailscale admin → Settings → Keys → Generate auth key: Reusable, Ephemeral=off, Tags=`tag:k8s`. Kopiér nøkkelen (`tskey-auth-...`).

Sikre at ACL har tag-eier (admin-konsoll → Access controls):
```jsonc
"tagOwners": { "tag:k8s": ["autogroup:admin"] }
```

- [ ] **Step 2: Installer Tailscale på hver node**

For hver IP (89.167.125.132, 204.168.243.124, 204.168.206.78):
```bash
ssh root@<NODE_IP> 'curl -fsSL https://tailscale.com/install.sh | sh && \
  tailscale up --authkey=tskey-auth-XXXX --hostname=$(hostname) --accept-routes'
```
(Sett `--hostname=kongebra-k8s-1/2/3` eksplisitt hvis `hostname` ikke alt er det.)

- [ ] **Step 3: Verifiser at alle 3 er på tailnet**

Run: `tailscale status | grep kongebra-k8s`
Expected: tre linjer, `kongebra-k8s-1`, `-2`, `-3`, hver med en `100.x.y.z`-adresse og `tag:k8s`.

- [ ] **Step 4: Verifiser MagicDNS-navngiving fungerer**

Run: `tailscale ping kongebra-k8s-1`
Expected: `pong from kongebra-k8s-1 ... via DERP/direct`

---

### Task 3: k3s HA install (3x server, embedded etcd)

**Files:**
- Create (på hver node): `/etc/rancher/k3s/config.yaml`
- Create (systemd drop-in, hver node): `/etc/systemd/system/k3s.service.d/override.conf` (GOMEMLIMIT)

**Interfaces:**
- Consumes: tailnet fra Task 2 (bruker node-1 tailnet-IP som join-endpoint).
- Produces: et 3-node HA-cluster. Node-token i `/var/lib/rancher/k3s/server/node-token` på node 1.

- [ ] **Step 1: Sett GOMEMLIMIT-override på node 1 (mot etcd-OOM på 4GB)**

På `kongebra-k8s-1`:
```bash
mkdir -p /etc/systemd/system/k3s.service.d
cat >/etc/systemd/system/k3s.service.d/override.conf <<'EOF'
[Service]
Environment=GOMEMLIMIT=2750MiB
EOF
```

- [ ] **Step 2: Init HA-clusteret på node 1**

På `kongebra-k8s-1` (bruk nodens egen tailnet-IP som `<TS_IP_1>`):
```bash
cat >/etc/rancher/k3s/config.yaml <<EOF
cluster-init: true
node-ip: <TS_IP_1>
advertise-address: <TS_IP_1>
tls-san:
  - kongebra-k8s-1
kubelet-arg:
  - "system-reserved=cpu=250m,memory=256Mi"
  - "kube-reserved=cpu=250m,memory=256Mi"
  - "eviction-hard=memory.available<256Mi"
EOF
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.32.5+k3s1 sh -
```

- [ ] **Step 3: Verifiser node 1 er oppe + hent token**

Run: `ssh root@89.167.125.132 'k3s kubectl get nodes && cat /var/lib/rancher/k3s/server/node-token'`
Expected: én node `Ready` med rolle `control-plane,etcd,master`, + en token-streng `K10...::server:...`.

- [ ] **Step 4: Join node 2 og 3 som servere**

På `kongebra-k8s-2` og `kongebra-k8s-3` (egen tailnet-IP som `<TS_IP_N>`, samme GOMEMLIMIT-override som Step 1):
```bash
mkdir -p /etc/systemd/system/k3s.service.d
printf '[Service]\nEnvironment=GOMEMLIMIT=2750MiB\n' >/etc/systemd/system/k3s.service.d/override.conf
cat >/etc/rancher/k3s/config.yaml <<EOF
server: https://kongebra-k8s-1:6443
token: <NODE_TOKEN_FRA_STEP_3>
node-ip: <TS_IP_N>
kubelet-arg:
  - "system-reserved=cpu=250m,memory=256Mi"
  - "kube-reserved=cpu=250m,memory=256Mi"
  - "eviction-hard=memory.available<256Mi"
EOF
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.32.5+k3s1 sh -
```

- [ ] **Step 5: Verifiser HA-quorum (3 noder, 3 etcd-members)**

Run: `ssh root@89.167.125.132 'k3s kubectl get nodes -o wide'`
Expected: tre noder, alle `Ready`, alle med rolle `control-plane,etcd,master`.

- [ ] **Step 6: Verifiser etcd faktisk tåler node-tap (HA-bevis)**

Run: `ssh root@204.168.206.78 'systemctl stop k3s' && sleep 20 && ssh root@89.167.125.132 'k3s kubectl get nodes'`
Expected: `kongebra-k8s-3` blir `NotReady`, men `kubectl` svarer fortsatt (quorum 2/3 holder). Start igjen: `ssh root@204.168.206.78 'systemctl start k3s'`.

---

### Task 4: Remote kubectl over tailnet

**Files:**
- Create (lokalt): `~/.kube/kongebra-config`

**Interfaces:**
- Consumes: `tls-san: kongebra-k8s-1` fra Task 3 (gjør at sertifikatet er gyldig for det navnet).
- Produces: lokal `kubectl`-tilgang. Brukes av alle senere tasks.

- [ ] **Step 1: Hent kubeconfig og pek server på MagicDNS-navnet**

```bash
ssh root@89.167.125.132 'cat /etc/rancher/k3s/k3s.yaml' \
  | sed 's#https://127.0.0.1:6443#https://kongebra-k8s-1:6443#' \
  > ~/.kube/kongebra-config
chmod 600 ~/.kube/kongebra-config
```

- [ ] **Step 2: Verifiser lokal tilgang over tailnet**

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl get nodes`
Expected: tre noder `Ready`. (Hvis TLS-feil: bekreft at du er på tailnet og at `kongebra-k8s-1` resolver via MagicDNS.)

---

### Task 5: cert-manager + Cloudflare DNS-01

**Files:**
- Create: `kongebra-gitops/platform/cert-manager/application.yaml`
- Create: `kongebra-gitops/platform/cert-manager/cloudflare-issuer.yaml`
- Create: `kongebra-gitops/platform/cert-manager/wildcard-cert.yaml`

**Interfaces:**
- Consumes: Cloudflare-zone for kongebra.net (NS byttet, må være propagert).
- Produces: ClusterIssuer `cloudflare-dns01` + et wildcard-Secret `kongebra-net-tls` i `kube-system`/`traefik` for `*.kongebra.net`. Traefik (Task 10) konsumerer dette.

- [ ] **Step 1: Verifiser at Cloudflare faktisk styrer sona (propagering ferdig)**

Run: `dig NS kongebra.net +short`
Expected: to `*.ns.cloudflare.com`-servere. Hvis ikke - vent på propagering før du fortsetter.

- [ ] **Step 2: Lag Cloudflare API-token + secret**

Cloudflare dashboard → My Profile → API Tokens → Create → template "Edit zone DNS", scope til zone `kongebra.net`. Deretter:
```bash
KUBECONFIG=~/.kube/kongebra-config kubectl create namespace cert-manager
KUBECONFIG=~/.kube/kongebra-config kubectl -n cert-manager create secret generic cloudflare-api-token \
  --from-literal=api-token=<CF_TOKEN>
```
`# ponytail: token håndteres manuelt i v1; Sealed Secrets/ESO er dag-2-oppgradering`

- [ ] **Step 3: Installer cert-manager (pinnet Helm-chart) via direkte apply**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f \
  https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
```

- [ ] **Step 4: Verifiser cert-manager kjører**

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=120s`
Expected: `deployment "cert-manager-webhook" successfully rolled out`

- [ ] **Step 5: Skriv ClusterIssuer**

`kongebra-gitops/platform/cert-manager/cloudflare-issuer.yaml`:
```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: cloudflare-dns01
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: svein.danielsen@crayon.no
    privateKeySecretRef:
      name: cloudflare-dns01-account-key
    solvers:
      - dns01:
          cloudflare:
            apiTokenSecretRef:
              name: cloudflare-api-token
              key: api-token
```

- [ ] **Step 6: Skriv wildcard-Certificate**

`kongebra-gitops/platform/cert-manager/wildcard-cert.yaml`:
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: kongebra-net-wildcard
  namespace: kube-system
spec:
  secretName: kongebra-net-tls
  issuerRef:
    name: cloudflare-dns01
    kind: ClusterIssuer
  commonName: "*.kongebra.net"
  dnsNames:
    - "*.kongebra.net"
    - "kongebra.net"
```

- [ ] **Step 7: Apply issuer + cert, verifiser at sertifikatet utstedes**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f kongebra-gitops/platform/cert-manager/cloudflare-issuer.yaml
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f kongebra-gitops/platform/cert-manager/wildcard-cert.yaml
```

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n kube-system get certificate kongebra-net-wildcard -w`
Expected: `READY=True` innen ~2 min (DNS-01-propagering). Hvis stuck: `kubectl describe certificate` viser challenge-status.

- [ ] **Step 8: Commit cert-manager-manifester**

```bash
cd /Users/svedanie/kongebra-gitops
git add platform/cert-manager
git commit -m "platform: cert-manager + Cloudflare DNS-01 wildcard for kongebra.net"
git push
```

---

### Task 6: ArgoCD install + app-of-apps bootstrap

**Files:**
- Create: `kongebra-gitops/platform/argocd/application.yaml`
- Create: `kongebra-gitops/platform/argocd/ingressroute.yaml`
- Create: `kongebra-gitops/bootstrap/platform-appset.yaml`

**Interfaces:**
- Consumes: `kongebra-net-tls` (Task 5), root-app (Task 1).
- Produces: kjørende ArgoCD nådd på `https://argocd.kongebra.net`, og en `platform-appset` som auto-oppretter en Argo Application per mappe under `platform/`.

- [ ] **Step 1: Installer ArgoCD (pinnet) imperativt (chicken-egg: ArgoCD må eksistere før det kan styre seg selv)**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl create namespace argocd
KUBECONFIG=~/.kube/kongebra-config kubectl apply -n argocd -f \
  https://raw.githubusercontent.com/argoproj/argo-cd/v2.13.2/manifests/install.yaml
```

- [ ] **Step 2: Verifiser ArgoCD kjører**

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n argocd rollout status deploy/argocd-server --timeout=180s`
Expected: `deployment "argocd-server" successfully rolled out`

- [ ] **Step 3: Eksponer ArgoCD via Traefik IngressRoute med wildcard-TLS**

`kongebra-gitops/platform/argocd/ingressroute.yaml`:
```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: argocd
  namespace: argocd
spec:
  entryPoints: [websecure]
  routes:
    - match: Host(`argocd.kongebra.net`)
      kind: Rule
      services:
        - name: argocd-server
          port: 80
  tls:
    secretName: kongebra-net-tls
```
(ArgoCD-server kjøres i insecure-modus bak Traefik-TLS: `kubectl -n argocd patch configmap argocd-cmd-params-cm --type merge -p '{"data":{"server.insecure":"true"}}' && kubectl -n argocd rollout restart deploy/argocd-server`.)

- [ ] **Step 4: Peg DNS for argocd.kongebra.net mot en node-tailnet-IP, apply route**

Cloudflare DNS: `argocd.kongebra.net` A-record → `<TS_IP_1>` (tailnet-only, eller Tailscale-IP via split-DNS). Deretter:
```bash
# kopier wildcard-secret til argocd-ns (cert-manager utsteder i kube-system)
KUBECONFIG=~/.kube/kongebra-config kubectl get secret kongebra-net-tls -n kube-system -o yaml \
  | sed 's/namespace: kube-system/namespace: argocd/' \
  | KUBECONFIG=~/.kube/kongebra-config kubectl apply -f -
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f kongebra-gitops/platform/argocd/ingressroute.yaml
```
`# ponytail: manuell secret-kopi v1; reflector/cert per-namespace er dag-2`

- [ ] **Step 5: Verifiser ArgoCD-UI over HTTPS**

Run: `curl -sS -o /dev/null -w '%{http_code}' https://argocd.kongebra.net/healthz`
Expected: `200`. Hent admin-passord: `kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d`.

- [ ] **Step 6: Skriv platform-ApplicationSet (dir-generator over platform/)**

`kongebra-gitops/bootstrap/platform-appset.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: platform
  namespace: argocd
spec:
  generators:
    - git:
        repoURL: https://github.com/kongebra/kongebra-gitops.git
        revision: main
        directories:
          - path: platform/*
  template:
    metadata:
      name: 'platform-{{path.basename}}'
    spec:
      project: default
      source:
        repoURL: https://github.com/kongebra/kongebra-gitops.git
        targetRevision: main
        path: '{{path}}'
      destination:
        server: https://kubernetes.default.svc
      syncPolicy:
        automated: { prune: true, selfHeal: true }
        syncOptions: [CreateNamespace=true]
```

- [ ] **Step 7: Apply root-app + platform-appset, commit**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f kongebra-gitops/bootstrap/root-app.yaml
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f kongebra-gitops/bootstrap/platform-appset.yaml
cd /Users/svedanie/kongebra-gitops && git add . && git commit -m "platform: ArgoCD install + app-of-apps + platform ApplicationSet" && git push
```

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n argocd get applications`
Expected: en `platform-cert-manager`-Application synlig (bevis på at dir-generatoren plukket opp Task 5-mappen).

---

### Task 7: GHCR imagePullSecret

**Files:**
- Create: `kongebra-gitops/platform/ghcr-pull/application.yaml` (eller dokumentert imperativ secret)

**Interfaces:**
- Produces: dockerconfigjson-secret `ghcr-pull` i app-namespaces. Konsumeres av app-Deployments i Task 10.

- [ ] **Step 1: Lag classic PAT med read:packages**

GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic) → scope `read:packages`. Kopiér tokenet.

- [ ] **Step 2: Opprett pull-secret i `default` (og senere per app-namespace)**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username=kongebra \
  --docker-password=<CLASSIC_PAT> \
  --docker-email=svein.danielsen@crayon.no \
  -n default
```
`# ponytail: per-namespace secret v1; en operator (reflector) som speiler secret er dag-2`

- [ ] **Step 3: Verifiser secret finnes og er riktig type**

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl get secret ghcr-pull -n default -o jsonpath='{.type}'`
Expected: `kubernetes.io/dockerconfigjson`

---

### Task 8: Apps-ApplicationSet (dir-generator) + Image Updater

**Files:**
- Create: `kongebra-gitops/bootstrap/apps-appset.yaml`
- Create: `kongebra-gitops/platform/argocd-image-updater/application.yaml`

**Interfaces:**
- Consumes: `apps/`-mappen (Task 1), GHCR-credentials (Task 7).
- Produces: auto-oppretting av en Argo Application per mappe under `apps/`. Image Updater bumper tags fra GHCR.

- [ ] **Step 1: Skriv apps-ApplicationSet**

`kongebra-gitops/bootstrap/apps-appset.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
  namespace: argocd
spec:
  generators:
    - git:
        repoURL: https://github.com/kongebra/kongebra-gitops.git
        revision: main
        directories:
          - path: apps/*
  template:
    metadata:
      name: 'app-{{path.basename}}'
      annotations:
        argocd-image-updater.argoproj.io/image-list: "main=ghcr.io/kongebra/{{path.basename}}"
        argocd-image-updater.argoproj.io/main.update-strategy: "newest-build"
    spec:
      project: default
      source:
        repoURL: https://github.com/kongebra/kongebra-gitops.git
        targetRevision: main
        path: '{{path}}'
      destination:
        server: https://kubernetes.default.svc
        namespace: '{{path.basename}}'
      syncPolicy:
        automated: { prune: true, selfHeal: true }
        syncOptions: [CreateNamespace=true]
```

- [ ] **Step 2: Skriv Image Updater som platform-Application (pinnet chart via raw manifest)**

`kongebra-gitops/platform/argocd-image-updater/application.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: platform-argocd-image-updater
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://argoproj.github.io/argo-helm
    chart: argocd-image-updater
    targetRevision: 0.12.0
    helm:
      values: |
        config:
          registries:
            - name: ghcr
              api_url: https://ghcr.io
              prefix: ghcr.io
              credentials: pullsecret:argocd/ghcr-pull
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated: { prune: true, selfHeal: true }
```

- [ ] **Step 3: Kopiér ghcr-pull til argocd-ns (Image Updater leser registry-creds derfra)**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl get secret ghcr-pull -n default -o yaml \
  | sed 's/namespace: default/namespace: argocd/' \
  | KUBECONFIG=~/.kube/kongebra-config kubectl apply -f -
```

- [ ] **Step 4: Apply apps-appset, commit platform-mappen**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl apply -f kongebra-gitops/bootstrap/apps-appset.yaml
cd /Users/svedanie/kongebra-gitops && git add . && git commit -m "gitops: apps ApplicationSet + ArgoCD Image Updater (GHCR)" && git push
```

- [ ] **Step 5: Verifiser ApplicationSet-controller + Image Updater kjører**

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n argocd get applicationset apps && kubectl -n argocd rollout status deploy/platform-argocd-image-updater-argocd-image-updater --timeout=120s`
Expected: ApplicationSet `apps` listet (0 apper enda, `apps/` er tom), Image Updater rullet ut.

---

### Task 9: OTel Collector → eksisterende otel-lgtm

**Files:**
- Create: `kongebra-gitops/platform/otel-collector/application.yaml`
- Create: `kongebra-gitops/platform/otel-collector/values.yaml`

**Interfaces:**
- Consumes: en tailnet-nåbar OTLP-endpoint på otel-lgtm.
- Produces: OTLP-mottak i clusteret (`otel-collector.observability:4318`) som apper sender til; videresender til otel-lgtm.

- [ ] **Step 1: Bestem og verifiser otel-lgtm OTLP-endpoint over tailnet (KRITISK avhengighet)**

otel-lgtm kjører på Swarm-boksen og er intern-overlay (`http://monitoring-otellgtm-qsiwan:4318`) - IKKE nåbar fra k3s. Eksponer OTLP på tailnet: enten Swarm-host tailnet-IP + publisert port 4318, eller Traefik-route `otlp.newb.no`.

Run (fra en k3s-node): `ssh root@89.167.125.132 'curl -sS -o /dev/null -w "%{http_code}" http://<OTEL_LGTM_TAILNET>:4318/v1/traces -X POST -H "Content-Type: application/json" -d "{}"'`
Expected: `400` eller `200` (port svarer). `000`/timeout = endpoint ikke nåbar → fiks eksponering før du fortsetter.

- [ ] **Step 2: Skriv collector-values (DaemonSet-mottak + OTLP-eksport)**

`kongebra-gitops/platform/otel-collector/values.yaml`:
```yaml
mode: daemonset
image:
  repository: otel/opentelemetry-collector-contrib
config:
  receivers:
    otlp:
      protocols:
        http: { endpoint: 0.0.0.0:4318 }
        grpc: { endpoint: 0.0.0.0:4317 }
  processors:
    batch: {}
    resourcedetection:
      detectors: [env, system]
  exporters:
    otlphttp:
      endpoint: http://<OTEL_LGTM_TAILNET>:4318
  service:
    pipelines:
      traces:  { receivers: [otlp], processors: [resourcedetection, batch], exporters: [otlphttp] }
      metrics: { receivers: [otlp], processors: [resourcedetection, batch], exporters: [otlphttp] }
      logs:    { receivers: [otlp], processors: [resourcedetection, batch], exporters: [otlphttp] }
```

- [ ] **Step 2b: Skriv collector-Application**

`kongebra-gitops/platform/otel-collector/application.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: platform-otel-collector
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://open-telemetry.github.io/opentelemetry-helm-charts
    chart: opentelemetry-collector
    targetRevision: 0.108.0
    helm:
      valueFiles: []
      values: |  # inlines values.yaml-innholdet over
  destination:
    server: https://kubernetes.default.svc
    namespace: observability
  syncPolicy:
    automated: { prune: true, selfHeal: true }
    syncOptions: [CreateNamespace=true]
```
(Lim values.yaml-innholdet inn under `helm.values`, eller bruk `valueFiles: [values.yaml]` med values.yaml i samme path.)

- [ ] **Step 3: Commit, la ArgoCD synce, verifiser collector kjører**

```bash
cd /Users/svedanie/kongebra-gitops && git add platform/otel-collector && git commit -m "platform: OTel Collector exporting to otel-lgtm" && git push
```

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n observability rollout status daemonset/platform-otel-collector-opentelemetry-collector-agent --timeout=120s`
Expected: daemonset rullet ut på alle 3 noder.

- [ ] **Step 4: End-to-end-verifisering: send en test-trace, se den i Grafana**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl -n observability run otel-test --rm -it --restart=Never \
  --image=curlimages/curl -- \
  curl -sS -X POST http://platform-otel-collector-opentelemetry-collector-agent.observability:4318/v1/traces \
  -H 'Content-Type: application/json' -d @- <<'EOF'
{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"otel-smoke-test"}}]},"scopeSpans":[{"spans":[{"traceId":"00000000000000000000000000000001","spanId":"0000000000000001","name":"smoke","kind":1,"startTimeUnixNano":"1","endTimeUnixNano":"2"}]}]}]}
EOF
```
Expected: `200`. Verifiser i Grafana (`grafana.newb.no` → Tempo → search `service.name=otel-smoke-test`) at spanet dukker opp innen ~1 min.

---

### Task 10: Deploy go-hello-world via GitOps (første app, ende-til-ende)

**Files:**
- Verify: `kongebra-apps/apps/go-hello-world/` + eksisterende CI pusher til GHCR
- Create: `kongebra-gitops/apps/go-hello-world/deployment.yaml`
- Create: `kongebra-gitops/apps/go-hello-world/service.yaml`
- Create: `kongebra-gitops/apps/go-hello-world/ingressroute.yaml`
- Create: `kongebra-gitops/apps/go-hello-world/kustomization.yaml`

**Interfaces:**
- Consumes: apps-ApplicationSet (Task 8), ghcr-pull (Task 7), wildcard-TLS (Task 5), Traefik.
- Produces: go-hello-world live på `https://hello.kongebra.net`. Beviser hele "lag ny app"-flyten.

- [ ] **Step 1: Bekreft GHCR-image finnes (CI fra apps-repoet)**

Run: `gh api /users/kongebra/packages/container/go-hello-world/versions --jq '.[0].metadata.container.tags' 2>/dev/null || echo "ingen image - trigg CI først"`
Expected: en tag-liste (f.eks `["latest","2026-06-26-abcd123"]`). Hvis tom: kjør `gh workflow run go-hello-world.yml` i apps-repoet og vent.

- [ ] **Step 2: Skriv Deployment (med ghcr-pull + ressursgrenser for 4GB)**

`kongebra-gitops/apps/go-hello-world/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: go-hello-world }
spec:
  replicas: 2
  selector: { matchLabels: { app: go-hello-world } }
  template:
    metadata: { labels: { app: go-hello-world } }
    spec:
      imagePullSecrets: [{ name: ghcr-pull }]
      containers:
        - name: app
          image: ghcr.io/kongebra/go-hello-world:latest  # Image Updater bumper denne
          ports: [{ containerPort: 8080 }]
          resources:
            requests: { cpu: 25m, memory: 32Mi }
            limits:   { cpu: 250m, memory: 128Mi }
          env:
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: http://platform-otel-collector-opentelemetry-collector-agent.observability:4318
```

- [ ] **Step 3: Skriv Service**

`kongebra-gitops/apps/go-hello-world/service.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata: { name: go-hello-world }
spec:
  selector: { app: go-hello-world }
  ports: [{ port: 80, targetPort: 8080 }]
```

- [ ] **Step 4: Skriv IngressRoute + kopier wildcard-secret til app-ns**

`kongebra-gitops/apps/go-hello-world/ingressroute.yaml`:
```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata: { name: go-hello-world }
spec:
  entryPoints: [websecure]
  routes:
    - match: Host(`hello.kongebra.net`)
      kind: Rule
      services: [{ name: go-hello-world, port: 80 }]
  tls: { secretName: kongebra-net-tls }
```

`kongebra-gitops/apps/go-hello-world/kustomization.yaml`:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: [deployment.yaml, service.yaml, ingressroute.yaml]
```

- [ ] **Step 5: Forhåndsoppsett app-namespace (pull-secret + TLS-secret må finnes i `go-hello-world`-ns)**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl create namespace go-hello-world
for s in ghcr-pull:default kongebra-net-tls:kube-system; do
  KUBECONFIG=~/.kube/kongebra-config kubectl get secret ${s%%:*} -n ${s##*:} -o yaml \
    | sed "s/namespace: ${s##*:}/namespace: go-hello-world/" \
    | KUBECONFIG=~/.kube/kongebra-config kubectl apply -f -
done
```
`# ponytail: manuell secret-kopi per app v1; reflector-operator automatiserer dette dag-2`

- [ ] **Step 6: Cloudflare DNS for hello.kongebra.net + commit (ApplicationSet auto-deployer)**

Cloudflare: `hello.kongebra.net` A → `<TS_IP_1>` (tailnet).
```bash
cd /Users/svedanie/kongebra-gitops && git add apps/go-hello-world && git commit -m "app: go-hello-world via GitOps on hello.kongebra.net" && git push
```

- [ ] **Step 7: Verifiser ArgoCD auto-opprettet appen (beviser dir-generator-flyten)**

Run: `KUBECONFIG=~/.kube/kongebra-config kubectl -n argocd get application app-go-hello-world -o jsonpath='{.status.sync.status}'`
Expected: `Synced` (innen ~1-3 min etter push).

- [ ] **Step 8: Ende-til-ende: appen svarer over HTTPS**

Run: `curl -sS -o /dev/null -w '%{http_code}' https://hello.kongebra.net`
Expected: `200`. Dette beviser hele kjeden: GHCR-pull → k3s → Traefik → wildcard-TLS → GitOps-deploy.

- [ ] **Step 9: Verifiser app-telemetri når Grafana (OTEL ende-til-ende)**

Generer trafikk (`for i in $(seq 20); do curl -s https://hello.kongebra.net >/dev/null; done`), så sjekk Grafana (`grafana.newb.no` → Tempo) for `service.name=go-hello-world`.
Expected: traces fra appen synlige innen ~1 min.

---

## Self-Review

**Spec coverage:**
- k3s 3x HA → Task 3 (+ HA-bevis Step 6). ✓
- ArgoCD GitOps → Task 6 + 8. ✓
- OTEL ut av clusteret → Task 9. ✓
- "Lag ny app"-flyt fra GHCR → Task 7 (pull) + 8 (appset/updater) + 10 (bevis ende-til-ende). ✓
- Tailscale-tilgang → Task 2 + 4. ✓
- cert-manager + Cloudflare DNS-01 → Task 5. ✓
- Repos/rename/naming → Task 1. ✓
- GOMEMLIMIT/eviction (4GB) → Task 3 Step 1-4 (Global Constraints). ✓
- Hetzner privat nett + firewall → bevisst dag-2 (spec Ikke-mål/dag-2), ikke en v1-task. ✓

**Kjent skarp kant gjort eksplisitt:** OTel-endpoint-nåbarhet (Task 9 Step 1) er en ekte avhengighet til Swarm-infraen - markert KRITISK med egen verifisering før resten av tasken.

**Manuelle forenklinger markert `ponytail:`:** secret-håndtering (CF-token, ghcr-pull, wildcard-secret-kopi) er manuell i v1, Sealed Secrets/reflector er dag-2. Bevisst, ikke uvitenhet.

**Versjoner pinnet:** k3s v1.32.5+k3s1, cert-manager v1.16.2, ArgoCD v2.13.2, image-updater chart 0.12.0, otel-collector chart 0.108.0. (Verifiser/oppdater til siste patch ved utførelse.)
