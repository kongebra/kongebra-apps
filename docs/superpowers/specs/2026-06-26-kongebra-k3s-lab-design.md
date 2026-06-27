# Kongebra k3s-lab - design

Dato: 2026-06-26
Status: godkjent design, klar for implementeringsplan

## Mål

Et minimalt, men produksjonskvalitets selvhostet Kubernetes-miljø som læringslab for HA, multi-node, observability og drift.
Minste-krav (kjernen):

1. Kubernetes (k3s) kjørende på 3 noder.
2. ArgoCD for GitOps-deploy.
3. OTEL-telemetri ut av clusteret.
4. En GitOps-måte å lage nye apper på, der Docker-images ligger i GHCR.

Alt utover dette er bevisst utsatt (se Ikke-mål).

## Ikke-mål (bevisst utsatt)

- Talos Linux. k3s kjører rett på de eksisterende Ubuntu-imagene, ingen ISO-reflash. Talos vurderes ved oppgradering til 8GB-noder.
- Cilium / Hubble. k3s default (Flannel + kube-proxy) holder for v1. Cilium er en 8GB-oppgradering.
- Zitadel HA, Patroni-Postgres, etcd raft-lab, chaos-engineering. Interessante, men ikke en del av minste-kravet.
- Egen in-cluster observability-stack (Prometheus/Loki/Tempo). Vi gjenbruker eksisterende otel-lgtm.

## Maskinvare

- 3x Hetzner CX23 (Helsinki, x86, 2 vCPU / 4GB RAM / 40GB disk hver). Totalt 6 vCPU / 12GB / 120GB.
- Disk er NVMe-backed (viktig for etcd fsync).
- Boksene kjører Ubuntu i dag og forblir Ubuntu. Ingen OS-bytte.
- Noder døpes om fra Hetzner-default (`ubuntu-4gb-hel1-*`) til `kongebra-k8s-1`, `kongebra-k8s-2`, `kongebra-k8s-3`. Funksjonelt, maks klarhet, skalerer trivielt.
- Oppgraderingssti: bump til 8GB per node når tilgjengelig. Designet holdes kompatibelt med en senere Talos/Cilium-migrasjon.

## Navngiving, repos og domener

GitHub-eier er `kongebra` (personlig konto med blandede repos), derfor `kongebra-`-prefix på plattform-repoene for å gruppere dem.

| Rolle | Repo | Innhold |
|---|---|---|
| Apper | `kongebra/kongebra-apps` (rename fra `kongebra/newb.no`) | monorepo for alle apper, CI bygger til GHCR |
| GitOps | `kongebra/kongebra-gitops` (nytt) | ArgoCD app-of-apps + ApplicationSet + k8s-manifester per app |
| Infra | `kongebra/kongebra-infra` (nytt, dag-2) | OpenTofu for Hetzner privat nett + firewall |

- Images: `ghcr.io/kongebra/<app>`.
- Domene: `*.kongebra.net` styrt av Cloudflare. `*.newb.no` kan legges til som ekstra ingress-host senere uten relokking.
- Domenet er en deploy-detalj per app (IngressRoute host), ikke noe plattformen låses til.
- Rename av monorepoet gjøres nå mens kun go-hello-world finnes (blast-radius ~0; GitHub auto-redirecter gammel URL, kun image-org-path i CI må oppdateres).

## Arkitektur

### Noder og nettverk

- Tailscale på alle 3 noder (`tag:k8s`). Kreves fordi eksisterende otel-lgtm er tailnet-only, og gir samtidig kubectl-tilgang og lås av kube-apiserver mot offentlig nett.
- Hetzner Cloud Firewall deny-all inbound utenom ICMP + UDP 41641 (Tailscale). Kan settes opp dag-2, ikke blokkerende for v1.
- Hetzner privat nettverk for etcd/CNI-traffikk: dag-2 (OpenTofu), v1 kan kjøre over tailnet/privat-IP der det er enkelt.

### k3s

- 3x server med embedded etcd (HA control plane, tåler 1 node nede).
- Install via offisiell one-liner: `--cluster-init` på node 1, `--server` join på node 2 og 3.
- Beholder k3s-batterier: Traefik (ingress), servicelb (klipper), local-path storage, metrics-server.
- `GOMEMLIMIT` satt på k3s-prosessen + eksplisitte memory-eviction-thresholds. Dette er ikke valgfritt på 4GB: den skarpe kanten er at en etcd-member-restart kan spike til 2-3GB og trigge OOM-kaskade.

### TLS og ingress

- Traefik (k3s-bundled) som ingress-controller.
- cert-manager med Cloudflare DNS-01 ClusterIssuer for `*.kongebra.net` wildcard-cert. DNS-01 (ikke HTTP-01) fordi nodene er bak Tailscale/firewall.
- Cloudflare API-token lagres som k8s Secret (bootstrappes manuelt, eller via Sealed Secrets dag-2).

### GitOps

- ArgoCD installeres og bootstrappes med app-of-apps-mønster.
- ApplicationSet med Git directory-generator scanner `kongebra-gitops/apps/*` og lager én Argo Application per mappe automatisk.
- GHCR er privat, så et imagePullSecret kreves. Classic PAT med `read:packages` (fine-grained PAT funker ikke mot GHCR, kjent driftsfelle).
- ArgoCD Image Updater watcher GHCR-tags, bumper image-tag og skriver tilbake til gitops-repoet.

### Observability (OTEL)

- OpenTelemetry Collector som DaemonSet + gateway-Deployment. Dette er OTEL-læringssentrum: receivers -> processors -> exporters.
- Eksporterer traces/metrics/logs til eksisterende otel-lgtm (Tempo/Loki/Prometheus/Grafana) på Swarm-boksen over tailnet.
- metrics-server (k3s-bundled) gir `kubectl top` og HPA.
- Ingen tung observability-backend i clusteret. Det er bevisst, for å spare RAM på 4GB.

## "Lag ny app"-flyt (kjerneleveransen)

1. Legg `apps/<navn>/` i `kongebra-apps` med en per-app CI-workflow (kopiér eksisterende mal). CI bygger og pusher `ghcr.io/kongebra/<navn>:<tag>`.
2. Legg `apps/<navn>/` i `kongebra-gitops` med en kustomization: Deployment + Service + Traefik IngressRoute (host velger `<navn>.kongebra.net` eller `<navn>.newb.no`).
3. ApplicationSet ser den nye mappen og ArgoCD oppretter + deployer Applicationen automatisk. Ny mappe = ny app live.
4. Image Updater holder image-tag i synk med GHCR fremover.

## RAM-budsjett (12GB total, ærlig)

| Komponent | ~RAM |
|---|---|
| k3s control plane + etcd (3 noder) | ~2GB |
| ArgoCD | ~0.5-1GB |
| cert-manager + Traefik + Tailscale + OTel Collector | ~0.5-0.7GB |
| Igjen til apper | ~8GB |

Romslig nok for små Go-apper. Oppgrader til 8GB-noder når det knaper.

## Bootstrap-rekkefølge

1. Tailscale på alle 3 noder (`tag:k8s`).
2. k3s HA install (node 1 `--cluster-init`, node 2+3 join som server), sett `GOMEMLIMIT` + eviction-thresholds.
3. Hent kubeconfig, verifiser 3 noder `Ready` + etcd-quorum.
4. cert-manager + Cloudflare DNS-01 ClusterIssuer.
5. ArgoCD install + app-of-apps bootstrap.
6. GHCR imagePullSecret + ApplicationSet (directory-generator) + Image Updater.
7. OTel Collector -> otel-lgtm.
8. Rename `newb.no` -> `kongebra-apps`, oppdater image-org-path i CI.
9. Deploy go-hello-world via GitOps som første app (på `hello.kongebra.net`).

## Risiko og kjente feller

- etcd-restart-OOM på 4GB. Mitigeres med GOMEMLIMIT + eviction-thresholds + NVMe-disk. Å overleve og instrumentere dette er selve drift-leksjonen.
- GHCR krever classic PAT, ikke fine-grained.
- LE HTTP-01 funker ikke bak tailnet; derfor DNS-01.
- Cloudflare API-token er en hemmelighet som må håndteres trygt (manuelt v1, Sealed Secrets dag-2).

## Oppgraderingssti (8GB-trapp)

Når 8GB lander på nodene:

- Vurder migrasjon til Talos (immutable, API-only) for prod-realisme.
- Vurder Cilium + Hubble for eBPF-nettverk og flow-observability.
- Vurder tyngre workloads (Zitadel HA, Patroni-Postgres) og et dedikert etcd raft-/chaos-lab.
