// roster-tjenesten (SPEC §4, §7): Person-roster (tenant-eid), manuell
// account-kobling og person-events via transactional outbox. Person er ALDRI en
// brukerkonto (hard splitt, SPEC §4) - all deltakelse peker på Person.
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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/otelsetup"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/outbox"
)

// version settes ved build via -ldflags "-X main.version=<tag>", env VERSION overstyrer.
var version = "dev"

// healthCheck gjør en GET mot lokal /healthz og returnerer exit-kode.
// k8s exec-probe ("/app", "-health") siden distroless ikke har shell/curl.
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

	if err := run(); err != nil {
		log.Fatalf("roster: %v", err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	v := version
	if env := os.Getenv("VERSION"); env != "" {
		v = env
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// OTel kun når endpoint er satt (lokal kjøring uten collector er stille).
	shutdownOTel := func(context.Context) error { return nil }
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		shutdownOTel, err = otelsetup.Setup(ctx, "tronderleikan-roster")
		if err != nil {
			return fmt.Errorf("otel setup: %w", err)
		}
	}
	defer func() {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownOTel(shCtx)
	}()

	// Migrasjoner før pool-en tas i bruk (SPEC §8: goose per tjeneste).
	if err := runMigrations(cfg.DatabaseURL); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Drain() //nolint:errcheck
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream: %w", err)
	}
	if err := ensureStream(ctx, js); err != nil {
		return err
	}

	validator, err := authn.NewValidator(ctx, cfg.AuthAudience)
	if err != nil {
		return fmt.Errorf("auth validator: %w", err)
	}

	a := &api{
		store:     NewPgStore(pool),
		vis:       NewPgVisibility(pool),
		validator: validator,
	}

	// Outbox-publisher: flytter usendte events til JetStream (SPEC §9).
	publisher := outbox.NewPublisher(pool, js)
	go func() {
		if err := publisher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("outbox publisher stoppet: %v", err)
		}
	}()

	// Tenant-projeksjon-konsument: lærer public_visibility fra platform-events.
	go func() {
		if err := runTenantConsumer(ctx, js, pool); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("tenant-consumer stoppet: %v", err)
		}
	}()

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: a.routes()}
	go func() {
		log.Printf("tronderleikan-roster listening on :%s (version=%s)", cfg.Port, v)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shCtx)
}
