# Build-once-promote til k3s via GitOps

Status: accepted

CI bygger ett image per main-commit med en immutabel tag, deployer det automatisk til dev, og promoterer det til prod ved at den *samme* taggen skrives til prod-overlayet i `kongebra-gitops` bak en manuell GitHub Environment-gate.
Det erstatter den gamle Dokploy-deploy-flyten (`_build-deploy.yml` pekte Dokploy på taggen over tailnet): clusteret er nå k3s + ArgoCD, og deploy skjer ved at CI committer tag til gitops-repoet, ikke ved et API-kall mot deploy-targetet.
Build-once garanterer at prod kjører nøyaktig de bytene som ble testet i dev, gjør hvert deploy til en git-commit (revisjonshistorikk + triviell rollback), og holder prod bak et menneskelig godkjenningssteg.

## Consequences

- CI trenger skrivetilgang til `kongebra-gitops` (egen write deploy-key, ArgoCD sin key forblir read-only).
- Dokploy-stegene i `_build-deploy.yml` fjernes; tailnet-join trengs ikke lenger for deploy (push til GitHub holder).
- Speiler `kongebra-gitops` ADR-0003 (build-once-promote, CI-eide image-tags) fra GitOps-siden.
