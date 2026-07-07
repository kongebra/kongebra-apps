// Command platform er TrønderLeikans platform-tjeneste (SPEC §7, arbeidspakke
// 1.1): tenant-registry, Zitadel-provisjonering og admin-plane-API.
//
// Oppstart: kjør migrasjoner -> koble Postgres/NATS/Zitadel -> utled JWT-audience
// fra Zitadel-prosjektet -> start outbox-publisher + HTTP-server. Konfig leses
// fra env (issuer/Zitadel-domenet ALDRI hardkodet, SPEC §5).
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

// streamName + streamSubjects: the ONE shared JetStream stream for the product
// (SPEC §9) - all services ensure the same "tl"/"tl.>" stream idempotently, so a
// fresh cluster or local run needs no manual stream setup. platform publishes its
// domain events to tl.platform.* subjects (which fall under tl.>); roster/competition
// consume via FilterSubjects. A per-service stream (e.g. tl-platform/tl.platform.>)
// would OVERLAP this shared stream's subjects and JetStream rejects that (bit us
// 2026-07-07: platform's old tl-platform collided with roster/competition's tl).
const (
	streamName     = "tl"
	streamSubjects = "tl.>"
)

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

	port := os.Getenv(EnvPort)
	if port == "" {
		port = defaultPort
	}
	if *health {
		os.Exit(healthCheck(port))
	}

	if err := run(); err != nil {
		log.Fatalf("platform: %v", err)
	}
}

func run() error {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
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
		shutdownOTel, err = otelsetup.Setup(ctx, "tronderleikan-platform")
		if err != nil {
			return fmt.Errorf("otel setup: %w", err)
		}
	}
	defer func() {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownOTel(shCtx)
	}()

	// Migrasjoner før noe annet rører databasen (SPEC §8).
	if err := runMigrations(ctx, cfg.DatabaseURL); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	// NATS JetStream + idempotent stream-ensure for platform-subjektene.
	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Drain() //nolint:errcheck // best-effort ved shutdown
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream: %w", err)
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{streamSubjects},
	}); err != nil {
		return fmt.Errorf("ensure jetstream stream %q: %w", streamName, err)
	}

	// Zitadel-klient for provisjonering (retry: instansen kan være under oppstart).
	dir, err := connectZitadelWithRetry(ctx, cfg, 12, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect zitadel: %w", err)
	}
	defer func() { _ = dir.Close() }()
	prov := NewProvisioner(dir, cfg.PlatformOrgName, cfg.ProjectName, log.Printf)

	// JWT-audience utledes fra Zitadel-prosjektet (ingen hardkodet/driftende
	// project-id). Krever at zitadel-seed er kjørt (project finnes).
	audience, err := prov.ProjectAudience(ctx)
	if err != nil {
		return fmt.Errorf("utled jwt-audience: %w", err)
	}
	validator, err := authn.NewValidator(ctx, audience)
	if err != nil {
		return fmt.Errorf("authn validator: %w", err)
	}

	repo := NewRepo(pool)
	svc := NewService(pool, repo, prov, log.Printf)
	server := NewServer(svc, repo, validator)

	// Outbox-publisher: flytter domene-events til NATS (SPEC §9).
	publisher := outbox.NewPublisher(pool, js)
	go func() {
		if err := publisher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("outbox publisher stopped: %v", err)
		}
	}()

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: server.Handler()}
	go func() {
		log.Printf("tronderleikan-platform listening on :%s (version=%s, audience=%s)", cfg.Port, v, audience)
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
	return nil
}

// connectZitadelWithRetry prøver å koble til Zitadel noen ganger (instansen kan
// være under oppstart). Samme mønster som zitadel-seed.
func connectZitadelWithRetry(ctx context.Context, cfg Config, attempts int, wait time.Duration) (*zitadelDirectory, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		dir, err := newZitadelDirectory(ctx, cfg.ZitadelTarget, cfg.ZitadelToken)
		if err == nil {
			return dir, nil
		}
		lastErr = err
		log.Printf("zitadel connect attempt %d/%d failed: %v (retry in %s)", i+1, attempts, err, wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}
