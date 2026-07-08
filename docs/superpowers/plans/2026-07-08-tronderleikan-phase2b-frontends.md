# TrønderLeikan Phase 2b - frontends (web + admin) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy the two already-built TanStack Start BFF frontends (`web`, `admin`) to dev + prod (tailnet-only), by provisioning their Zitadel OIDC apps via the seed, wiring config/secrets, and adding gitops manifests.

**Architecture:** Extend the existing idempotent `zitadel-seed` (kongebra-apps, Go) to find-or-create two public PKCE OIDC apps and emit their `client_id`s. Run the seed locally against the cluster Zitadel, capture the ids, and land them (non-secret) in gitops ConfigMaps. Add web/admin Deployment/Service/IngressRoute to the shared `apps/tronderleikan` kustomize tree (kongebra-gitops); CI already promotes the image digests. `SESSION_SECRET` is the only real secret, created out-of-band.

**Tech Stack:** Go 1.x + `zitadel-go/v3` (seed), kustomize + ArgoCD + Traefik IngressRoute (gitops), openid-client BFF (frontends, already built), distroless nodejs22.

## Global Constraints

- Repo artifacts in English; point-in-time docs (specs/plans) may stay Norwegian. No em-dash, use plain hyphen. No `Co-Authored-By` trailer.
- The seed is idempotent: two runs produce identical end-state. Every new `Directory` op is either idempotent (`Ensure*`) or find-before-create; the `fakeDirectory` `Create*` FAILS on a second create, so idempotency is proven not assumed.
- Issuer/domain/client-id are NEVER hardcoded in seed code - always from env (SPEC §5). Redirect URIs come from env (comma-separated).
- OIDC apps are **public PKCE** clients: `AppType = OIDC_APP_TYPE_WEB`, `AuthMethodType = OIDC_AUTH_METHOD_TYPE_NONE`, `ResponseTypes = [CODE]`, `GrantTypes = [AUTHORIZATION_CODE, REFRESH_TOKEN]`, `AccessTokenType = JWT`, `AccessTokenRoleAssertion = true`, `DevMode = false`, `Version = OIDC_VERSION_1_0`.
- One OIDC app per frontend, spanning both envs (redirect URIs for dev AND prod on the one app). Same `client_id` in dev and prod.
- `client_id` is public (non-secret) -> ConfigMap is honest. `SESSION_SECRET` (>=32 chars) is secret -> out-of-band k8s Secret, documented in `SECRETS.md` + 1Password.
- Frontends: distroless `nodejs22-debian12:nonroot`, container `app`, port `3000`, `httpGet /healthz` probes, `runAsUser/Group/fsGroup 65532`, writable `/tmp` emptyDir (readOnlyRootFilesystem inherited from `_components/hardened-workload`).
- Hosts (single-label under `*.newb.no`): web `leikan.newb.no` / `leikan-dev.newb.no`; admin `leikan-admin.newb.no` / `leikan-admin-dev.newb.no`. admin serves under basePath `/admin`.
- Exposure: BOTH tailnet-only in this phase. No `_components/expose-public`. web-public is a deferred fast-follow.
- env-dev/env-prod components append `OTEL_RESOURCE_ATTRIBUTES` to `containers/0/env` via JSON-patch, so every base Deployment MUST define a non-empty `env:` array (both frontend deployments do).

---

## Phase A - seed OIDC-app support (kongebra-apps)

Working dir: `~/github/kongebra/kongebra-apps/apps/tronderleikan/zitadel-seed`. Run tests with `go test ./...` from that dir (the module is `saga-api`-style local; `go.work` wires it).

### Task A1: OIDC redirect-URI config from env

**Files:**
- Modify: `apps/tronderleikan/zitadel-seed/config.go`
- Test: `apps/tronderleikan/zitadel-seed/config_test.go`

**Interfaces:**
- Produces: `OIDCAppSpec{ Name string; RedirectURIs []string; PostLogoutURIs []string }`; `Config.OIDCApps []OIDCAppSpec`; env consts `EnvWebRedirectURIs="SEED_WEB_REDIRECT_URIS"`, `EnvWebPostLogoutURIs="SEED_WEB_POST_LOGOUT_URIS"`, `EnvAdminRedirectURIs="SEED_ADMIN_REDIRECT_URIS"`, `EnvAdminPostLogoutURIs="SEED_ADMIN_POST_LOGOUT_URIS"`. App names: `"tronderleikan-web"`, `"tronderleikan-admin"`.

- [ ] **Step 1: Write the failing test**

Add to `config_test.go`:

```go
func TestLoadConfigParsesOIDCApps(t *testing.T) {
	cfg, err := LoadConfig(envFrom(map[string]string{
		EnvAPIURL:              "https://auth.newb.no",
		EnvPAT:                 "tok",
		EnvTestPassword:        "Password1!",
		EnvWebRedirectURIs:     "https://leikan.newb.no/auth/callback, https://leikan-dev.newb.no/auth/callback",
		EnvWebPostLogoutURIs:   "https://leikan.newb.no/",
		EnvAdminRedirectURIs:   "https://leikan-admin.newb.no/admin/auth/callback",
		EnvAdminPostLogoutURIs: "https://leikan-admin.newb.no/admin",
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.OIDCApps) != 2 {
		t.Fatalf("want 2 OIDC apps, got %d", len(cfg.OIDCApps))
	}
	web := cfg.OIDCApps[0]
	if web.Name != "tronderleikan-web" || len(web.RedirectURIs) != 2 {
		t.Fatalf("web app parsed wrong: %+v", web)
	}
	if web.RedirectURIs[1] != "https://leikan-dev.newb.no/auth/callback" {
		t.Fatalf("whitespace not trimmed: %q", web.RedirectURIs[1])
	}
}

func TestLoadConfigSkipsOIDCAppWithoutRedirects(t *testing.T) {
	cfg, err := LoadConfig(envFrom(map[string]string{
		EnvAPIURL:       "https://auth.newb.no",
		EnvPAT:          "tok",
		EnvTestPassword: "Password1!",
		// no redirect-URI envs set
	}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.OIDCApps) != 0 {
		t.Fatalf("want 0 OIDC apps when no redirect URIs set, got %d", len(cfg.OIDCApps))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ -run TestLoadConfig -v`
Expected: FAIL - `cfg.OIDCApps` undefined / `EnvWebRedirectURIs` undefined.

- [ ] **Step 3: Implement config parsing**

In `config.go`, add to the env const block:

```go
	// OIDC-app redirect URIs (comma-separated). Empty -> that app is skipped
	// (local runs that don't need cluster apps stay unaffected). SPEC §5:
	// never hardcode URLs in code.
	EnvWebRedirectURIs     = "SEED_WEB_REDIRECT_URIS"
	EnvWebPostLogoutURIs   = "SEED_WEB_POST_LOGOUT_URIS"
	EnvAdminRedirectURIs   = "SEED_ADMIN_REDIRECT_URIS"
	EnvAdminPostLogoutURIs = "SEED_ADMIN_POST_LOGOUT_URIS"
```

Add the type + Config field:

```go
// OIDCAppSpec beskriver en OIDC-app som skal finnes i prosjektet. Public PKCE-
// klient; redirect-URIene kommer fra env (begge miljøer på samme app).
type OIDCAppSpec struct {
	Name           string
	RedirectURIs   []string
	PostLogoutURIs []string
}
```

Add `OIDCApps []OIDCAppSpec` to the `Config` struct. In `LoadConfig`, before `return cfg, nil` (after `cfg.Users = defaultUsers()`):

```go
	cfg.OIDCApps = oidcAppsFrom(getenv)
```

Add the helpers:

```go
// oidcAppsFrom bygger app-spec-ene fra env. En app med tomt redirect-sett
// utelates (skip), så et lokalt kjør uten cluster-URLer ikke feiler.
func oidcAppsFrom(getenv func(string) string) []OIDCAppSpec {
	var apps []OIDCAppSpec
	for _, a := range []struct {
		name, redirectEnv, logoutEnv string
	}{
		{"tronderleikan-web", EnvWebRedirectURIs, EnvWebPostLogoutURIs},
		{"tronderleikan-admin", EnvAdminRedirectURIs, EnvAdminPostLogoutURIs},
	} {
		redirects := splitCSV(getenv(a.redirectEnv))
		if len(redirects) == 0 {
			continue
		}
		apps = append(apps, OIDCAppSpec{
			Name:           a.name,
			RedirectURIs:   redirects,
			PostLogoutURIs: splitCSV(getenv(a.logoutEnv)),
		})
	}
	return apps
}

// splitCSV deler en komma-separert liste, trimmer whitespace, dropper tomme.
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ -run TestLoadConfig -v`
Expected: PASS (both new tests + existing config tests).

- [ ] **Step 5: Commit**

```bash
git add apps/tronderleikan/zitadel-seed/config.go apps/tronderleikan/zitadel-seed/config_test.go
git commit -m "feat(zitadel-seed): parse OIDC app redirect URIs from env"
```

### Task A2: Seeder ensures OIDC apps (idempotent + converge)

**Files:**
- Modify: `apps/tronderleikan/zitadel-seed/seed.go`
- Test: `apps/tronderleikan/zitadel-seed/seed_test.go`

**Interfaces:**
- Consumes: `Config.OIDCApps` (Task A1), `sameStringSet` (existing in seed.go).
- Produces: `Directory` gains `FindOIDCApp(ctx, orgID, projectID, name) (appID, clientID string, currentRedirects []string, found bool, err error)`, `CreateOIDCApp(ctx, orgID, projectID string, spec OIDCAppSpec) (appID, clientID string, err error)`, `UpdateOIDCApp(ctx, orgID, projectID, appID string, spec OIDCAppSpec) error`. `Result` gains `ClientIDs map[string]string` (app name -> client_id). `Seeder.ensureOIDCApp(ctx, orgID, projectID string, spec OIDCAppSpec) (clientID string, err error)`.

- [ ] **Step 1: Write the failing tests**

Add OIDC support to `fakeDirectory` in `seed_test.go` (add fields to the struct literal in `newFakeDirectory` too):

```go
// --- add to fakeDirectory struct ---
	oidcApps      map[string]string   // orgID/projectID/name -> appID
	oidcClientID  map[string]string   // appID -> clientID
	oidcRedirects map[string][]string // appID -> redirect URIs

// --- add to newFakeDirectory() return literal ---
		oidcApps: map[string]string{}, oidcClientID: map[string]string{}, oidcRedirects: map[string][]string{},

// --- add methods ---
func (f *fakeDirectory) FindOIDCApp(_ context.Context, orgID, projectID, name string) (string, string, []string, bool, error) {
	appID, ok := f.oidcApps[orgID+"/"+projectID+"/"+name]
	if !ok {
		return "", "", nil, false, nil
	}
	return appID, f.oidcClientID[appID], f.oidcRedirects[appID], true, nil
}

func (f *fakeDirectory) CreateOIDCApp(_ context.Context, orgID, projectID string, spec OIDCAppSpec) (string, string, error) {
	key := orgID + "/" + projectID + "/" + spec.Name
	if _, ok := f.oidcApps[key]; ok {
		return "", "", fmt.Errorf("NOT-IDEMPOTENT: OIDC app %q created twice", key)
	}
	appID := f.nextID("app")
	f.oidcApps[key] = appID
	f.oidcClientID[appID] = f.nextID("client") + "@tronderleikan"
	f.oidcRedirects[appID] = spec.RedirectURIs
	f.createdCount["oidcApp"]++
	return appID, f.oidcClientID[appID], nil
}

func (f *fakeDirectory) UpdateOIDCApp(_ context.Context, _, _, appID string, spec OIDCAppSpec) error {
	f.oidcRedirects[appID] = spec.RedirectURIs
	f.createdCount["oidcUpdate"]++
	return nil
}
```

Add tests:

```go
func TestSeedCreatesOIDCApps(t *testing.T) {
	fake := newFakeDirectory()
	cfg := testConfig()
	cfg.OIDCApps = []OIDCAppSpec{
		{Name: "tronderleikan-web", RedirectURIs: []string{"https://leikan.newb.no/auth/callback"}},
		{Name: "tronderleikan-admin", RedirectURIs: []string{"https://leikan-admin.newb.no/admin/auth/callback"}},
	}
	res, err := NewSeeder(fake, nil).Seed(context.Background(), cfg)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if fake.createdCount["oidcApp"] != 2 {
		t.Fatalf("want 2 OIDC apps created, got %d", fake.createdCount["oidcApp"])
	}
	if res.ClientIDs["tronderleikan-web"] == "" || res.ClientIDs["tronderleikan-admin"] == "" {
		t.Fatalf("client IDs not captured: %+v", res.ClientIDs)
	}
}

func TestSeedOIDCAppsIdempotentAndConverge(t *testing.T) {
	fake := newFakeDirectory()
	cfg := testConfig()
	cfg.OIDCApps = []OIDCAppSpec{
		{Name: "tronderleikan-web", RedirectURIs: []string{"https://leikan.newb.no/auth/callback", "https://leikan-dev.newb.no/auth/callback"}},
	}
	ctx := context.Background()

	res1, err := NewSeeder(fake, nil).Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	// Corrupt: drop one redirect URI so run 2 must converge.
	appID := fake.oidcApps["org-1/project-2/tronderleikan-web"]
	if appID == "" {
		// find it without hardcoding ids
		for k, v := range fake.oidcApps {
			if len(k) > 0 {
				appID = v
			}
		}
	}
	fake.oidcRedirects[appID] = []string{"https://leikan.newb.no/auth/callback"}

	res2, err := NewSeeder(fake, nil).Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("run 2 (idempotency broken): %v", err)
	}
	if fake.createdCount["oidcApp"] != 1 {
		t.Fatalf("app re-created on run 2: count=%d", fake.createdCount["oidcApp"])
	}
	if fake.createdCount["oidcUpdate"] != 1 {
		t.Fatalf("want exactly 1 converge-update, got %d", fake.createdCount["oidcUpdate"])
	}
	if !sameStringSet(fake.oidcRedirects[appID], cfg.OIDCApps[0].RedirectURIs) {
		t.Fatalf("redirects did not converge: %v", fake.oidcRedirects[appID])
	}
	if res1.ClientIDs["tronderleikan-web"] != res2.ClientIDs["tronderleikan-web"] {
		t.Fatalf("client_id changed between runs")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ -run 'TestSeed.*OIDC' -v`
Expected: FAIL - `Directory` has no `FindOIDCApp` (fake doesn't satisfy interface) / `Result.ClientIDs` undefined.

- [ ] **Step 3: Extend the Directory interface + Result + Seeder**

In `seed.go`, add to the `Directory` interface:

```go
	// FindOIDCApp returnerer appID, client_id og nåværende redirect-URIer, så
	// Seeder kan konvergere redirect-settet (ikke bare eksistensen).
	FindOIDCApp(ctx context.Context, orgID, projectID, name string) (appID, clientID string, currentRedirects []string, found bool, err error)
	CreateOIDCApp(ctx context.Context, orgID, projectID string, spec OIDCAppSpec) (appID, clientID string, err error)
	// UpdateOIDCApp setter redirect/post-logout-settet (brukes når det avviker).
	UpdateOIDCApp(ctx context.Context, orgID, projectID, appID string, spec OIDCAppSpec) error
```

Add `ClientIDs map[string]string` to the `Result` struct. In `Seed`, initialise it at the top next to `UserIDs`:

```go
	res := Result{UserIDs: map[string]string{}, ClientIDs: map[string]string{}}
```

After the users loop (step 6), before `return res, nil`, add step 7:

```go
	// 7. OIDC-apper (public PKCE) for frontendene. client_id fanges i Result.
	for _, app := range cfg.OIDCApps {
		clientID, err := s.ensureOIDCApp(ctx, platformOrgID, projectID, app)
		if err != nil {
			return res, fmt.Errorf("oidc app %s: %w", app.Name, err)
		}
		res.ClientIDs[app.Name] = clientID
		s.log("oidc app ensured: %s (client_id=%s)", app.Name, clientID)
	}
```

Add the method (mirrors `ensureProjectGrant`):

```go
func (s *Seeder) ensureOIDCApp(ctx context.Context, orgID, projectID string, spec OIDCAppSpec) (string, error) {
	appID, clientID, currentRedirects, found, err := s.dir.FindOIDCApp(ctx, orgID, projectID, spec.Name)
	if err != nil {
		return "", err
	}
	if found {
		if !sameStringSet(currentRedirects, spec.RedirectURIs) {
			if err := s.dir.UpdateOIDCApp(ctx, orgID, projectID, appID, spec); err != nil {
				return "", err
			}
			s.log("oidc app redirects converged: %s: %v -> %v", spec.Name, currentRedirects, spec.RedirectURIs)
		} else {
			s.log("oidc app exists: %s (%s)", spec.Name, appID)
		}
		return clientID, nil
	}
	_, clientID, err = s.dir.CreateOIDCApp(ctx, orgID, projectID, spec)
	if err != nil {
		return "", err
	}
	s.log("oidc app created: %s", spec.Name)
	return clientID, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./ -v`
Expected: PASS - all existing seed tests + the two new OIDC tests. (`TestSeedIdempotent` and `TestSeedCreatesExpectedState` still pass because `testConfig()` sets no OIDC env, so `cfg.OIDCApps` is empty and step 7 is a no-op.)

- [ ] **Step 5: Commit**

```bash
git add apps/tronderleikan/zitadel-seed/seed.go apps/tronderleikan/zitadel-seed/seed_test.go
git commit -m "feat(zitadel-seed): idempotently ensure OIDC apps, capture client_ids"
```

### Task A3: Zitadel adapter - OIDC app operations

**Files:**
- Modify: `apps/tronderleikan/zitadel-seed/directory_zitadel.go`

**Interfaces:**
- Consumes: `OIDCAppSpec` (A1), `Directory` OIDC methods (A2). Uses `zitadel-go/v3` `management` + `app` packages.
- Produces: concrete `zitadelDirectory` implementations of the three OIDC methods (no unit test - adapter is verified in the live idempotency run, per the file's existing convention).

- [ ] **Step 1: Add the app package import**

In `directory_zitadel.go` imports, add:

```go
	apppb "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/app"
```

- [ ] **Step 2: Implement the three methods**

Append to `directory_zitadel.go` (before the `var _ Directory` line):

```go
func (d *zitadelDirectory) FindOIDCApp(ctx context.Context, orgID, projectID, name string) (string, string, []string, bool, error) {
	resp, err := d.api.ManagementService().ListApps(inOrg(ctx, orgID), &managementpb.ListAppsRequest{
		ProjectId: projectID,
		Queries: []*apppb.AppQuery{{
			Query: &apppb.AppQuery_NameQuery{NameQuery: &apppb.AppNameQuery{
				Name:   name,
				Method: objectpb.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS,
			}},
		}},
	})
	if err != nil {
		return "", "", nil, false, fmt.Errorf("list apps: %w", err)
	}
	for _, a := range resp.GetResult() {
		if a.GetName() != name {
			continue
		}
		oidc := a.GetOidcConfig()
		return a.GetId(), oidc.GetClientId(), oidc.GetRedirectUris(), true, nil
	}
	return "", "", nil, false, nil
}

func (d *zitadelDirectory) CreateOIDCApp(ctx context.Context, orgID, projectID string, spec OIDCAppSpec) (string, string, error) {
	resp, err := d.api.ManagementService().AddOIDCApp(inOrg(ctx, orgID), oidcAppRequest(projectID, spec))
	if err != nil {
		return "", "", fmt.Errorf("add oidc app: %w", err)
	}
	return resp.GetAppId(), resp.GetClientId(), nil
}

func (d *zitadelDirectory) UpdateOIDCApp(ctx context.Context, orgID, projectID, appID string, spec OIDCAppSpec) error {
	_, err := d.api.ManagementService().UpdateOIDCAppConfig(inOrg(ctx, orgID), &managementpb.UpdateOIDCAppConfigRequest{
		ProjectId:                projectID,
		AppId:                    appID,
		RedirectUris:             spec.RedirectURIs,
		PostLogoutRedirectUris:   spec.PostLogoutURIs,
		ResponseTypes:            []apppb.OIDCResponseType{apppb.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
		GrantTypes:               []apppb.OIDCGrantType{apppb.OIDCGrantType_OIDC_GRANT_TYPE_AUTHORIZATION_CODE, apppb.OIDCGrantType_OIDC_GRANT_TYPE_REFRESH_TOKEN},
		AppType:                  apppb.OIDCAppType_OIDC_APP_TYPE_WEB,
		AuthMethodType:           apppb.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE,
		AccessTokenType:          apppb.OIDCTokenType_OIDC_TOKEN_TYPE_JWT,
		AccessTokenRoleAssertion: true,
		DevMode:                  false,
	})
	if err != nil {
		return fmt.Errorf("update oidc app: %w", err)
	}
	return nil
}

// oidcAppRequest bygger en public PKCE web-app-request (SPEC §10, Phase 2b plan
// Global Constraints). Auth method NONE = ingen client secret; Code + PKCE.
func oidcAppRequest(projectID string, spec OIDCAppSpec) *managementpb.AddOIDCAppRequest {
	return &managementpb.AddOIDCAppRequest{
		ProjectId:                projectID,
		Name:                     spec.Name,
		RedirectUris:             spec.RedirectURIs,
		PostLogoutRedirectUris:   spec.PostLogoutURIs,
		ResponseTypes:            []apppb.OIDCResponseType{apppb.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
		GrantTypes:               []apppb.OIDCGrantType{apppb.OIDCGrantType_OIDC_GRANT_TYPE_AUTHORIZATION_CODE, apppb.OIDCGrantType_OIDC_GRANT_TYPE_REFRESH_TOKEN},
		AppType:                  apppb.OIDCAppType_OIDC_APP_TYPE_WEB,
		AuthMethodType:           apppb.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE,
		Version:                  apppb.OIDCVersion_OIDC_VERSION_1_0,
		AccessTokenType:          apppb.OIDCTokenType_OIDC_TOKEN_TYPE_JWT,
		AccessTokenRoleAssertion: true,
		DevMode:                  false,
	}
}
```

- [ ] **Step 3: Build to verify it compiles**

Run: `go build ./...`
Expected: no output (success). Then `go test ./ -v` - still all PASS (adapter has no unit test; the fake drives the Seeder tests).

- [ ] **Step 4: Commit**

```bash
git add apps/tronderleikan/zitadel-seed/directory_zitadel.go
git commit -m "feat(zitadel-seed): zitadel-go adapter for OIDC app create/find/update"
```

### Task A4: main logs captured client_ids

**Files:**
- Modify: `apps/tronderleikan/zitadel-seed/main.go`

**Interfaces:**
- Consumes: `Result.ClientIDs` (A2).

- [ ] **Step 1: Read main.go to find where Result is printed**

Run: `sed -n '1,80p' apps/tronderleikan/zitadel-seed/main.go`
Locate where `Seed(...)` returns `res` and where the summary is logged.

- [ ] **Step 2: Add client-id logging**

After the seed result is obtained (where other Result fields like `ProjectID` are logged), add a clearly-marked block so the operator can copy the ids from stdout:

```go
	for name, clientID := range res.ClientIDs {
		log.Printf("CAPTURE client_id: %s = %s", name, clientID)
	}
```

(Match the existing logger; if `main.go` uses a `log(format, args...)` closure like the seeder, use that instead of `log.Printf`.)

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add apps/tronderleikan/zitadel-seed/main.go
git commit -m "feat(zitadel-seed): log captured OIDC client_ids for gitops wiring"
```

**Push Phase A:** `git push origin main` (or hand to Svein if the classifier blocks it). No CI/deploy impact - the seed has no cluster deployment.

---

## Phase B - provision + secrets (ops, run against the live cluster)

These are operational steps, not code. Do them from a tailnet-connected machine with `KUBECONFIG=~/.kube/kongebra-config` and the Zitadel IAM PAT (`zitadel/iam-admin-pat` secret, or from 1Password).

### Task B1: Run the extended seed, capture client_ids

- [ ] **Step 1: Get the IAM PAT**

```bash
export KUBECONFIG=~/.kube/kongebra-config
PAT=$(kubectl -n zitadel get secret iam-admin-pat -o jsonpath='{.data.pat}' | base64 -d)
```

- [ ] **Step 2: Run the seed with redirect URIs for both apps (both envs)**

```bash
cd ~/github/kongebra/kongebra-apps/apps/tronderleikan/zitadel-seed
ZITADEL_API_URL=https://auth.newb.no \
ZITADEL_PAT="$PAT" \
SEED_TEST_PASSWORD='<from 1Password: SEED_TEST_PASSWORD>' \
SEED_WEB_REDIRECT_URIS='https://leikan.newb.no/auth/callback,https://leikan-dev.newb.no/auth/callback' \
SEED_WEB_POST_LOGOUT_URIS='https://leikan.newb.no/,https://leikan-dev.newb.no/' \
SEED_ADMIN_REDIRECT_URIS='https://leikan-admin.newb.no/admin/auth/callback,https://leikan-admin-dev.newb.no/admin/auth/callback' \
SEED_ADMIN_POST_LOGOUT_URIS='https://leikan-admin.newb.no/admin,https://leikan-admin-dev.newb.no/admin' \
go run .
```

- [ ] **Step 3: Capture the two client_ids**

Expected: two `CAPTURE client_id:` lines in stdout - `tronderleikan-web = <id>` and `tronderleikan-admin = <id>`. Record both. Re-running is safe (idempotent - same ids, converges redirect URIs).

### Task B2: Create SESSION_SECRETs, document

- [ ] **Step 1: Generate + create the 4 secrets**

```bash
export KUBECONFIG=~/.kube/kongebra-config
for ns in tronderleikan-dev tronderleikan-prod; do
  kubectl -n "$ns" create secret generic tronderleikan-web-session \
    --from-literal=SESSION_SECRET="$(openssl rand -hex 32)"
  kubectl -n "$ns" create secret generic tronderleikan-admin-session \
    --from-literal=SESSION_SECRET="$(openssl rand -hex 32)"
done
```

Note: namespaces `tronderleikan-dev` / `-prod` already exist (backends run there). `openssl rand -hex 32` = 64 chars, satisfies the >=32 check.

- [ ] **Step 2: Document in SECRETS.md + 1Password**

Add a `SESSION_SECRET` section to `~/github/kongebra/kongebra-gitops/SECRETS.md` noting: out-of-band (not in git), 4 secrets `tronderleikan-{web,admin}-session` in `tronderleikan-{dev,prod}`, key `SESSION_SECRET`, mirror the actual values into 1Password. Same out-of-band pattern as `zitadel-masterkey`.

```bash
cd ~/github/kongebra/kongebra-gitops
# edit SECRETS.md, then:
git add SECRETS.md
git commit -m "docs(secrets): tronderleikan frontend SESSION_SECRETs (out-of-band)"
```

---

## Phase C - gitops manifests (kongebra-gitops)

Working dir: `~/github/kongebra/kongebra-gitops`. After each commit, ArgoCD picks it up within ~3 min (or the change is verified in Phase D). The web/admin image entries already exist in both overlays' `images:` lists (CI promotes them).

### Task C1: base web Deployment + Service

**Files:**
- Create: `apps/tronderleikan/base/web-deployment.yaml`
- Create: `apps/tronderleikan/base/web-service.yaml`

- [ ] **Step 1: Create the Service**

`apps/tronderleikan/base/web-service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  selector:
    app: web
  ports:
    - name: http
      port: 3000
      targetPort: http
```

- [ ] **Step 2: Create the Deployment**

`apps/tronderleikan/base/web-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      # distroless nodejs22 :nonroot runs as 65532; pin it (hardened-workload sets
      # runAsNonRoot but deliberately not the uid).
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
      containers:
        - name: app
          image: ghcr.io/kongebra/tronderleikan-web
          ports:
            - name: http
              containerPort: 3000
          envFrom:
            # AUTH_ISSUER + AUTH_AUDIENCE (shared, per-overlay ConfigMap).
            - configMapRef:
                name: tronderleikan-auth
            # AUTH_CLIENT_ID (base ConfigMap, Task C3).
            - configMapRef:
                name: tronderleikan-web-oidc
          env:
            - name: PORT
              value: "3000"
            - name: PLATFORM_URL
              value: http://platform:8080
            - name: ROSTER_URL
              value: http://roster:8080
            - name: COMPETITION_URL
              value: http://competition:8080
            - name: SESSION_SECRET
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-web-session
                  key: SESSION_SECRET
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 10
            periodSeconds: 15
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          # readOnlyRootFilesystem (from hardened-workload) -> node needs a writable /tmp.
          volumeMounts:
            - name: tmp
              mountPath: /tmp
          resources:
            requests:
              cpu: 50m
              memory: 96Mi
            limits:
              memory: 256Mi
      volumes:
        - name: tmp
          emptyDir: {}
```

- [ ] **Step 3: Commit** (kustomization wiring happens in C3, so this is staged now, built later)

```bash
git add apps/tronderleikan/base/web-deployment.yaml apps/tronderleikan/base/web-service.yaml
git commit -m "feat(tronderleikan): base web Deployment + Service"
```

### Task C2: base admin Deployment + Service

**Files:**
- Create: `apps/tronderleikan/base/admin-deployment.yaml`
- Create: `apps/tronderleikan/base/admin-service.yaml`

- [ ] **Step 1: Create the Service**

`apps/tronderleikan/base/admin-service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: admin
spec:
  selector:
    app: admin
  ports:
    - name: http
      port: 3000
      targetPort: http
```

- [ ] **Step 2: Create the Deployment**

`apps/tronderleikan/base/admin-deployment.yaml` (same shape as web, but only `PLATFORM_URL`, admin OIDC ConfigMap, admin session secret; note the `/healthz` path is NOT under the `/admin` basePath - nitro serves the healthz route at root):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: admin
spec:
  replicas: 1
  selector:
    matchLabels:
      app: admin
  template:
    metadata:
      labels:
        app: admin
    spec:
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
      containers:
        - name: app
          image: ghcr.io/kongebra/tronderleikan-admin
          ports:
            - name: http
              containerPort: 3000
          envFrom:
            - configMapRef:
                name: tronderleikan-auth
            - configMapRef:
                name: tronderleikan-admin-oidc
          env:
            - name: PORT
              value: "3000"
            - name: PLATFORM_URL
              value: http://platform:8080
            - name: SESSION_SECRET
              valueFrom:
                secretKeyRef:
                  name: tronderleikan-admin-session
                  key: SESSION_SECRET
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 10
            periodSeconds: 15
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          volumeMounts:
            - name: tmp
              mountPath: /tmp
          resources:
            requests:
              cpu: 50m
              memory: 96Mi
            limits:
              memory: 256Mi
      volumes:
        - name: tmp
          emptyDir: {}
```

- [ ] **Step 3: Verify the `/healthz` path assumption**

Run: `grep -rn "healthz" ~/github/kongebra/kongebra-apps/apps/tronderleikan/admin/src ~/github/kongebra/kongebra-apps/apps/tronderleikan/admin/vite.config.ts`
Expected: a `/healthz` route registered at root (the web Dockerfile comment says the probe hits `/healthz`). If admin only exposes it under `/admin/healthz`, change both probe paths to `/admin/healthz`. Fix inline before committing.

- [ ] **Step 4: Commit**

```bash
git add apps/tronderleikan/base/admin-deployment.yaml apps/tronderleikan/base/admin-service.yaml
git commit -m "feat(tronderleikan): base admin Deployment + Service"
```

### Task C3: base OIDC ConfigMaps + wire base kustomization

**Files:**
- Create: `apps/tronderleikan/base/web-oidc-config.yaml`
- Create: `apps/tronderleikan/base/admin-oidc-config.yaml`
- Modify: `apps/tronderleikan/base/kustomization.yaml`

- [ ] **Step 1: Create the ConfigMaps** (fill in the real ids captured in Task B1)

`apps/tronderleikan/base/web-oidc-config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tronderleikan-web-oidc
data:
  # Public PKCE client_id (non-secret) for tronderleikan-web, from the zitadel-seed
  # run (Phase 2b plan Task B1). Same id in dev + prod (one app spans both envs).
  # Re-seeding a fresh Zitadel changes it -> update here + re-sync.
  AUTH_CLIENT_ID: "<WEB_CLIENT_ID_FROM_SEED>"
```

`apps/tronderleikan/base/admin-oidc-config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tronderleikan-admin-oidc
data:
  AUTH_CLIENT_ID: "<ADMIN_CLIENT_ID_FROM_SEED>"
```

- [ ] **Step 2: Add all four base files to resources**

In `apps/tronderleikan/base/kustomization.yaml`, extend `resources:` (after the competition lines):

```yaml
  # Phase 2b frontends (SPEC 2026-07-08).
  - web-deployment.yaml
  - web-service.yaml
  - web-oidc-config.yaml
  - admin-deployment.yaml
  - admin-service.yaml
  - admin-oidc-config.yaml
```

- [ ] **Step 3: Build the base to verify it resolves**

Run: `kustomize build apps/tronderleikan/base >/dev/null && echo OK`
Expected: `OK` (no missing-resource errors). Deployments reference ConfigMaps/Secrets by name - kustomize does not fail on Secret refs not present in the tree.

- [ ] **Step 4: Commit**

```bash
git add apps/tronderleikan/base/web-oidc-config.yaml apps/tronderleikan/base/admin-oidc-config.yaml apps/tronderleikan/base/kustomization.yaml
git commit -m "feat(tronderleikan): base OIDC client_id ConfigMaps + wire frontends into base"
```

### Task C4: dev overlay - IngressRoutes + base-URL patches

**Files:**
- Modify: `apps/tronderleikan/overlays/dev/ingressroute.yaml`
- Create: `apps/tronderleikan/overlays/dev/frontend-baseurl.yaml` (patch)
- Modify: `apps/tronderleikan/overlays/dev/kustomization.yaml`

- [ ] **Step 1: Inspect the existing dev IngressRoute to match its style**

Run: `cat apps/tronderleikan/overlays/dev/ingressroute.yaml`
Note the entryPoints/tls form used by the backend routes so the new ones match exactly.

- [ ] **Step 2: Add web + admin IngressRoutes**

Append to `apps/tronderleikan/overlays/dev/ingressroute.yaml` (adjust `kind`/apiVersion to match what is already in the file):

```yaml
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: web
spec:
  entryPoints:
    - websecure
  routes:
    - kind: Rule
      match: Host(`leikan-dev.newb.no`)
      services:
        - name: web
          port: 3000
  tls: {}
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: admin
spec:
  entryPoints:
    - websecure
  routes:
    - kind: Rule
      match: Host(`leikan-admin-dev.newb.no`)
      services:
        - name: admin
          port: 3000
  tls: {}
```

- [ ] **Step 3: Create the base-URL patch**

`apps/tronderleikan/overlays/dev/frontend-baseurl.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  template:
    spec:
      containers:
        - name: app
          env:
            - name: WEB_BASE_URL
              value: https://leikan-dev.newb.no
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: admin
spec:
  template:
    spec:
      containers:
        - name: app
          env:
            - name: ADMIN_BASE_URL
              value: https://leikan-admin-dev.newb.no/admin
```

- [ ] **Step 4: Wire the patch into the overlay**

In `apps/tronderleikan/overlays/dev/kustomization.yaml`, add (after `resources:` / alongside existing patches - add a `patches:` block if none exists):

```yaml
patches:
  - path: frontend-baseurl.yaml
```

- [ ] **Step 5: Build the dev overlay**

Run: `kustomize build apps/tronderleikan/overlays/dev >/dev/null && echo OK`
Expected: `OK`. If the env-dev component's JSON-patch to `containers/0/env` errors, confirm both frontend base Deployments have a non-empty `env:` array (they do).

- [ ] **Step 6: Commit**

```bash
git add apps/tronderleikan/overlays/dev/ingressroute.yaml apps/tronderleikan/overlays/dev/frontend-baseurl.yaml apps/tronderleikan/overlays/dev/kustomization.yaml
git commit -m "feat(tronderleikan): dev overlay IngressRoutes + base-URL for web/admin"
```

### Task C5: prod overlay - IngressRoutes + base-URL patches

**Files:**
- Modify: `apps/tronderleikan/overlays/prod/ingressroute.yaml`
- Create: `apps/tronderleikan/overlays/prod/frontend-baseurl.yaml`
- Modify: `apps/tronderleikan/overlays/prod/kustomization.yaml`

- [ ] **Step 1: Add web + admin IngressRoutes (prod hosts)**

Append to `apps/tronderleikan/overlays/prod/ingressroute.yaml` - identical to C4 Step 2 but with prod hosts: `Host(`leikan.newb.no`)` for web, `Host(`leikan-admin.newb.no`)` for admin.

- [ ] **Step 2: Create the prod base-URL patch**

`apps/tronderleikan/overlays/prod/frontend-baseurl.yaml` - identical to C4 Step 3 but: web `value: https://leikan.newb.no`, admin `value: https://leikan-admin.newb.no/admin`.

- [ ] **Step 3: Wire the patch**

In `apps/tronderleikan/overlays/prod/kustomization.yaml`, add:

```yaml
patches:
  - path: frontend-baseurl.yaml
```

- [ ] **Step 4: Build the prod overlay**

Run: `kustomize build apps/tronderleikan/overlays/prod >/dev/null && echo OK`
Expected: `OK`.

- [ ] **Step 5: Commit + push**

```bash
git add apps/tronderleikan/overlays/prod/ingressroute.yaml apps/tronderleikan/overlays/prod/frontend-baseurl.yaml apps/tronderleikan/overlays/prod/kustomization.yaml
git commit -m "feat(tronderleikan): prod overlay IngressRoutes + base-URL for web/admin"
git push origin main   # or hand to Svein if the classifier blocks the push
```

---

## Phase D - end-to-end verification (tailnet)

Run from a tailnet-connected machine with a browser. Verify dev before prod.

### Task D1: Verify ArgoCD sync + pods

- [ ] **Step 1: Confirm ArgoCD synced the new resources**

```bash
export KUBECONFIG=~/.kube/kongebra-config
kubectl -n argocd get app app-tronderleikan-dev app-tronderleikan-prod
```

Expected: both `Synced/Healthy`. If stale after ~3 min, it will reconcile on its own (do not force-refresh prod).

- [ ] **Step 2: Confirm the frontend pods are running**

```bash
for ns in tronderleikan-dev tronderleikan-prod; do echo "== $ns =="; kubectl -n "$ns" get pods -l 'app in (web,admin)'; done
```

Expected: `web-*` and `admin-*` pods `1/1 Running` in both namespaces. If `CreateContainerConfigError` ("image will run as root") appears, the uid pin is wrong - re-check `runAsUser: 65532`.

### Task D2: Verify login flow (dev, then prod)

- [ ] **Step 1: web dev login**

In a browser on the tailnet, open `https://leikan-dev.newb.no`. Click login. Expected: redirect to `auth.newb.no`, log in as a seeded user (e.g. `player@demo.tronderleikan.local` / `SEED_TEST_PASSWORD`), get redirected back to `leikan-dev.newb.no/auth/callback`, land authenticated with a role-gated view rendering. Confirm no redirect_uri-mismatch error (that means the seed's redirect URIs are wrong).

- [ ] **Step 2: admin dev login**

Open `https://leikan-admin-dev.newb.no/admin`. Same flow; log in as `platform-admin@tronderleikan.local`. Expected: authenticated admin view.

- [ ] **Step 3: prod login (both)**

Repeat Steps 1-2 against `https://leikan.newb.no` and `https://leikan-admin.newb.no/admin`.

- [ ] **Step 4: Update the handoff doc**

Note the Phase 2b outcome (client_ids captured, secrets created, frontends live dev+prod, login verified) in a new handoff or by appending to `docs/superpowers/HANDOFF-tronderleikan-2026-07-08.md`. Mark web-public as the remaining fast-follow.

---

## Self-Review notes

- **Spec coverage:** OIDC provisioning (A1-A4, B1), SESSION_SECRET (B2), client-id ConfigMaps (C3), web/admin manifests (C1-C2, C4-C5), tailnet-only/no expose-public (C4-C5 IngressRoutes), E2E (D1-D2). Deferred items (web-public, Gatus, root->/admin redirect) intentionally out of scope.
- **Known verify-at-impl points (flagged inline, not placeholders):** admin `/healthz` path vs basePath (C2 Step 3); exact apiVersion/kind + patch-block presence in the existing overlay files (C4 Step 1, C4 Step 4). These are "match the file you find" instructions with a concrete fallback, not unspecified work.
- **Type consistency:** `OIDCAppSpec` fields (`Name`/`RedirectURIs`/`PostLogoutURIs`), `Directory` method signatures, and `Result.ClientIDs` are used identically across A1/A2/A3 and the fake.
