# status.newb.no Implementation Plan (fase 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bygg en status-side (`status.newb.no`) som viser opp/ned + responstid for konfigurerte tjenester, som app #2 på k3s-laben og som mal for Go + TanStack Start-apper.

**Architecture:** To tjenester. `status-checker` (Go, distroless) poller domene-URL-er (`*.newb.no`) på en ticker, holder siste status i minne (atomic snapshot), eksponerer JSON-API in-cluster. `status-web` (TanStack Start, Node SSR) henter via én `createServerFn` (SSR + client-refresh) og rendrer grid, eneste tjeneste med IngressRoute.

**Tech Stack:** Go 1.23+ (stdlib net/http, OTEL), TanStack Start (React, Vite, Nitro node-server), distroless (checker) / node-slim (web), GHCR, kustomize/ArgoCD (gitops), eksisterende build-once-promote CI.

## Global Constraints

- Kode-identifikatorer + URL-paths på engelsk. Kommentarer kan være norsk. Ingen em-dash (bruk `-`). Ingen `Co-Authored-By`-trailer.
- Pin alle tredjeparts-actions til full SHA (kommentar med versjon).
- Marker bevisste forenklinger med `// ponytail:` (hva + oppgraderingssti).
- Hver Go-app er egen modul (eget `go.mod`), ingen delt root-`go.mod`.
- OTEL gjenbrukes EKSAKT fra `apps/go-hello-world/main.go`: `semconv/v1.26.0`, no-op når `OTEL_EXPORTER_OTLP_ENDPOINT` tom, SIGTERM-flush, otelhttp auto-spans, k8s-semconv resource (`k8s.pod.name` fra hostname; `k8s.node.name`/`k8s.namespace.name` fra `NODE_NAME`/`POD_NAMESPACE` Downward API).
- checker prober KUN `https://<app>.newb.no` (domene), aldri in-cluster service-DNS. Web→checker går over in-cluster service-DNS.
- PR-workflows: `on: pull_request` (ALDRI `pull_request_target`), `permissions: contents: read`, ingen `secrets: inherit`, `docker-build push=false`. Tester per-app i caller.
- Image-tag-skjema + digest-pin + prod-gate håndteres av eksisterende `_build-deploy.yml` / `docker-build` / `gitops-promote`. Ikke reimplementer.
- checker intervall = 30s, timeout = 5s, som hardkodede konstanter (`// ponytail:` oppgraderingssti til env hvis nødvendig).

---

## Task 0: Verifiser CoreDNS-resolusjon av `*.newb.no` fra en pod (BLOKKERENDE, manuell)

**Hvorfor:** checker prober domene-URL-er. Pods bruker CoreDNS, ikke tailnet MagicDNS. Hvis `*.newb.no` ikke resolver inni en pod, har checker ingenting å probe - hele appen er nytteløs. Dette MÅ verifiseres før Go-koden bygges. Krever cluster-tilgang (kubeconfig over tailnet) som ikke er tilgjengelig fra CI/agent - utføres manuelt av operatør.

- [ ] **Step 1: Test resolusjon + nåbarhet fra en throwaway-pod**

```bash
KUBECONFIG=~/.kube/kongebra-config kubectl run dns-test --rm -it --restart=Never \
  --image=curlimages/curl -- \
  curl -sS -o /dev/null -w '%{http_code}\n' https://go-hello-world.newb.no/health
```
Expected: `200`. Hvis `000`/timeout/DNS-feil → `*.newb.no` resolver ikke i pod.

- [ ] **Step 2: Hvis Step 1 feiler - konfigurer CoreDNS forward**

Legg `newb.no`-forward til tailnet-resolveren i CoreDNS (k3s: `kube-system/coredns` ConfigMap, `Corefile`). Eksempel-blokk:
```
newb.no:53 {
    forward . 100.100.100.100   # tailnet MagicDNS-resolver
}
```
Re-kjør Step 1 til `200`. (Dette er en cluster-endring i `kongebra-gitops` eller imperativ - dokumenter valget.)

- [ ] **Step 3: Bekreft før du fortsetter**

Ikke start Task 1 før Step 1 returnerer `200`. Noter resultatet i PR-en.

---

## Task 1: status-checker - modul + config-lasting

**Files:**
- Create: `apps/status-checker/go.mod`
- Create: `apps/status-checker/config.go`
- Test: `apps/status-checker/config_test.go`

**Interfaces:**
- Produces: `type Target struct { Name, URL, HealthPath string }`, `type Config struct { Targets []Target }`, `func loadConfig(path string) (*Config, error)`.

- [ ] **Step 1: Opprett modulen**

```bash
cd apps/status-checker
go mod init status-checker
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Skriv failing test** (`config_test.go`)

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "targets.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigValid(t *testing.T) {
	p := writeTemp(t, `
targets:
  - name: go-hello-world
    url: https://go-hello-world.newb.no
    health_path: /health
`)
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatalf("uventet feil: %v", err)
	}
	if len(cfg.Targets) != 1 || cfg.Targets[0].Name != "go-hello-world" {
		t.Fatalf("feil parse: %+v", cfg.Targets)
	}
	if cfg.Targets[0].HealthPath != "/health" {
		t.Errorf("health_path = %q", cfg.Targets[0].HealthPath)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := loadConfig("/nope/targets.yaml"); err == nil {
		t.Fatal("forventet feil for manglende fil")
	}
}

func TestLoadConfigEmptyTargets(t *testing.T) {
	p := writeTemp(t, "targets: []\n")
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatalf("tom liste skal ikke feile: %v", err)
	}
	if len(cfg.Targets) != 0 {
		t.Errorf("forventet 0 targets, fikk %d", len(cfg.Targets))
	}
}
```

- [ ] **Step 3: Kjør testen, verifiser at den feiler**

Run: `cd apps/status-checker && go test ./... -run TestLoadConfig`
Expected: FAIL (`undefined: loadConfig`).

- [ ] **Step 4: Implementer** (`config.go`)

```go
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Target er en tjeneste checker poller (kun domene-URL, aldri in-cluster service-DNS).
type Target struct {
	Name       string `yaml:"name"`
	URL        string `yaml:"url"`
	HealthPath string `yaml:"health_path"`
}

type Config struct {
	Targets []Target `yaml:"targets"`
}

// loadConfig leser og parser targets.yaml (montert ConfigMap). Fail-fast ved feil.
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("les config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	return &cfg, nil
}
```

- [ ] **Step 5: Kjør testen, verifiser PASS**

Run: `cd apps/status-checker && go test ./... -run TestLoadConfig -v`
Expected: PASS (3 tester).

- [ ] **Step 6: Commit**

```bash
git add apps/status-checker/go.mod apps/status-checker/go.sum apps/status-checker/config.go apps/status-checker/config_test.go
git commit -m "status-checker: config loading (targets.yaml, fail-fast)"
```

---

## Task 2: status-checker - en enkelt sjekk (Result, http-klient, reason)

**Files:**
- Create: `apps/status-checker/check.go`
- Test: `apps/status-checker/check_test.go`

**Interfaces:**
- Consumes: `Target` (Task 1).
- Produces:
  - `type Status string` med konstanter `StatusUp = "up"`, `StatusDown = "down"`, `StatusUnknown = "unknown"`.
  - `type Result struct { Name, URL string; Status Status; LatencyMs *int64; HTTPCode *int; Reason *string; LastChecked *time.Time }`
  - `func newHTTPClient() *http.Client`
  - `func check(ctx context.Context, client *http.Client, t Target) Result` (timeout settes av caller via ctx).

- [ ] **Step 1: Skriv failing test** (`check_test.go`)

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := check(context.Background(), newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/health"})
	if res.Status != StatusUp {
		t.Fatalf("status = %q, vil ha up", res.Status)
	}
	if res.LatencyMs == nil {
		t.Error("latency skal være satt ved up")
	}
	if res.HTTPCode == nil || *res.HTTPCode != 200 {
		t.Errorf("http_code = %v, vil ha 200", res.HTTPCode)
	}
	if res.Reason != nil {
		t.Errorf("reason skal være nil ved up, var %v", *res.Reason)
	}
}

func TestCheckDown5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := check(context.Background(), newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/"})
	if res.Status != StatusDown {
		t.Fatalf("status = %q, vil ha down", res.Status)
	}
	if res.Reason == nil || *res.Reason != "http_5xx" {
		t.Errorf("reason = %v, vil ha http_5xx", res.Reason)
	}
	if res.LatencyMs != nil {
		t.Error("latency skal være nil ved down")
	}
}

func TestCheckDownTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := check(ctx, newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/"})
	if res.Status != StatusDown {
		t.Fatalf("status = %q, vil ha down", res.Status)
	}
	if res.Reason == nil || *res.Reason != "timeout" {
		t.Errorf("reason = %v, vil ha timeout", res.Reason)
	}
}

func TestCheckRedirectFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := check(context.Background(), newHTTPClient(), Target{Name: "x", URL: srv.URL, HealthPath: "/redir"})
	if res.Status != StatusUp {
		t.Fatalf("status = %q, vil ha up (redirect fulgt)", res.Status)
	}
	if res.HTTPCode == nil || *res.HTTPCode != 200 {
		t.Errorf("http_code = %v, vil ha endelig 200", res.HTTPCode)
	}
}
```

- [ ] **Step 2: Kjør, verifiser FAIL**

Run: `cd apps/status-checker && go test ./... -run TestCheck`
Expected: FAIL (`undefined: check`).

- [ ] **Step 3: Implementer** (`check.go`)

```go
package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Status string

const (
	StatusUp      Status = "up"
	StatusDown    Status = "down"
	StatusUnknown Status = "unknown"
)

// Result er siste sjekk-resultat for en target. Pekere = nullable i JSON.
type Result struct {
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Status      Status     `json:"status"`
	LatencyMs   *int64     `json:"latency_ms"`
	HTTPCode    *int       `json:"http_code"`
	Reason      *string    `json:"reason"`
	LastChecked *time.Time `json:"last_checked"`
}

// newHTTPClient bygger EN delt klient. Timeout styres per-sjekk via context (ikke Client.Timeout).
// Eksplisitt Transport: default MaxIdleConnsPerHost=2 serialiserer parallelle sjekker mot samme host.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// check utfører en enkelt GET. Følger redirects (default). Endelig 2xx = up.
func check(ctx context.Context, client *http.Client, t Target) Result {
	res := Result{Name: t.Name, URL: t.URL}
	now := time.Now()
	res.LastChecked = &now

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL+t.HealthPath, nil)
	if err != nil {
		res.Status, res.Reason = StatusDown, ptr("other")
		return res
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusDown
		res.Reason = ptr(classifyErr(err))
		return res
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) // drain for connection-reuse
		resp.Body.Close()
	}()

	code := resp.StatusCode
	res.HTTPCode = &code
	lat := time.Since(start).Milliseconds()

	if code >= 200 && code < 300 {
		res.Status = StatusUp
		res.LatencyMs = &lat
	} else {
		res.Status = StatusDown
		if code >= 500 {
			res.Reason = ptr("http_5xx")
		} else if code >= 400 {
			res.Reason = ptr("http_4xx")
		} else {
			res.Reason = ptr("other")
		}
	}
	return res
}

// classifyErr mapper en transport-feil til en reason-streng.
func classifyErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "conn_refused"
	case strings.Contains(msg, "tls"), strings.Contains(msg, "certificate"):
		return "tls_error"
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "dns"):
		return "dns_error"
	default:
		return "other"
	}
}

func ptr[T any](v T) *T { return &v }
```

- [ ] **Step 4: Kjør, verifiser PASS**

Run: `cd apps/status-checker && go test ./... -run TestCheck -race -v`
Expected: PASS (4 tester).

- [ ] **Step 5: Commit**

```bash
git add apps/status-checker/check.go apps/status-checker/check_test.go
git commit -m "status-checker: single check (status/reason/latency, shared client, context timeout)"
```

---

## Task 3: status-checker - store + ticker (atomic snapshot, parallell fan-out)

**Files:**
- Create: `apps/status-checker/store.go`
- Test: `apps/status-checker/store_test.go`

**Interfaces:**
- Consumes: `Target`, `Result`, `check`, `newHTTPClient` (Task 1-2).
- Produces:
  - `type Store struct { ... }`, `func newStore() *Store`, `func (s *Store) snapshot() []Result`, `func (s *Store) set([]Result)`.
  - `type Checker struct { ... }`, `func newChecker(targets []Target, interval, timeout time.Duration) *Checker`, `func (c *Checker) runOnce(ctx context.Context)`, `func (c *Checker) Run(ctx context.Context)` (blokkerer til ctx done; kjører runOnce umiddelbart, så på ticker), `func (c *Checker) Snapshot() []Result`.

- [ ] **Step 1: Skriv failing test** (`store_test.go`)

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStoreSnapshotIsolation(t *testing.T) {
	s := newStore()
	s.set([]Result{{Name: "a", Status: StatusUp}})
	snap := s.snapshot()
	snap[0].Name = "mutated"
	if s.snapshot()[0].Name != "a" {
		t.Fatal("snapshot skal være isolert fra intern state")
	}
}

func TestCheckerRunOnceParallelAndUnknownBefore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newChecker([]Target{
		{Name: "a", URL: srv.URL, HealthPath: "/"},
		{Name: "b", URL: srv.URL, HealthPath: "/"},
	}, 30*time.Second, 5*time.Second)

	// Før første kjøring: alle unknown.
	for _, r := range c.Snapshot() {
		if r.Status != StatusUnknown {
			t.Fatalf("%s: forventet unknown før runOnce, fikk %s", r.Name, r.Status)
		}
	}

	c.runOnce(context.Background())
	snap := c.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("forventet 2 resultater, fikk %d", len(snap))
	}
	for _, r := range snap {
		if r.Status != StatusUp {
			t.Errorf("%s: status = %s, vil ha up", r.Name, r.Status)
		}
	}
}
```

- [ ] **Step 2: Kjør, verifiser FAIL**

Run: `cd apps/status-checker && go test ./... -run 'TestStore|TestChecker'`
Expected: FAIL (`undefined: newStore`).

- [ ] **Step 3: Implementer** (`store.go`)

```go
package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Store holder siste snapshot lock-fritt via atomic.Pointer. Lesere får et konsistent
// snapshot uten mutex på hot read-path; skriver bytter hele snapshotet per tick.
type Store struct {
	snap atomic.Pointer[[]Result]
}

func newStore() *Store {
	s := &Store{}
	empty := []Result{}
	s.snap.Store(&empty)
	return s
}

func (s *Store) set(results []Result) { s.snap.Store(&results) }

func (s *Store) snapshot() []Result {
	p := s.snap.Load()
	out := make([]Result, len(*p))
	copy(out, *p)
	return out
}

// Checker eier targets + en delt http.Client + en Store. Run poller på ticker.
type Checker struct {
	targets  []Target
	interval time.Duration
	timeout  time.Duration
	client   *http.Client
	store    *Store
}

func newChecker(targets []Target, interval, timeout time.Duration) *Checker {
	c := &Checker{
		targets:  targets,
		interval: interval,
		timeout:  timeout,
		client:   newHTTPClient(),
		store:    newStore(),
	}
	// Initial snapshot: alle unknown til første sjekk lander.
	init := make([]Result, len(targets))
	for i, t := range targets {
		init[i] = Result{Name: t.Name, URL: t.URL, Status: StatusUnknown}
	}
	c.store.set(init)
	return c
}

// runOnce sjekker alle targets parallelt og bytter snapshot atomisk når alle er ferdige.
func (c *Checker) runOnce(ctx context.Context) {
	results := make([]Result, len(c.targets))
	var wg sync.WaitGroup
	for i, t := range c.targets {
		wg.Add(1)
		go func(i int, t Target) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()
			results[i] = check(cctx, c.client, t) // hver goroutine skriver sin egen indeks
		}(i, t)
	}
	wg.Wait()
	c.store.set(results)
}

// Run kjører en sjekk umiddelbart, så på ticker, til ctx er done.
func (c *Checker) Run(ctx context.Context) {
	c.runOnce(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runOnce(ctx)
		}
	}
}

func (c *Checker) Snapshot() []Result { return c.store.snapshot() }
```

Merk: legg til `"net/http"` i import-blokka.

- [ ] **Step 4: Kjør med -race, verifiser PASS**

Run: `cd apps/status-checker && go test ./... -run 'TestStore|TestChecker' -race -v`
Expected: PASS, ingen race-advarsler. (`results[i]` fra distinkte goroutiner er trygt - egne indekser i en pre-allokert slice, ingen samtidig map-skriving.)

- [ ] **Step 5: Commit**

```bash
git add apps/status-checker/store.go apps/status-checker/store_test.go
git commit -m "status-checker: store (atomic snapshot) + parallel ticker, unknown-before-first-check"
```

---

## Task 4: status-checker - HTTP-server, OTEL, main (wiring + graceful shutdown)

**Files:**
- Create: `apps/status-checker/main.go`
- Test: `apps/status-checker/api_test.go`
- Reference: `apps/go-hello-world/main.go` (kopier OTEL-oppsett eksakt)

**Interfaces:**
- Consumes: `loadConfig`, `Checker`, `newChecker`, `Result` (Task 1-3).
- Produces: `func statusHandler(c *Checker) http.HandlerFunc` (JSON `{"checked_at":...,"services":[...]}`); `main()` som leser `CONFIG_PATH`, starter `Checker.Run` i goroutine, serverer `/api/status` + `/health`.

- [ ] **Step 1: Skriv failing test** (`api_test.go`)

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusHandlerShape(t *testing.T) {
	c := newChecker([]Target{{Name: "a", URL: "https://a.newb.no", HealthPath: "/health"}}, 30*time.Second, 5*time.Second)
	c.store.set([]Result{{Name: "a", URL: "https://a.newb.no", Status: StatusUp}})

	rec := httptest.NewRecorder()
	statusHandler(c)(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("kode = %d", rec.Code)
	}
	var body struct {
		CheckedAt string   `json:"checked_at"`
		Services  []Result `json:"services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ugyldig JSON: %v", err)
	}
	if body.CheckedAt == "" {
		t.Error("checked_at mangler")
	}
	if len(body.Services) != 1 || body.Services[0].Name != "a" {
		t.Errorf("services feil: %+v", body.Services)
	}
}
```

- [ ] **Step 2: Kjør, verifiser FAIL**

Run: `cd apps/status-checker && go test ./... -run TestStatusHandler`
Expected: FAIL (`undefined: statusHandler`).

- [ ] **Step 3: Implementer** (`main.go`)

Kopier OTEL-blokken (`setupOTel`, imports, resource-attrs) EKSAKT fra `apps/go-hello-world/main.go`, bytt `ServiceName("go-hello-world")` → `ServiceName("status-checker")`. Legg så til:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	// ... resten av OTEL-importene kopiert fra go-hello-world
)

const (
	checkInterval = 30 * time.Second // ponytail: hardkodet, én operatør; env hvis det trengs
	checkTimeout  = 5 * time.Second  // ponytail: per-sjekk timeout
)

// statusHandler serverer siste snapshot som JSON.
func statusHandler(c *Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			CheckedAt string   `json:"checked_at"`
			Services  []Result `json:"services"`
		}{
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
			Services:  c.Snapshot(),
		})
	}
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		log.Fatal("CONFIG_PATH må settes")
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("config: %v", err) // fail-fast
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	shutdownOTel, err := setupOTel(ctx, "status-checker-v1")
	if err != nil {
		log.Fatalf("otel setup: %v", err)
	}

	checker := newChecker(cfg.Targets, checkInterval, checkTimeout)
	go checker.Run(ctx) // poller til ctx cancelleres (SIGTERM)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", statusHandler(checker))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	handler := otelhttp.NewHandler(mux, "server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
	srv := &http.Server{Addr: ":" + port, Handler: handler}

	go func() {
		log.Printf("status-checker listening on :%s (%d targets)", port, len(cfg.Targets))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	_ = shutdownOTel(shCtx)
}
```

(Merk: `setupOTel`-signaturen tar `(ctx, version string)`; bruk en version-konstant eller VERSION-env som go-hello-world. OTEL-imports + funksjonskropp kopieres verbatim fra go-hello-world.)

- [ ] **Step 4: Kjør tester + bygg**

Run: `cd apps/status-checker && go test ./... -race && go vet ./... && go build ./...`
Expected: alle tester PASS, vet rent, bygger.

- [ ] **Step 5: Manuell røyktest**

```bash
cd apps/status-checker
cat > /tmp/targets.yaml <<'EOF'
targets:
  - name: example
    url: https://example.com
    health_path: /
EOF
CONFIG_PATH=/tmp/targets.yaml PORT=8091 go run . &
sleep 3 && curl -s localhost:8091/api/status && curl -s localhost:8091/health
kill %1
```
Expected: `/api/status` viser `example` (up hvis nett, ellers down m/reason), `/health` = `{"status":"ok"}`.

- [ ] **Step 6: Commit**

```bash
git add apps/status-checker/main.go apps/status-checker/api_test.go
git commit -m "status-checker: http server, OTEL (go-hello-world pattern), graceful shutdown"
```

---

## Task 5: status-checker - per-target OTEL-metrics

**Files:**
- Modify: `apps/status-checker/store.go` (registrer + observer metrics i runOnce)
- Modify: `apps/status-checker/main.go` (init meter)
- Test: `apps/status-checker/metrics_test.go`

**Interfaces:**
- Produces: `func setupMetrics() (recordFn, error)` der `recordFn func(results []Result)` oppdaterer gauge `target_up{name}` (1/0) + histogram `target_latency_ms{name}`. `Checker` får et valgfritt `record recordFn`-felt kalt på slutten av `runOnce`.

- [ ] **Step 1: Skriv failing test** (`metrics_test.go`)

```go
package main

import "testing"

// Røyktest: setupMetrics skal returnere en ikke-nil recorder uten å panikke,
// og recorderen skal tåle å bli kalt med resultater (inkl. nil latency).
func TestSetupMetricsRecord(t *testing.T) {
	rec, err := setupMetrics()
	if err != nil {
		t.Fatalf("setupMetrics: %v", err)
	}
	if rec == nil {
		t.Fatal("recorder er nil")
	}
	lat := int64(42)
	rec([]Result{
		{Name: "a", Status: StatusUp, LatencyMs: &lat},
		{Name: "b", Status: StatusDown}, // nil latency skal ikke panikke
	})
}
```

- [ ] **Step 2: Kjør, verifiser FAIL**

Run: `cd apps/status-checker && go test ./... -run TestSetupMetrics`
Expected: FAIL (`undefined: setupMetrics`).

- [ ] **Step 3: Implementer** (ny fil `metrics.go`)

```go
package main

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type recordFn func([]Result)

// setupMetrics registrerer per-target up/down-gauge + latency-histogram på global meter.
// Meter-provideren settes av setupOTel; uten OTLP-endpoint er det en no-op-provider.
func setupMetrics() (recordFn, error) {
	m := otel.Meter("status-checker")
	upGauge, err := m.Int64Gauge("target_up")
	if err != nil {
		return nil, err
	}
	latHist, err := m.Float64Histogram("target_latency_ms")
	if err != nil {
		return nil, err
	}
	return func(results []Result) {
		ctx := context.Background()
		for _, r := range results {
			attrs := metric.WithAttributes(attribute.String("name", r.Name))
			up := int64(0)
			if r.Status == StatusUp {
				up = 1
			}
			upGauge.Record(ctx, up, attrs)
			if r.LatencyMs != nil {
				latHist.Record(ctx, float64(*r.LatencyMs), attrs)
			}
		}
	}, nil
}
```

Legg `record recordFn` til `Checker`-structen, sett den i `newChecker` (eller via en setter), og kall `if c.record != nil { c.record(results) }` på slutten av `runOnce`. I `main.go`: `rec, err := setupMetrics()` etter `setupOTel`, og gi den til checker.

- [ ] **Step 4: Kjør tester + bygg**

Run: `cd apps/status-checker && go test ./... -race && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/status-checker/metrics.go apps/status-checker/store.go apps/status-checker/main.go apps/status-checker/metrics_test.go
git commit -m "status-checker: per-target OTEL metrics (target_up gauge + latency histogram)"
```

---

## Task 6: status-checker - Dockerfile (distroless)

**Files:**
- Create: `apps/status-checker/Dockerfile`
- Create: `apps/status-checker/.dockerignore`
- Reference: `apps/go-hello-world/Dockerfile`

**Interfaces:** Produces et distroless image som tar `VERSION` build-arg (samme mønster som go-hello-world).

- [ ] **Step 1: Kopier go-hello-world sin Dockerfile som utgangspunkt**

Run: `cat apps/go-hello-world/Dockerfile`
Bruk den eksakte strukturen (multi-stage, statisk binær, `gcr.io/distroless/static-debian12`, `VERSION` ldflags). Kun forskjell: ingen, siden begge er enkle Go-binærer i egen modul.

- [ ] **Step 2: Skriv Dockerfile** (`apps/status-checker/Dockerfile`)

```dockerfile
# bygg statisk binær
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /app .

# distroless runtime (ingen shell)
FROM gcr.io/distroless/static-debian12
COPY --from=build /app /app
EXPOSE 8080
ENTRYPOINT ["/app"]
```

(Hvis go-hello-world sin Dockerfile avviker - f.eks. annen go-versjon eller build-flagg - match den i stedet for denne; den er fasiten.)

- [ ] **Step 3: .dockerignore**

```
*_test.go
```

- [ ] **Step 4: Bygg lokalt**

Run: `cd apps/status-checker && docker build --build-arg VERSION=test -t status-checker:test .`
Expected: bygger uten feil.

- [ ] **Step 5: Commit**

```bash
git add apps/status-checker/Dockerfile apps/status-checker/.dockerignore
git commit -m "status-checker: distroless Dockerfile"
```

---

## Task 7: status-checker - CI callers

**Files:**
- Create: `.github/workflows/status-checker.yml`
- Create: `.github/workflows/status-checker-pr.yml`
- Reference: `.github/workflows/go-hello-world.yml`, `.github/workflows/go-hello-world-pr.yml`

**Interfaces:** Produces main-caller (push → `_build-deploy.yml`) + PR-caller (tester + bygg-only). Følger Global Constraints PR-regler.

- [ ] **Step 1: Skriv main-caller** (`status-checker.yml`)

Kopier `go-hello-world.yml`, bytt verdier:
```yaml
name: status-checker

on:
  push:
    branches: [main]
    paths:
      - "apps/status-checker/**"
      - ".github/workflows/status-checker.yml"
      - ".github/workflows/_build-deploy.yml"
      - ".github/actions/gitops-promote/**"
      - ".github/actions/docker-build/**"
  workflow_dispatch:
    inputs:
      image_tag:
        description: "Eksisterende tag aa deploye (rollback/promote). Tom = bygg current."
        required: false

permissions:
  contents: read
  packages: write

jobs:
  deploy:
    uses: ./.github/workflows/_build-deploy.yml
    with:
      app: status-checker
      app_dir: apps/status-checker
      image: ghcr.io/kongebra/status-checker
      image_tag: ${{ github.event.inputs.image_tag || '' }}
    secrets: inherit
```

- [ ] **Step 2: Skriv PR-caller** (`status-checker-pr.yml`)

Kopier `go-hello-world-pr.yml`, bytt path/app_dir/image, behold `-race` i test:
```yaml
name: status-checker PR

on:
  pull_request:
    paths:
      - "apps/status-checker/**"
      - ".github/workflows/status-checker-pr.yml"
      - ".github/actions/docker-build/**"

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0  # v7.0.0
      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16  # v6.5.0
        with:
          go-version-file: apps/status-checker/go.mod
          cache-dependency-path: apps/status-checker/go.sum
      - name: Test
        working-directory: apps/status-checker
        run: |
          go vet ./...
          go test -race ./...
      - name: Build image (no push)
        uses: ./.github/actions/docker-build
        with:
          app_dir: apps/status-checker
          image: ghcr.io/kongebra/status-checker
          tag: pr-${{ github.sha }}
          push: false
```

- [ ] **Step 3: Valider med actionlint**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/status-checker.yml .github/workflows/status-checker-pr.yml`
Expected: ingen output (rent).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/status-checker.yml .github/workflows/status-checker-pr.yml
git commit -m "ci: status-checker main + PR caller workflows"
```

---

## Task 8: status-checker - per-app AGENTS.md

**Files:**
- Create: `apps/status-checker/AGENTS.md`
- Create symlink: `apps/status-checker/CLAUDE.md -> AGENTS.md`
- Reference: `apps/go-hello-world/AGENTS.md`

- [ ] **Step 1: Skriv AGENTS.md** (følg go-hello-world sin struktur: formål, endepunkter, bygg/kjør, env, OTEL, deploy)

Dekk: formål (status-checker for status.newb.no), endepunkter (`/api/status`, `/health`), prober kun domene-URL-er, ConfigMap (`CONFIG_PATH`, targets.yaml-format), env-tabell (`PORT`, `CONFIG_PATH`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `NODE_NAME`, `POD_NAMESPACE`), intervall/timeout er konstanter, OTEL + per-target metrics, deploy (image `ghcr.io/kongebra/status-checker`, ingen IngressRoute, replica=1).

- [ ] **Step 2: Lag symlink**

```bash
cd apps/status-checker && ln -s AGENTS.md CLAUDE.md
```

- [ ] **Step 3: Commit**

```bash
git add apps/status-checker/AGENTS.md apps/status-checker/CLAUDE.md
git commit -m "docs: status-checker AGENTS.md"
```

---

## Task 9: status-web - TanStack Start scaffold

**Files:**
- Create: `apps/status-web/package.json`
- Create: `apps/status-web/vite.config.ts`
- Create: `apps/status-web/tsconfig.json`
- Create: `apps/status-web/src/router.tsx`
- Create: `apps/status-web/src/routes/__root.tsx`
- Reference: TanStack Start docs (createFileRoute, vite build → `.output/server/index.mjs`)

**Interfaces:** Produces et byggbart TanStack Start-skjelett. `npm run build` → `.output/server/index.mjs`; `npm run start` kjører den.

- [ ] **Step 1: Scaffold via TanStack CLI eller manuelt**

Foretrukket (gir korrekt, versjonspinnet oppsett):
```bash
cd apps && npm create @tanstack/start@latest status-web -- --template typescript
```
Hvis CLI-flagg avviker: scaffold manuelt med `package.json` under. Pin TanStack Start-versjonen som CLI-en gir (noter den i AGENTS.md, Task 13).

- [ ] **Step 2: package.json scripts** (verifiser/sett)

```json
{
  "type": "module",
  "scripts": {
    "dev": "vite dev",
    "build": "vite build",
    "start": "node .output/server/index.mjs",
    "typecheck": "tsc --noEmit"
  }
}
```

- [ ] **Step 3: Verifiser bygg**

Run: `cd apps/status-web && npm install && npm run build`
Expected: produserer `.output/server/index.mjs`.

- [ ] **Step 4: Verifiser SSR-server starter**

Run: `cd apps/status-web && PORT=3001 npm run start &` så `curl -s localhost:3001` → HTML. `kill %1`.
Expected: SSR-rendret HTML.

- [ ] **Step 5: Commit**

```bash
git add apps/status-web/package.json apps/status-web/package-lock.json apps/status-web/vite.config.ts apps/status-web/tsconfig.json apps/status-web/src
git commit -m "status-web: TanStack Start scaffold (SSR, node-server build)"
```

---

## Task 10: status-web - status server-fn, grid, health route

**Files:**
- Create: `apps/status-web/src/routes/index.tsx` (server-fn + loader + grid + interval refresh)
- Create: `apps/status-web/src/routes/health.ts` (health server route)
- Create: `apps/status-web/src/types.ts` (Service-type matcher checker JSON-kontrakt)

**Interfaces:**
- Consumes: checker `/api/status` JSON (Task 4 kontrakt).
- Produces: index-route som SSR-rendrer status-grid + refresher hvert 30s; `/health` → 200.

- [ ] **Step 1: Type matcher checker-kontrakt** (`src/types.ts`)

```ts
export type Status = "up" | "down" | "unknown"

export interface Service {
  name: string
  url: string
  status: Status
  latency_ms: number | null
  http_code: number | null
  reason: string | null
  last_checked: string | null
}

export interface StatusResponse {
  checked_at: string
  services: Service[]
}
```

- [ ] **Step 2: Server-fn + route + grid** (`src/routes/index.tsx`)

```tsx
import { createFileRoute, useRouter } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useState } from "react"
import type { StatusResponse, Service } from "../types"

// Eneste sted som kjenner CHECKER_URL + gjør fetch. Kjører server-side under SSR
// OG eksponeres som RPC for client-refresh. Nettleseren når aldri checker.
const fetchStatus = createServerFn({ method: "GET" }).handler(async (): Promise<StatusResponse> => {
  const base = process.env.CHECKER_URL
  if (!base) throw new Error("CHECKER_URL ikke satt")
  const res = await fetch(`${base}/api/status`)
  if (!res.ok) throw new Error(`checker svarte ${res.status}`)
  return res.json()
})

export const Route = createFileRoute("/")({
  component: StatusPage,
  loader: () => fetchStatus(),
  errorComponent: () => (
    <main style={{ padding: 24, fontFamily: "system-ui" }}>
      <h1>status.newb.no</h1>
      <p style={{ color: "#b00" }}>Kan ikke nå checker.</p>
    </main>
  ),
})

function StatusPage() {
  const initial = Route.useLoaderData()
  const router = useRouter()

  // Client auto-refresh: invalider loader hvert 30s (samme server-fn, én datavei).
  useEffect(() => {
    const id = setInterval(() => router.invalidate(), 30_000)
    return () => clearInterval(id)
  }, [router])

  return (
    <main style={{ padding: 24, fontFamily: "system-ui", maxWidth: 720, margin: "0 auto" }}>
      <h1>status.newb.no</h1>
      <p style={{ color: "#666" }}>Sjekket: {initial.checked_at}</p>
      <div style={{ display: "grid", gap: 12 }}>
        {initial.services.map((s) => (
          <ServiceCard key={s.name} s={s} />
        ))}
      </div>
    </main>
  )
}

const COLOR: Record<string, string> = { up: "#1a7f37", down: "#b00", unknown: "#999" }

function ServiceCard({ s }: { s: Service }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12, padding: 16, border: "1px solid #ddd", borderRadius: 8 }}>
      <span style={{ width: 12, height: 12, borderRadius: "50%", background: COLOR[s.status] }} aria-label={s.status} />
      <strong style={{ flex: 1 }}>{s.name}</strong>
      <span style={{ color: "#666" }}>
        {s.status === "up" && s.latency_ms != null ? `${s.latency_ms} ms` : s.reason ?? s.status}
      </span>
      <LastChecked iso={s.last_checked} />
    </div>
  )
}

// Render absolutt tid server-side; humaniser FØRST etter mount (unngå hydration-mismatch).
function LastChecked({ iso }: { iso: string | null }) {
  const [rel, setRel] = useState<string | null>(null)
  useEffect(() => {
    if (!iso) return
    const secs = Math.round((Date.now() - new Date(iso).getTime()) / 1000)
    setRel(`for ${secs}s siden`)
  }, [iso])
  return <small style={{ color: "#999", minWidth: 90, textAlign: "right" }}>{rel ?? iso ?? "-"}</small>
}
```

- [ ] **Step 3: Health route** (`src/routes/health.ts`)

```ts
import { createFileRoute } from "@tanstack/react-router"

// Selv-avhengig health (aldri gated på checker - ellers tar checker-nedetid ned status-siden).
export const Route = createFileRoute("/health")({
  server: {
    handlers: {
      GET: async () => Response.json({ status: "ok" }),
    },
  },
})
```

- [ ] **Step 4: Verifiser bygg + typecheck**

Run: `cd apps/status-web && npm run typecheck && npm run build`
Expected: ingen TS-feil, bygger.

- [ ] **Step 5: Manuell røyktest mot en fake checker**

```bash
cd apps/status-web
# enkel fake checker
node -e 'require("http").createServer((_,r)=>{r.setHeader("content-type","application/json");r.end(JSON.stringify({checked_at:new Date().toISOString(),services:[{name:"demo",url:"https://x.newb.no",status:"up",latency_ms:12,http_code:200,reason:null,last_checked:new Date().toISOString()}]}))}).listen(8080)' &
CHECKER_URL=http://localhost:8080 PORT=3001 npm run start &
sleep 3 && curl -s localhost:3001/health && curl -s localhost:3001 | grep -o "demo"
kill %1 %2
```
Expected: `/health` = `{"status":"ok"}`, forsiden inneholder `demo`.

- [ ] **Step 6: Commit**

```bash
git add apps/status-web/src
git commit -m "status-web: status server-fn + grid + health route + 30s refresh"
```

---

## Task 11: status-web - Dockerfile (multi-stage node)

**Files:**
- Create: `apps/status-web/Dockerfile`
- Create: `apps/status-web/.dockerignore`

**Interfaces:** Produces et Node SSR-image som tar `VERSION` build-arg, kjører `.output/server/index.mjs`. Ikke distroless (Node-runtime kreves).

- [ ] **Step 1: Skriv Dockerfile**

```dockerfile
# build-stage: full toolchain
FROM node:22-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VERSION=dev
ENV VITE_APP_VERSION=${VERSION}
RUN npm run build

# runtime: kun .output + prod-deps på slank base (web er eneste tailnet-vendte tjeneste)
FROM node:22-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /src/.output ./.output
EXPOSE 3000
ENTRYPOINT ["node", ".output/server/index.mjs"]
```

(Hvis Nitro-output bundler deps inn i `.output` trengs ikke separat `npm ci --omit=dev` i runtime - verifiser i Step 3. Hvis ikke, legg til prod-deps-kopiering.)

- [ ] **Step 2: .dockerignore**

```
node_modules
.output
.vite
```

- [ ] **Step 3: Bygg + kjør image**

```bash
cd apps/status-web
docker build --build-arg VERSION=test -t status-web:test .
docker run --rm -e CHECKER_URL=http://x -e PORT=3000 -p 3000:3000 status-web:test &
sleep 3 && curl -s localhost:3000/health
docker stop $(docker ps -q --filter ancestor=status-web:test)
```
Expected: image bygger, `/health` = `{"status":"ok"}`.

- [ ] **Step 4: Commit**

```bash
git add apps/status-web/Dockerfile apps/status-web/.dockerignore
git commit -m "status-web: multi-stage node SSR Dockerfile"
```

---

## Task 12: status-web - CI callers

**Files:**
- Create: `.github/workflows/status-web.yml`
- Create: `.github/workflows/status-web-pr.yml`

**Interfaces:** main-caller (push → `_build-deploy.yml`) + PR-caller (npm typecheck/build + docker-build push=false).

- [ ] **Step 1: Main-caller** (`status-web.yml`)

Kopier `status-checker.yml`-strukturen, bytt til `app: status-web`, `app_dir: apps/status-web`, `image: ghcr.io/kongebra/status-web`, path-filtre til `apps/status-web/**` + `.github/workflows/status-web.yml`.

- [ ] **Step 2: PR-caller** (`status-web-pr.yml`)

```yaml
name: status-web PR

on:
  pull_request:
    paths:
      - "apps/status-web/**"
      - ".github/workflows/status-web-pr.yml"
      - ".github/actions/docker-build/**"

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0  # v7.0.0
      - name: Set up Node
        uses: actions/setup-node@39370e3970a6d050c480ffad4ff0ed4d3fdee5af  # v4.1.0
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: apps/status-web/package-lock.json
      - name: Install + typecheck + build
        working-directory: apps/status-web
        run: |
          npm ci
          npm run typecheck
          npm run build
      - name: Build image (no push)
        uses: ./.github/actions/docker-build
        with:
          app_dir: apps/status-web
          image: ghcr.io/kongebra/status-web
          tag: pr-${{ github.sha }}
          push: false
```

(Verifiser setup-node SHA mot siste v4 ved utførelse; bytt hvis nyere.)

- [ ] **Step 3: actionlint**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/status-web.yml .github/workflows/status-web-pr.yml`
Expected: rent.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/status-web.yml .github/workflows/status-web-pr.yml
git commit -m "ci: status-web main + PR caller workflows"
```

---

## Task 13: status-web - per-app AGENTS.md

**Files:**
- Create: `apps/status-web/AGENTS.md`
- Create symlink: `apps/status-web/CLAUDE.md -> AGENTS.md`

- [ ] **Step 1: Skriv AGENTS.md**

Dekk: formål (frontend for status.newb.no), at det er TanStack Start SSR (pin versjon), `createServerFn`-mønsteret (én datavei, browser når aldri checker), env (`CHECKER_URL` env-suffikset namespace, `PORT`), bygg (`.output/server/index.mjs`, ikke distroless - bevisst), `/health` selv-avhengig, deploy (image `ghcr.io/kongebra/status-web`, eneste med IngressRoute `status.newb.no`). Noter at dette er TanStack+Go-malen for fremtidige apper.

- [ ] **Step 2: Symlink + commit**

```bash
cd apps/status-web && ln -s AGENTS.md CLAUDE.md && cd ../..
git add apps/status-web/AGENTS.md apps/status-web/CLAUDE.md
git commit -m "docs: status-web AGENTS.md"
```

---

## Task 14: GitOps-manifester (handoff til kongebra-gitops)

**Files:** (i `kongebra-gitops`-repoet, ikke dette)
- `apps/status-checker/{base,overlays/dev,overlays/prod}`
- `apps/status-web/{base,overlays/dev,overlays/prod}`

**Interfaces:** Produces deployerbare manifester. Krever cluster-tilgang for verifisering → handoff via GitHub-issue (mønster fra go-hello-world gitops#1/#3).

- [ ] **Step 1: Opprett handoff-issue på kongebra-gitops**

Issue-innhold MÅ spesifisere:
- **status-checker:** Deployment (replica=1, image `ghcr.io/kongebra/status-checker`, ingen IngressRoute, ingen imagePullSecret hvis pakka er public), ConfigMap via `configMapGenerator` (targets.yaml med `https://<app>.newb.no` + self-target `https://status.newb.no/health`), env `CONFIG_PATH` + Downward API `NODE_NAME`/`POD_NAMESPACE` + `OTEL_EXPORTER_OTLP_ENDPOINT`, probes `httpGet /health`, resources `req 25m/32Mi lim 250m/64Mi`.
- **status-web:** Deployment (image `ghcr.io/kongebra/status-web`, env `CHECKER_URL=http://status-checker.status-checker-<env>.svc.cluster.local:8080` injisert ULIKT per env-overlay, `PORT`), IngressRoute `status.newb.no` (prod) / dev-variant, probes `httpGet /health` (readiness selv-avhengig, ALDRI checker-gated), resources `req 50m/128Mi lim 500m/256Mi`.
- Per-app-per-env namespace. build-once-promote (CI pinner digest).
- NetworkPolicy-intent (default-deny + allow web→checker:8080), `// ponytail:` aspirasjonell til Cilium.
- DoD: `status.newb.no` viser grid; ta target ned → down m/reason; ConfigMap-endring ruller checker; checker ikke nåbar utenfra; OTEL-metrics i Grafana.

- [ ] **Step 2: Krysslenk**

Lenk issue-en til denne plan-en + spec-en. Når gitops-agenten lukker den, verifiser DoD live.

---

## Self-Review

**Spec coverage:**
- Probe domene-URL + CoreDNS-avhengighet → Task 0 + Task 2/config. ✓
- Concurrency atomic snapshot → Task 3 (race-test). ✓
- Delt http.Client + context-timeout → Task 2. ✓
- reason/unknown/JSON-kontrakt → Task 2 + 4. ✓
- ConfigMap-reload (configMapGenerator) → Task 14. ✓
- Én createServerFn (SSR + refresh) → Task 10. ✓
- SSR build output named → Task 9 + 11. ✓
- OTEL eksakt gjenbruk + per-target metrics → Task 4 + 5. ✓
- CHECKER_URL env-suffikset per env → Task 10 (fetch) + Task 14 (injeksjon). ✓
- Probes/resources/replica=1/self-monitor/NetworkPolicy → Task 14. ✓
- Hydration-safe tid → Task 10 (`LastChecked`). ✓
- PR-sikkerhetsregler → Task 7 + 12. ✓
- Distroless checker / node web → Task 6 + 11. ✓

**Placeholder scan:** Ingen TBD/TODO. CLI-scaffold (Task 9) og setup-node-SHA (Task 12) har eksplisitt "verifiser versjon ved utførelse" - bevisst, ikke placeholder.

**Type consistency:** `Result` (Go, Task 2) ↔ `Service` (TS, Task 10) feltnavn matcher checker JSON-tags (`latency_ms`, `http_code`, `last_checked`, `reason`). `Status`-enum-verdier (`up`/`down`/`unknown`) konsistent Go↔TS. `Checker.Snapshot()`/`statusHandler` konsistent Task 3↔4. `setupMetrics`/`recordFn` konsistent Task 5.

**Kjent avhengighet utenfor planen:** Task 0 (CoreDNS) + Task 14 (gitops) krever cluster-tilgang og utføres av operatør/gitops-agent, ikke i denne repo-en. Eksplisitt markert.
