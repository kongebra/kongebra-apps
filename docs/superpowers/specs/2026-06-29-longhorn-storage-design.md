# Delsystem A: Longhorn persistent storage - design

Dato: 2026-06-29.
Status: **SUPERSEDED 2026-06-29** - bygges IKKE. Clusteret bruker allerede **hcloud-csi** (`hcloud-volumes`, Hetzner nettverks-block-storage: overlever node-død + reattacher, ingen open-iscsi-prereq, ingen per-node-overhead). Deployet på main før denne spec-en ble vurdert. Longhorn unødvendig. CNPG/Gatus bruker `storageClass: hcloud-volumes`. gitops#6 lukket. Beholdt som dokumentasjon av storage-vurderingen.

(Original handoff-design under - ikke gjeldende.)

Første av tre delsystemer for status.newb.no fase 2 (persistent historikk). Avhengighet: **A (denne) → B (Postgres+Redis) → C (status-checker fase 2 app)**.
A leverer replikert HA block storage så PVC-er overlever node-tap - grunnmuren B og all fremtidig stateful hviler på.

## Mål

- Replikert persistent storage på det 3-node k3s-clusteret (3x Hetzner CX33, 8GB, Tailscale).
- PVC-er overlever node-tap (ekte HA på data-laget - kjernen i lab-øvelsen).
- `longhorn` som default StorageClass, så B og senere apper bare ber om en PVC.

## Ikke-mål (parkert)

- Offsite-backup (S3-target + recurring snapshots) - egen senere øvelse.
- Talos/Cilium-migrering - egen lab-tråd.
- HA på compute-laget (PG-replikering, Traefik/CoreDNS-skalering, workload anti-affinity) - se Follow-up.

## Komponenter

### 1. Node-prereqs (eneste ikke-GitOps-bit)

Longhorn krever iSCSI for volume-attach. På **alle 3 noder**:
```sh
apt-get update && apt-get install -y open-iscsi nfs-common
systemctl enable --now iscsid
```
Dette kan ikke gjøres via GitOps (node-pakker). Manuelt via ssh, eller en bootstrap. Verifiser `systemctl status iscsid` = active på hver node før Longhorn installeres.

### 2. Longhorn via ArgoCD Application

`kongebra-gitops/platform/longhorn/application.yaml` - Helm-chart **pinnet til eksakt versjon** (verifiser siste stabile ved utførelse), namespace `longhorn-system`:
- `defaultSettings.defaultReplicaCount: 3` (full redundans over de 3 nodene).
- Gjør `longhorn` til **default StorageClass** (`storageClass.defaultClass: true`); hvis k3s `local-path` er default, fjern dens default-annotation (`kubectl annotate sc local-path storageclass.kubernetes.io/is-default-class- `).
- `defaultSettings.defaultDataPath: /var/lib/longhorn` (default; CX33 enkelt-disk).
- syncPolicy automated (prune + selfHeal), `CreateNamespace=true`, matcher platform-appset-mønsteret.

### 3. Longhorn UI (IngressRoute)

`kongebra-gitops/platform/longhorn/ingressroute.yaml` - Traefik IngressRoute `longhorn.newb.no`, entryPoint websecure, wildcard-TLS (`kongebra-net-tls` / newb.no-cert), tailnet-only. Matcher argocd/grafana-mønsteret. Cloudflare DNS A-record `longhorn.newb.no` → node-tailnet-IP.
**Merk:** Longhorn-UI har ingen innebygd auth - tailnet er gaten (som resten av `*.newb.no`).

## Verifisering (DoD)

1. `iscsid` active på alle 3 noder; Longhorn-pods (manager DaemonSet + instance-managers) Running på alle 3.
2. `kubectl get sc` viser `longhorn (default)`.
3. Provisjoner en test-PVC (RWO, longhorn), bind den til et test-pod, skriv en fil.
4. **HA-bevis:** finn noden som holder volumets primær-replica, `systemctl stop k3s` på den (eller cordon+drain) → test-pod reschedules, **dataen er fortsatt der** (Longhorn re-attacher fra en sunn replica). Start noden igjen, verifiser replica re-syncer i UI.
5. `longhorn.newb.no` viser volume-dashboard over HTTPS.

## Ressurs-merknad

Longhorn manager + instance-manager kjører **per node** (~200-300Mi/node), fast overhead uavhengig av antall volumer/databaser - amortiseres over alt stateful. På 8GB CX33 greit, men hold det i bakhodet sammen med PG+Redis (B).

## Follow-up (parkert, ikke del av A)

- **Backup:** S3-target (Hetzner Object Storage / Backblaze B2 / in-cluster MinIO) + recurring snapshot-jobb. Egen øvelse.
- **HA-herding av compute** (separat tråd, tas når lab-testing starter): PG-replikering (CloudNativePG) i/etter B, Traefik + CoreDNS replica-skalering, `topologySpreadConstraints`/anti-affinity på app-Deployments (go-hello-world, status-web replicas=2 er ikke HA før spredning tvinges - CLAUDE.md "replicas != HA"). Etter A er control-plane + storage HA; workloads er ikke fullt HA ennå.

## Referanser

- status.newb.no fase 2-dekomponering: A→B→C. B-spec (Postgres+Redis) og C-spec (status-checker fase 2) skrives separat.
- Cluster: `kongebra-gitops` (platform-appset, base/overlays, wildcard-TLS, ArgoCD), k3s-lab plan `docs/superpowers/plans/2026-06-26-kongebra-k3s-lab.md`.
