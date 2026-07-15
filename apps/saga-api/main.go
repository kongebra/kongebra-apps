// saga-api is the backend for saga.kongebra.no: a Postgres-backed job queue
// plus modules that summarize content via the in-cluster LiteLLM gateway, which
// fans out to the two local Ollama GPU boxes (round-robin) and Ollama Cloud
// behind one OpenAI-compatible endpoint.
// Design: docs/superpowers/specs/2026-07-04-saga-platform-design.md +
// docs/superpowers/specs/2026-07-13-litellm-gateway-design.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"saga-api/internal/api"
	"saga-api/internal/catalog"
	"saga-api/internal/config"
	"saga-api/internal/db"
	"saga-api/internal/llm"
	"saga-api/internal/module"
	"saga-api/internal/module/ytsummary"
	"saga-api/internal/obs"
	"saga-api/internal/store"
	"saga-api/internal/worker"
	"saga-api/internal/ytdlp"
)

// version is set at build time via -ldflags "-X main.version=<git-sha>".
var version = "dev"

func main() {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// otelShutdown flushes traces/metrics; called explicitly in the shutdown
	// sequence below (after the worker drains, before the pool closes) so no
	// telemetry from an in-flight job's final write is lost. Disabled (empty
	// endpoint) path returns a no-op, so this never fails boot.
	otelShutdown, err := obs.Setup(ctx, cfg.OTELEndpoint, version)
	if err != nil {
		log.Fatalf("otel setup: %v", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	// pool.Close is called explicitly at the end of the shutdown sequence
	// below (not deferred here) so it happens strictly after the worker has
	// drained and OTel has flushed - a deferred Close would race both.
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	module.Register(ytsummary.Module{})

	// All LLM traffic goes through the in-cluster LiteLLM gateway (one OpenAI
	// endpoint; it fans out to the two Ollama boxes + Ollama Cloud by model name).
	llmClient := llm.New(cfg.LiteLLMURL, cfg.LiteLLMAPIKey)
	// cloudEnabled drives the frontend Turbo gate: probe the gateway for a live
	// cloud-tier model rather than trusting a static flag. If cloud is
	// unreachable the pinned cloud translate model is unusable, so fall back to
	// a local Norwegian-capable model at boot (not mid-pipeline).
	cloudEnabled := probeCloud(ctx, cfg.LiteLLMURL, cfg.LiteLLMAPIKey)
	if !cloudEnabled {
		cfg.TranslateModel = "gemma4:e4b"
	}

	deps := module.Deps{
		LLM:            llmClient,
		Fetcher:        ytdlp.Exec{Bin: cfg.YtdlpPath, WorkDir: cfg.WorkDir},
		ChunkTimeout:   cfg.ChunkTimeout,
		TranslateModel: cfg.TranslateModel,
		Transcripts: func(ctx context.Context, sha string) (*store.Transcript, error) {
			return store.GetTranscript(ctx, pool, sha)
		},
	}
	bus := api.NewBus()
	// workerDone closes when worker.Run returns - which only happens once ctx
	// is canceled AND its dispatch goroutines have drained in-flight jobs
	// (worker.Run's own wg.Wait()). The shutdown sequence below waits on it
	// so a job that is mid-write when SIGTERM arrives finishes its job_runs
	// insert (and the span/metrics that describe it) before OTel flushes and
	// the pool closes underneath it.
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx, pool, deps, bus, cfg.SAGACloudConcurrency)
	}()

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: api.New(pool, bus, deps.LLM, version, cfg.TranslateModel, cloudEnabled)}
	// shutdownComplete gates main's return on the full shutdown sequence, in
	// order: HTTP server drains -> worker drains -> OTel flushes -> pool
	// closes. Without this, main would fall through as soon as
	// ListenAndServe returns (which happens as soon as the listener closes,
	// not once the rest of this sequence finishes) and the process would
	// exit mid-drain.
	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}

		<-workerDone

		flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer flushCancel()
		if err := otelShutdown(flushCtx); err != nil {
			log.Printf("otel shutdown: %v", err)
		}

		pool.Close()
	}()

	log.Printf("saga-api %s listening on :%s (modules: %v)", version, cfg.Port, module.Names())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownComplete
}

// probeCloud reports whether the LiteLLM gateway currently serves any cloud-tier
// catalog model. Drives the frontend Turbo gate honestly (vs a static env that
// drifts from reachability). Best-effort: any error -> false.
func probeCloud(ctx context.Context, baseURL, apiKey string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return false
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	for _, m := range body.Data {
		if cm, ok := catalog.Get(m.ID); ok && cm.Tier == "cloud" {
			return true
		}
	}
	return false
}
