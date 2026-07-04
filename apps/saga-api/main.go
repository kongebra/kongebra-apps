// saga-api is the backend for saga.kongebra.no: a Postgres-backed job queue
// plus modules that summarize content with the local Ollama instance.
// Design: docs/superpowers/specs/2026-07-04-saga-platform-design.md.
package main

import (
	"log"
	"net/http"

	"saga-api/internal/config"
)

// version is set at build time via -ldflags "-X main.version=<git-sha>".
var version = "dev"

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	log.Printf("saga-api %s listening on :%s", version, cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
