// saga-api is the backend for saga.kongebra.no: a Postgres-backed job queue
// plus modules that summarize content with Ollama - the local GPU by default,
// or Ollama Cloud when the model is a cloud tag and an API key is configured.
// Design: docs/superpowers/specs/2026-07-04-saga-platform-design.md.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"saga-api/internal/api"
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
	// The default translate model is a cloud tag; without an API key it is
	// unreachable, so fall back to a local Norwegian-capable model at boot
	// (not mid-pipeline) so translate keeps working offline.
	if cfg.OllamaAPIKey == "" {
		cfg.TranslateModel = "gemma4:e4b"
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

	// Local GPU always; Ollama Cloud only when an API key is configured.
	// The Router picks per model: cloud tags (":cloud"/"-cloud") go to cloud.
	var cloud llm.Provider
	if cfg.OllamaAPIKey != "" {
		cloud = llm.NewCloud(cfg.OllamaCloudURL, cfg.OllamaAPIKey)
	}
	router := llm.NewRouter(llm.New(cfg.OllamaURL), cloud)

	deps := module.Deps{
		LLM:            router,
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

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: api.New(pool, bus, deps.LLM, version, cfg.TranslateModel, cfg.OllamaAPIKey != "")}
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
