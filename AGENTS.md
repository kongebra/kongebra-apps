# newb.no monorepo

Self-hostede apper deployet til en privat Dokploy-instans (`dokploy.newb.no`, bak Tailscale).
Dette er en lab for å lære HA, multi-node, observability og drift - men koden skal holde produksjonskvalitet.

## Struktur

```
apps/<navn>/            # én app per mappe, eget go.mod (multi-module monorepo)
.github/workflows/
  _build-deploy.yml     # gjenbrukbar workflow (workflow_call): build -> GHCR -> Dokploy-deploy
  <app>.yml             # tynn per-app workflow, path-filtrert, kaller _build-deploy
```

Ny app: lag `apps/<navn>/`, kopier en eksisterende `<app>.yml` og bytt `app_dir`/`image`/`dokploy_app_id`.

## Teknologivalg

- **Backend: Go primært.** Python kun når et bibliotek er klart mye bedre for oppgaven (microservice da).
- **Frontend: TanStack Start**, eller enkel Vite + React hvis behovet er trivielt. **ALLTID avklar frontend-rammeverk med Svein før du velger** - ikke anta.
- **Image: distroless** (`gcr.io/distroless/static-debian12`) for Go. Ingen shell - health checks må være innebygd binær-flagg, ikke `curl`/`CMD-SHELL`.
- Hver Go-app er egen modul. Ingen delt root-`go.mod` med mindre vi bevisst innfører delte pakker.

## CI/CD (build-once, deploy via API)

- Build skjer i **GitHub Actions**, ikke på Dokploy-serveren. Image pushes til **GHCR**.
- Tre tags per image: `:latest`, `:<full-sha>`, `:<YYYY-MM-DD-shortsha>` (lesbar). Dokploy peker på den lesbare.
- Deploy: workflow joiner tailnet (Tailscale OAuth, `tag:ci`), kaller Dokploy-API (`application.update` for å sette image-tag, så `application.deploy`). Dokploy **puller** imaget - kloner ikke repoet.
- **Rollback:** `gh workflow run <app>.yml -f image_tag=<eksisterende-tag>` (bygger ikke, deployer eksisterende image).
- Pin alle tredjeparts-actions til full SHA (kommentar med versjon).
- **build-once-promote** (dev->prod): samme image-digest promoteres mellom miljøer, aldri rebuild per miljø.

## Konvensjoner

- Kode-identifikatorer på engelsk. URL-paths på engelsk (letter i18n/deploy). Kommentarer kan være norsk.
- Ikke `Co-Authored-By`-trailer i commits.
- Ikke em-dash i tekst; bruk vanlig bindestrek.
- Marker bevisste forenklinger med `// ponytail:`-kommentar (hva + oppgraderingssti).

## Lokal utvikling

- **Aspire (v13, polyglot)** for lokal orkestrering. AppHost authored i TypeScript (ikke C#). `aspire run` gir containerisert lokal dev + innebygd OTEL-dashboard. AppHost declarerer app + deps (Postgres/Redis) + relasjoner.
- Status: **spike-fase** - Go er Aspires minst modne integrasjon (community-toolkit/code-gen), så valider mønsteret før det blir standard for alle apper.
- Dokploy er ikke et førsteklasses Aspire-deploy-mål; prod går fortsatt via CI -> GHCR -> Dokploy. Aspire eier lokal dev, ikke prod-deploy (evt. emit compose-artefakt som bro).
- **Lokal Grafana-speiling:** AppHost inkluderer `grafana/otel-lgtm`-container lokalt (samme stack som prod-`monitoring`), så Grafana/Tempo/Prometheus-skills læres lokalt med full prod-paritet. App sender OTLP dit (`OTEL_EXPORTER_OTLP_ENDPOINT` -> lokal otel-lgtm). Aspires eget dashboard er bonus oppå.

## Infrastruktur (kontekst)

- 2-node Docker Swarm: manager `dokploy` + worker `swarm-worker-1` (begge x86, Docker 29.6.0, på tailnet).
- Hetzner Cloud Firewall `tailnet-only`: deny-all inbound utenom ICMP+UDP41641. SSH og alt annet via tailnet. Swarm-porter aldri offentlig.
- `*.newb.no` wildcard A -> tailnet-IP `100.120.49.73`. Traefik host-router. Alt tailnet-only by default.
- Observability: Dokploy-prosjekt `monitoring` kjører `grafana/otel-lgtm` (service `monitoring-otellgtm-qsiwan`). Grafana: `grafana.newb.no` (admin/admin). OTLP over overlay: `http://monitoring-otellgtm-qsiwan:4318`.
- OTEL-semconv: `host.name`=swarm-node (via `NODE_HOSTNAME={{.Node.Hostname}}`), `container.id`=replica.

## Driftsfeller (lært på den harde måten)

- **Ikke `apt install docker-ce` på manager** - restarter daemonen = nedetid (Dokploy kjører oppå den). Bruk `get.docker.com --version <x>` eller drain først.
- **Let's Encrypt HTTP-01 ⊥ tailnet-gjemt boks** - LE trenger offentlig port 80 (FW-blokkert). Bruk `.ts.net`-URL eller DNS-01/Tailscale Serve for cert.
- **Fine-grained PAT støtter ikke GHCR** - bruk classic PAT med `read:packages` for Dokploy registry-pull.
- **Replicas != HA** uten styrt plassering - bruk spread-preference `node.id`, ikke hard `Max/node`.

## Leksjoner

Læringsløpet er dokumentert som HTML-leksjoner i `~/vault/lessons/` (0001 baseline, 0002 CI/CD+swarm+observability). Oppdater når nye mønstre etableres.
