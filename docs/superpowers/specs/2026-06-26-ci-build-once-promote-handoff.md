# CI build-once-promote til k3s/ArgoCD - handoff

Dato: 2026-06-26.
Status: design besluttet (se ADR-0001 her + `kongebra-gitops` ADR-0003), ingen kode skrevet.
Formaal: gi en annen agent nok til aa implementere CI-siden av build-once-promote uten aa re-designe.

GitOps-siden er allerede LIVE i `kongebra-gitops` (per-app-per-env namespace, base/overlays, ApplicationSet, reflector).
Appene kjoerer naa paa en `newTag: latest`-bootstrap i overlay-ene; denne CI-jobben skal erstatte den med immutable SHA-tags.

## Naavaerende tilstand (det som skal endres)

- `.github/workflows/_build-deploy.yml`: gjenbrukbar workflow som (a) bygger image, (b) joiner tailnet, (c) peker **Dokploy** paa taggen og deployer via Dokploy-API.
  Dokploy-delen er LEGACY (gammel Swarm-homelab) og skal fjernes.
- `.github/workflows/go-hello-world.yml`: caller, trigges paa push til `apps/go-hello-world/**`, har allerede en `workflow_dispatch`-input `image_tag` for rollback.
- Tag-skjema som alt finnes og skal gjenbrukes: `TAG=$(date -u +%Y-%m-%d)-${GITHUB_SHA::7}` (lesbar, immutabel). Bygget pusher i dag `:latest`, `:<sha>` og `:<TAG>`.

## Maaltilstand

Per app, paa push til main:
1. Bygg `ghcr.io/kongebra/<app>:<TAG>` (behold det immutable date+sha-skjemaet) og push. `:latest` kan droppes (overlay pinner SHA naa).
2. **dev-jobb (auto):** skriv `<TAG>` inn i dev-overlayet i `kongebra-gitops`, commit + push. ArgoCD ruller dev.
3. **prod-jobb (gated):** `environment: production` med required reviewer. Paa godkjenning: skriv *samme* `<TAG>` inn i prod-overlayet, commit + push. ArgoCD ruller prod.

Ingen ny build for prod. Promotering = skrive samme tag til prod-overlay.

## GitOps write-kontrakt (eksakt)

Mot repo `git@github.com:kongebra/kongebra-gitops.git`, fil:
`apps/<app>/overlays/<env>/kustomization.yaml`, felt `images[].newTag`.

Skriv taggen trygt med kustomize (ikke sed) fra overlay-mappa:
```bash
cd apps/<app>/overlays/<env>
kustomize edit set image ghcr.io/kongebra/<app>=ghcr.io/kongebra/<app>:<TAG>
git add kustomization.yaml
git commit -m "promote <app> -> <env> (<TAG>)"
git pull --rebase && git push    # rebase for aa unngaa race ved samtidige app-pushes
```

`kongebra-gitops` main har INGEN branch protection (GitHub Pro-krav paa privat repo), saa direkte push til main fungerer. Direkte commit er bevisst: prod-kontrollen ligger i Environment-gaten, ikke i PR-review.

## Secrets som maa opprettes

- **Write deploy-key til kongebra-gitops:** lag en ny deploy-key paa `kongebra-gitops` med write-tilgang, legg privatdelen som Actions-secret i `kongebra-apps` (foreslaatt navn `GITOPS_DEPLOY_KEY`). Deploy-key er korrekt scopet til kun ett repo (smalere enn en PAT). ArgoCD sin eksisterende key forblir read-only.
- Dokploy- og tailnet-secrets (`DOKPLOY_*`, `TS_OAUTH_*`) trengs ikke lenger for deploy; kan fjernes fra bruken naar Dokploy-stegene er borte.

## GitHub Environment

Opprett Environment `production` i `kongebra-apps` med deg som required reviewer.
Prod-jobben maa ha `environment: production` (det er selve gaten - uten det er godkjenning bare pynt).

## Maalskisse for `_build-deploy.yml` (gjenbrukbar)

```yaml
on:
  workflow_call:
    inputs:
      app: { type: string, required: true }        # f.eks. go-hello-world
      app_dir: { type: string, required: true }    # apps/go-hello-world
      image: { type: string, required: true }      # ghcr.io/kongebra/go-hello-world
      image_tag: { type: string, default: "" }     # satt = rollback/promote eksisterende, hopp over build

jobs:
  build:
    # resolve TAG (date+sha eller input), bygg+push hvis ikke rollback. Output: tag.
  deploy-dev:
    needs: build
    # checkout kongebra-gitops (ssh-key: GITOPS_DEPLOY_KEY), kustomize edit set image i overlays/dev, commit+push
  deploy-prod:
    needs: deploy-dev
    environment: production        # <- required-reviewer-gaten
    # samme TAG, kustomize edit set image i overlays/prod, commit+push
```

Caller (`go-hello-world.yml`) sender `app`, `app_dir`, `image`, og evt. `image_tag` for rollback (input finnes alt).

## Rollback

- Re-kjoer workflowen med `workflow_dispatch` input `image_tag=<eldre-TAG>` (hopper build, promoterer eksisterende tag), eller
- `git revert` promote-committen i `kongebra-gitops`.
Umiddelbart, fordi imaget alt ligger i GHCR.

## Aapne valg for implementerende agent

1. Beholde `_build-deploy.yml` som reusable + per-app caller (anbefalt, matcher dagens monster), eller inline per app.
2. Concurrency-haandtering ved push til gitops: `git pull --rebase` + retry holder for lavt volum; vurder en `concurrency:`-group hvis mange apper pushes samtidig.
3. Skal dev og prod kunne promoteres til ulik tag samtidig (normalt ja - prod henger bak til godkjent). Bekreft at prod-jobben ikke auto-godkjennes.

## Verifisering (definition of done)

- Push til `apps/go-hello-world/**` bygger `:<TAG>`, skriver dev-overlay, ArgoCD ruller dev med ny tag (ikke `latest`).
- Prod-jobben venter paa manuell godkjenning; etter godkjenning star samme `<TAG>` i prod-overlay og ArgoCD ruller prod.
- `kubectl get deploy -n go-hello-world-prod -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'` viser `:<TAG>`, ikke `:latest`.
- Rollback via `image_tag`-input fungerer uten ny build.

## Referanser

- Denne repo: ADR-0001 (`docs/adr/0001-build-once-promote.md`).
- `kongebra-gitops`: ADR-0003, spec `docs/superpowers/specs/2026-06-26-multi-env-dev-prod-design.md`, plan `docs/superpowers/plans/2026-06-26-multi-env-dev-prod.md` (seksjon "Oppfoelging: kongebra-apps CI"), `SECRETS.md`.
