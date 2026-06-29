# Idé: status-operator (auto-discovery av monitorerte tjenester)

Status: **AVLØST / lukket 2026-06-29**. Bygges IKKE. Det som finnes hyllevare: **home-operations/gatus-sidecar** (Go, watcher Traefik IngressRoute first-class + Ingress/Service/HTTPRoute, annotation `gatus.home-operations.com/enabled`, genererer Gatus-config via shared-volume atomic writes - aktivt vedlikeholdt). Status-stacken ble pivotert til Gatus + gatus-sidecar (begge hyllevare) - se `docs/superpowers/specs/2026-06-29-gatus-stack-design.md`. Denne idéen (custom k8s-operator) ville reimplementert en moden, vedlikeholdt ting → buy-over-build vant. Beholdt som dokumentasjon av beslutningen + mønsteret (annotation→config-generering) for et evt. genuint novelt fremtidig operator-prosjekt.
Dato: 2026-06-29.

## Problemet

status.newb.no fase 1 leser hvilke tjenester den skal probe fra en statisk `targets.yaml` (ConfigMap).
Hver ny tjeneste = manuelt legge til en linje. Hvis en tjeneste endrer URL/health-path/forsvinner, må noen huske å oppdatere ConfigMap-en. Det skalerer dårlig og driver mot drift.

## Idéen

En k8s-operator (controller) som lar hver tjeneste **deklarere selv** at den skal monitoreres, og som så holder status-appen konfigurert automatisk. Flagg tjenesten -> den dukker opp i status-siden. Endre/slett tjenesten -> operatoren fikser det. Ingen manuell targets-liste.

Dette er auto-discovery-alternativet som ble parkert i den opprinnelige discovery-beslutningen (vi valgte statisk ConfigMap for fase 1 bevisst, for å sende raskt).

## Den elegante vrien: utled targets fra IngressRoutes

Targets er per definisjon `https://<app>.newb.no`-domener. Og de domenene **defineres allerede** av `IngressRoute`-ene i clusteret (`Host(\`<app>.newb.no\`)`). Så operatoren trenger ikke at noen skriver URL-en på nytt - den kan lese den ut av IngressRoute-en som allerede finnes.

Mekanisme (foretrukket, minst maskineri):
- Annoter en eksisterende `IngressRoute` (eller `Service`):
  ```yaml
  metadata:
    annotations:
      status.newb.no/monitor: "true"
      status.newb.no/health-path: "/health"   # default /health hvis utelatt
  ```
- Operatoren watcher IngressRoutes med `status.newb.no/monitor: "true"`, henter host fra `Host()`-regelen + health-path fra annotation, og bygger target-listen.
- Reconcile-loop: ved enhver endring (ny/endret/slettet annotert IngressRoute) regenererer operatoren konfigurasjonen status-checker konsumerer.

Alternativ (mer eksplisitt, mer maskineri): en egen CRD `MonitoredService` (à la Prometheus `ServiceMonitor`). Typed + selvdokumenterende, men en CRD + mer RBAC + mer å vedlikeholde. Annotation-på-IngressRoute er den late, gjenbrukende varianten - start der.

## Hvordan operatoren mater status-checker

To muligheter (velg ved design):
1. **Operatoren skriver ConfigMap-en** som status-checker allerede leser. Minst endring på checker (den er uvitende om operatoren). Checker trenger reload-on-change (configMapGenerator-hash trigget av operatoren, eller fsnotify i checker).
2. **Checker spør k8s-API selv** (ingen egen operator) - enklere topologi, men blander discovery-logikk inn i checker og krever RBAC på checker. Operatoren holder ansvaret separat (renere).

Anbefaling: (1) - operatoren eier discovery, checker forblir dum og leser bare config. Bevarer fase 1-checkeren nesten urørt.

## Hvorfor verdt det (lab-mål)

- Å skrive en k8s-operator (controller-runtime / kubebuilder, reconcile-loop, watch/informers, RBAC, ev. CRD) er en kjerne-k8s-ferdighet labben ikke har øvd på ennå.
- Demonstrerer deklarativ drift: tjenester eier sin egen monitorerings-intent, ingen sentral liste å holde i sync.
- Naturlig "fase 5" på status.newb.no-roadmapen (etter state/incidents/varsling).

## Hvor ting havner når det bygges

- **Operator-kode:** `kongebra-apps/apps/status-operator/` (Go, controller-runtime). Egen app, egen modul, samme build-once-promote CI som andre apper.
- **CRD (hvis valgt) + RBAC + deployment:** `kongebra-gitops` (platform/ eller apps/status-operator/). Operatoren trenger ClusterRole for å watche IngressRoutes/Services på tvers av namespaces.
- **status-checker:** uendret hvis operatoren skriver ConfigMap-en (mulighet 1).

## Åpne spørsmål (avklar ved brainstorm senere)

- Annotation-på-IngressRoute vs egen CRD? (start annotation, vurder CRD hvis det vokser)
- Cross-namespace watch -> hvilken RBAC-scope? (ClusterRole, least-privilege read-only på IngressRoutes/Services)
- Hvordan trigge checker-reload når ConfigMap endres av operatoren?
- Hva med eksterne targets (grafana/argocd som ikke nødvendigvis har en annotert IngressRoute vi eier)? Behold en liten statisk fallback-liste i tillegg til auto-discovery?
- Hvordan unngå at operatoren og en menneskelig ConfigMap-endring slåss? (operatoren bør eie hele targets-ConfigMap-en eller et eget felt)

## Referanser

- status.newb.no fase 1: `docs/superpowers/specs/2026-06-28-status-page-design.md` (Roadmap-seksjonen), `docs/superpowers/plans/2026-06-28-status-page.md`.
- Prior art: Prometheus Operator `ServiceMonitor`/`PodMonitor` (annotation/CRD-drevet scrape-discovery) - samme grunnmønster.
