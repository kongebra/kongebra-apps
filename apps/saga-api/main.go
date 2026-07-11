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

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
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
		LLM:          router,
		Fetcher:      ytdlp.Exec{Bin: cfg.YtdlpPath, WorkDir: cfg.WorkDir},
		ChunkTimeout: cfg.ChunkTimeout,
	}
	bus := api.NewBus()
	go worker.Run(ctx, pool, deps, bus, cfg.SAGACloudConcurrency)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: api.New(pool, bus, deps.LLM, version)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	log.Printf("saga-api %s listening on :%s (modules: %v)", version, cfg.Port, module.Names())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
