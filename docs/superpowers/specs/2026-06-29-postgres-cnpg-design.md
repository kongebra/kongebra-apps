# Delsystem B: Postgres (CloudNativePG) - design

Dato: 2026-06-29.
Status: design godkjent (etter 5-ekspert fan-out review, ship-with-changes; Redis kuttet fra B). Ingen kode. **Handoff til infra-agent** (implementeres i `kongebra-gitops`).

Andre av tre delsystemer for status.newb.no fase 2. Avhengighet: **A (Longhorn) → B (denne: Postgres) → C (status-checker fase 2 app)**.
B leverer en persistent Postgres per env som C konsumerer for durable status-historikk. **Redis er kuttet fra B** (se Redis-beslutning nederst) - C leser nå-status rett fra PG.

## Mål

- Persistent Postgres via **CloudNativePG-operator** på Longhorn (A), per env (dev/prod).
- Et presist, byggbart B→C connection-grensesnitt så C kan skrives uten gjetting.
- Data-HA via Longhorn nå; compute-HA (instances:3) som lavrisiko-oppgradering senere.

## Ikke-mål (kuttet/parkert)

- **Redis** - kuttet fra B (cacher en triviell query + sikkerhetsrisiko uten NetworkPolicy-håndhevelse). Returnerer i en senere C-slice som auth+TTL read-through cache HVIS aggregat-endepunktene måles trege.
- PG-replikering/failover (`instances: 1` nå), PgBouncer/Pooler, offsite-backup (S3/barman), Sealed Secrets/ESO, mTLS C↔PG.

## To deler

### B1 - CloudNativePG operator (platform, én gang)

ArgoCD Application `kongebra-gitops/platform/cloudnative-pg/`, operator i `cnpg-system`-ns, Helm-chart **pinnet til eksakt versjon**. Cluster-scoped - installeres én gang, betjener alle fremtidige PG-databaser.

**Må-fix (vanligste CNPG+ArgoCD-feil):**
- `ServerSideApply=true` på Application-en (`syncOptions`). CNPG-CRD `clusters.postgresql.cnpg.io` overstiger ArgoCDs client-side annotation-grense (262144 bytes) → `metadata.annotations: Too long` ellers.

### B2 - Status-databasen (per env)

CNPG **Cluster-CRD** `status-db` i hver env-ns (`status-checker-dev`/`-prod`), co-lokalisert med checkeren. Nøkkel-config:
- `instances: 1` (compute-HA utsatt; se oppgraderingssti).
- `bootstrap.initdb.database: status`, `bootstrap.initdb.owner: status` (eksplisitt - default er `app`/`app`). App-rollen `status` eier sin egen DB → kan kjøre goose-migrasjoner (CREATE TABLE) **uten superuser**. `enableSuperuserAccess: false` (default).
- `storage.storageClass: longhorn`, `storage.size: 5Gi`.
- **`walStorage.storageClass: longhorn`, `walStorage.size: 2Gi`** - WAL på egen volume (en full WAL-volume hard-stopper PG; kan IKKE legges til en eksisterende cluster uten recreate, så gjør det nå).
- `imageName: ghcr.io/cloudnative-pg/postgresql:17.x` pinnet til eksakt minor (operator-oppgradering skal ikke flytte major; dev=prod identisk).
- `resources`: requests `100m/256Mi`, limits `500m/512Mi` (start). `postgresql.parameters`: `shared_buffers: 128MB`, `work_mem: 8MB` - PG sizer til cgroup-limit, ikke node-RAM (CNPG auto-tuner ikke). 8GB CX33, 2 envs = betal 2x + Longhorn per-node-overhead.

**ArgoCD prune-sikkerhet (kritisk - ellers stille datatap):**
- `Prune=false` på Cluster-CR-en (per-resource sync-option/annotation). Bare `prune:true` på en database betyr at fjerning/omdøping av manifestet prunes CR-en → CNPG reclaim sletter PVC-ene → totalt datatap. Dokumenter at sletting av manifestet er destruktivt.
- CRD-før-CR-rekkefølge: operator+CRD via platform-appset, Cluster-CR via apps-appset (ingen ordnings-garanti). På fresh bootstrap feiler `no matches for kind Cluster`. Bruk sync-wave eller `SkipDryRunOnMissingResource`/retry - velg og dokumenter.

## B→C connection-kontrakt (eksakt - dette er grensesnittet C bygger mot)

CNPG lager automatisk secret **`status-db-app`** (type `kubernetes.io/basic-auth`) med disse keys:
`username`, `password`, `dbname`, `host`, `port`, `pgpass`, `uri`, `jdbc-uri`.

- **C bruker `uri`-key som DSN** (komplett connection-string rett til pgx + goose - ingen manuell sammensetting, ingen gjetting på key-navn). `db`/`user`/`pass` finnes IKKE.
- **Services:** C kobler til `status-db-rw.<ns>:5432` for ALT mens `instances: 1`. `status-db-ro`/`-r` har **null endpoints** ved instances:1 → connection-refused. Forby `-ro` til `instances >= 2`.
- **TLS:** CNPG aktiverer TLS på `-rw`. `uri` inkluderer ev. sslmode; C bruker `sslmode=require` (verify-full krever montert CA - utsatt).
- **Schema:** C eier alt schema via goose-migrasjoner. B leverer en tom database `status` + eier `status`. Ingen seed fra B.
- **C er env-agnostisk:** identisk secret-navn (`status-db-app`) + service-navn (`status-db-rw`) i begge env; eneste forskjell er namespace, håndtert i overlay. C har null env-betinget kode.

**PG-nede-kontrakt for C (instances:1 = pod-recreate er hard outage, ikke blip):**
- C MÅ degradere grasiøst når PG er utilgjengelig: status-siden faller ikke (ingen 500), feilede writes etterlater et hull i historikken (akseptert).
- C sin `/health` blir IKKE unready når PG er nede (samme regel som fase-1 web→checker - ellers tar en PG-restart ned status-siden, stikk motsatt av poenget).
- *HVORDAN* C oppnår dette (beholde en liten in-memory siste-status som fallback vs annet) er C sin designbeslutning - tas i C-spec. Merk spenningen: du valgte "drop in-memory, DB-backed" for C, men instances:1 gjør et rent DB-backed nå-status-lese sårbart for PG-restart. C-brainstorm må løse dette eksplisitt (f.eks. behold in-memory KUN som nå-status-fallback, PG for historikk).

## Verifisering (DoD)

- CNPG-operator Running i `cnpg-system`; CRD installert.
- `status-db` Cluster `Cluster in healthy state`; PGDATA + WAL PVC-er bundet på Longhorn; `-rw`-service svarer.
- Secret `status-db-app` finnes med `uri`-key; en test-pod kobler med `psql "$uri"` og kan `CREATE TABLE` som `status`-bruker (ingen superuser).
- ArgoCD-sync grønn med `ServerSideApply=true`; prune av Cluster-CR er bevisst blokkert.
- (HA-bump senere: se under.)

## Oppgraderingssti til compute-HA (IKKE trivielt - eksplisitt liste)

`instances: 1 → 3` drar inn: synkron-vs-async replikering (async default = mulig tap av siste sekunders writes ved failover - ok for status-historikk, men bevisst valg); `affinity.enablePodAntiAffinity`/`topologySpreadConstraints` så de 3 ligger på 3 ulike noder (CLAUDE.md "replicas != HA"); PodDisruptionBudget (CNPG lager en, verifiser); `primaryUpdateStrategy`; og Longhorn↔CNPG dobbel-replikering (3 CNPG-instanser × 3-way Longhorn = 9 fysiske kopier → ved instances≥2 vil du sannsynligvis droppe PG-volumets Longhorn-replica-count til 1, siden CNPG selv replikerer). Tas når lab-testing av failover starter.

## Backup (parkert, men billig sikkerhetsnett notert)

Ingen offsite-backup nå (C eier ingen uerstattelig data; status-historikk er regenererbar). Billigste nett når ønsket: **Longhorn RecurringJob volume-snapshot** på status-db PVC (Longhorn finnes fra A) - crash-consistent PITR, ikke logisk pg_dump. CNPG barman/object-store er dag-2-oppgraderingen.

## Sikkerhet

- PG kun in-cluster (ingen IngressRoute). Bekreftet.
- `status-db-app`-secret = B→C trust boundary. **k3s krypterer IKKE etcd at rest by default** - secreten ligger base64 på 3 bokser. Rett-størrelse-fix: k3s `--secrets-encryption` (IKKE Sealed Secrets/ESO for én secret). Beslutning: aktiver, eller aksepter plaintext-i-etcd med `// ponytail:`-note.
- NetworkPolicy-intent (default-deny + allow checker→`status-db-rw:5432`) skrevet men `// ponytail:` aspirasjonell til Cilium lander (flannel håndhever ikke - fase-1 linje 185).

## Merknad: hvorfor PG når Grafana alt har historikk?

Fase 1 sender `target_up`/`target_latency_ms` til Prometheus (= uptime-historikk). PG-begrunnelsen: app-eid durable historikk, app-rendret i status-siden, uavhengig av otel-retention + uten å spørre Grafana per request. Bevisst dobbel-lagring.

## Referanser

- A (Longhorn): `docs/superpowers/specs/2026-06-29-longhorn-storage-design.md`. Fase 1: `docs/superpowers/specs/2026-06-28-status-page-design.md`.
- k3s-lab-plan `docs/superpowers/plans/2026-06-26-kongebra-k3s-lab.md` er **utdatert** (CX23/Image-Updater) - B skrives mot dagens CX33 + CI-push build-once-promote + per-app-per-env, ikke den planens apps-appset.
