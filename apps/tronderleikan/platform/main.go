// ponytail: dummy platform-tjeneste kun for å gi CI-løypa (arbeidspakke 0.2)
// noe å bygge. Erstattes av ekte tenant-registry i arbeidspakke 1.1 (SPEC §7);
// behold main-skjelettet (healthz, -health-flagg, graceful shutdown, otelsetup).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/otelsetup"
)

// version settes ved build via -ldflags "-X main.version=<tag>", env VERSION overstyrer.
var version = "dev"

// healthCheck gjør en GET mot den lokale serverens /healthz og returnerer exit-kode.
// Brukes som k8s exec-probe ("/app", "-health") siden distroless ikke har shell/curl.
func healthCheck(port string) int {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

func main() {
	health := flag.Bool("health", false, "probe local /healthz and exit (distroless healthcheck)")
	flag.Parse()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if *health {
		os.Exit(healthCheck(port))
	}

	v := version
	if env := os.Getenv("VERSION"); env != "" {
		v = env
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// OTel kun når endpoint er satt, så lokal kjøring uten collector er stille.
	shutdownOTel := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		var err error
		shutdownOTel, err = otelsetup.Setup(ctx, "tronderleikan-platform")
		if err != nil {
			log.Fatalf("otel setup: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})

	srv := &http.Server{Addr: ":" + port, Handler: mux}

	go func() {
		log.Printf("tronderleikan-platform listening on :%s (version=%s)", port, v)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	// graceful shutdown: k8s sender SIGTERM ved rolling update/evict.
	<-ctx.Done()
	log.Println("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	_ = shutdownOTel(shCtx)
}
