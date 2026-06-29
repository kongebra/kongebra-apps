# Status-stack: Gatus + gatus-sidecar - design

Dato: 2026-06-29.
Status: design godkjent (etter 4-ekspert fan-out review, ship-with-changes). **Handoff til infra-agent** (kongebra-gitops). Ingen custom-kode.

**Avløser** den planlagte custom fase 2-4-appen (C) OG den parkerte custom-operatoren (fase 5). Begge erstattet av hyllevare etter buy-over-build-pivot:
- **Gatus** (TwiN/gatus) = status-siden + uptime-historikk + alerting (fase 2-4 innebygd).
- **gatus-sidecar** (home-operations/gatus-sidecar) = auto-discovery: watcher Traefik IngressRoute (+ Ingress/Service/HTTPRoute) via annotation, genererer Gatus endpoint-config (= det fase-5-operatoren skulle gjort).

All lab-læring i dette prosjektet konsentreres nå i **A (Longhorn HA storage) + B (CloudNativePG Postgres) + drift av Gatus-stacken** - app-bygging er kjøpt. Det rettferdiggjør CNPG-valget: PG-på-k8s er den primære k8s-leksjonen her.

Avhengighet: **A (Longhorn, gitops#6) → B (Postgres, gitops#7) → Gatus-stack (denne)**.

## Komponenter (per env, ArgoCD)

- **Gatus** Deployment, `replicas: 1` (Gatus har ingen multi-instans-koordinering; 2 replicas = uavhengig probing + dobbel-skriving av historikk - pin 1 eksplisitt). Helm-chart `gatus` fra `https://twin.github.io/helm-charts` **pinnet til eksakt versjon**, image pinnet. Namespace `status-dev`/`status-prod`.
- **gatus-sidecar** som sidecar-container i samme pod (eller egen deployment + delt volum). Pinnet image. RBAC: ClusterRole for å watche IngressRoutes på tvers av namespaces (read-only).
- **CNPG status-db** (B) i samme namespace (secret er namespace-lokal - se must-fix 4).

## Must-fix (fra panel, bakt inn)

### 1. DSN-injeksjon - aldri i ConfigMap
Gatus `storage.path` tar en postgres-DSN. CNPG-secret `status-db-app` eksponerer den under key `uri`. Injiser som env-var via `secretKeyRef` (chartens `envFrom`/`env`), og sett `storage.path: "${DATABASE_URL}"` (Gatus `${ENV}`-interpolering). DSN-en MÅ IKKE templates inn i den inline `config`-ConfigMap-en (charten rendrer `config` til en ConfigMap → passord i plaintext). `storage.type: postgres`.

### 2. sslmode verifisert mot lib/pq (ikke bare psql) - gater DoD
CNPG `uri` er `postgresql://...`; uten `sslmode` defaulter Gatus' lib/pq til `sslmode=require` mot CNPG sitt selvsignerte-CA `-rw`-endpoint. Sørg for at effektiv DSN har `sslmode=require` (B valgte dette; verify-full utsatt). **DoD MÅ verifisere Gatus' EGEN connection** (lib/pq negotierer TLS annerledes enn psql - B-spec DoD testet kun psql).

### 3. Config-topologi - løst av gatus-sidecar
ConfigMap-montert dir er read-only → en writer kan ikke skrive der. gatus-sidecar sitt native mønster løser det: **delt `emptyDir`** mellom sidecar og Gatus; sidecar skriver `/config/gatus-sidecar.yaml` via atomic tempfile+rename; Gatus `GATUS_CONFIG_PATH=/config` (katalog, deep-merge + hot-reload).
- **Base-config** (singletons: `storage.*`, `metrics`, `web`, ev. `alerting`) som ÉN fil - enten en initContainer som kopierer en ConfigMap inn i emptyDir, eller en egen base-fil. gatus-sidecar-filen MÅ KUN appende endpoints (Gatus directory-merge: primitive skalarer kan defineres kun én gang på tvers av filer).
- `skip-invalid-config-update=true` på Gatus (så en ugyldig sidecar-skrevet fil ikke får Gatus til å exit'e + ta ned siden). Viktig nå som en watcher skriver config.

### 4. Namespace-rename → push til infra-agent FØR B applyes
status-checker pensjoneres → DB + Gatus bor i `status-dev`/`status-prod`. **Gatus MÅ ligge i SAMME namespace som `status-db`** (CNPG-secret er namespace-lokal; eneste alternativ er reflector/ESO-replikering - ikke split dem). **Oppdater B-handoff (gitops#7) til `status-dev/-prod` i samme commit som denne beslutningen** - å flytte en CNPG Cluster på tvers av namespaces er destroy+recreate, ikke rename, så det MÅ lande før B applyes ellers brekker A→B→Gatus-kjeden.

### 5. Atomisk host-cutover av live status.newb.no
status-web serverer status.newb.no nå. To IngressRoutes på samme Host = ikke-deterministisk Traefik-routing på en live side. Sekvens:
1. Gatus dev opp på `status-dev.newb.no` (ingen kollisjon - status-web er prod-only). Verifiser full DoD inkl. persistens-overlever-restart.
2. Gatus prod opp UTEN `status.newb.no`-host (verifiser in-cluster/temp-host).
3. ÉN atomisk commit som fjerner `Host(status.newb.no)` fra status-web sin IngressRoute OG legger den på Gatus → ArgoCD applyer begge i samme sync-wave. Aldri to commits (add-så-fjern = kollisjonsvindu; fjern-så-add = nedetid). Ingen DNS-endring (A-record peker alt på node-IP-er; kun IngressRoute-eier endres).

## gatus-sidecar config

- Discovery via annotation `gatus.home-operations.com/enabled: "true"` på IngressRoutes (eller `--auto`-modus). Annoter dagens targets: go-hello-world, grafana, argocd (+ self).
- Default conditions per endpoint: `[STATUS] == 200` + response-time. Sidecar utleder URL fra IngressRoute `Host()`-regelen.
- CoreDNS-prereq (hard): Gatus-pods prober `*.newb.no` → samme avhengighet som fase 1 (verifisert fungerende). Bekreft at `newb.no`→tailnet-forward er **cluster-wide CoreDNS-config** (ikke scoped til checker-deployment), siden Gatus-pods er andre pods. Restat i DoD.

## Self-monitoring (regresjon fra fase 1 - må adresseres)

Fase 1 hadde status-web self-watch + OTEL-heartbeat-alert når checker sluttet rapportere. Gatus er én komponent som både prober og serverer - dør den, rapporterer ingenting. Tiltak:
- Gatus sin egen `/health` som et target (billig self-watch).
- En EKSTERN heartbeat: Grafana-alert når Gatus-metrics opphører, eller en ekstern sjekk på at status.newb.no slutter svare. (Fase-1 Grafana-paneler keyet på `target_up`/`target_latency_ms` forsvinner når checker pensjoneres - Gatus emitter ikke de seriene i samme form; vurder Gatus' egne Prometheus-metrics.)

## Verifisering (DoD)

1. Gatus dev på `status-dev.newb.no` viser dashboard, alle annoterte targets up.
2. **Gatus' egen** postgres-connection OK (ikke bare psql) - historikk skrives.
3. Persistens: restart **Gatus-pod** → historikk intakt. Restart **Postgres-pod** (instances:1 = hard outage, B linje 54) → bekreft Gatus reconnect + historikk intakt, og Gatus' posture når PG er nede (crash-loop vs serve-degraded - noter som akseptert lab-risiko eller par med instances:1→3).
4. gatus-sidecar: annoter en ny IngressRoute → endpoint dukker opp i Gatus innen reload (atomic write til emptyDir).
5. Atomisk host-cutover: status.newb.no serveres av Gatus, ingen Traefik-router-konflikt, status-web holdt warm for rollback.

## Cutover + rollback + dekommisjonering

- **Rollback:** behold status-web Deployment Running men host-løs gjennom et soak-vindu (f.eks. 48t / til historikk bekreftet persistere over Gatus-restart). Rollback = reverter den ene host-flyttingen (umiddelbart, ingen rebuild).
- **Dekommisjoner (etter soak, egen ryddejobb):** prune status-web + status-checker-manifester via ArgoCD; slett de 4 CI-workflowene; slett `apps/status-checker/` + `apps/status-web/`; deprecér GHCR-pakkene `ghcr.io/kongebra/status-checker` + `status-web`. **KRITISK:** å rive de gamle `status-checker-*`-namespacene må IKKE ta CNPG-Cluster med seg - derfor MÅ rename (must-fix 4) lande først (sletter du namespacet der B laget `status-db`, sletter du databasen).

## Bevisst ratifisert (yagni-merknad)

CNPG Postgres + per-env (2 Gatus + 2 PG) er tyngre enn Gatus alene trenger (SQLite-på-PVC ville eliminert must-fix 1+2). Beholdt **bevisst** som lab-læring: PG-på-k8s + per-app-per-env-paritet. Med operatoren også kjøpt er dette nå den primære k8s-læringen i prosjektet, ikke uutforsket default. `storage.size` modest (historikk for en håndfull targets er bitteliten); ingen CNPG-backup for Gatus-data (regenerabel, B linje 71).

## Referanser

- A: `2026-06-29-longhorn-storage-design.md`. B: `2026-06-29-postgres-cnpg-design.md` (oppdateres med namespace-rename). Fase 1 (pensjoneres): `2026-06-28-status-page-design.md`.
- gatus-sidecar: home-operations/gatus-sidecar (Go, Traefik IngressRoute first-class, annotation `gatus.home-operations.com/enabled`, shared-volume atomic writes).
