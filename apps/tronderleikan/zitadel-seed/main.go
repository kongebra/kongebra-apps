// Command zitadel-seed provisjonerer TrønderLeikans Zitadel-grunntilstand
// idempotent (SPEC §5, §6, §12): plattform-org, project `tronderleikan`, de 4
// project-rollene, én test-tenant-org med project-grant, og testbrukere med
// rolletildelinger. To kjøringer på rad gir samme sluttilstand.
//
// Konfig leses fra env (issuer/domenet ALDRI hardkodet, SPEC §5). Kjøres lokalt
// via Aspire (addExecutable) og mot cluster (auth.newb.no) - se README.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Zitadel kan være under oppstart (waitFor dekker helsesjekk, men API-et kan
	// trenge et øyeblikk til). Én kort retry-løkke gjør seeden robust lokalt.
	dir, err := connectWithRetry(ctx, cfg, 12, 5*time.Second)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer func() { _ = dir.Close() }()

	seeder := NewSeeder(dir, log.Printf)
	res, err := seeder.Seed(ctx, cfg)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	log.Printf("seed complete: platform-org=%s project=%s tenant-org=%s grant=%s users=%d",
		res.PlatformOrgID, res.ProjectID, res.TenantOrgID, res.ProjectGrantID, len(res.UserIDs))
}

func connectWithRetry(ctx context.Context, cfg Config, attempts int, wait time.Duration) (*zitadelDirectory, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		dir, err := newZitadelDirectory(ctx, cfg.Target, cfg.Token)
		if err == nil {
			return dir, nil
		}
		lastErr = err
		log.Printf("connect attempt %d/%d failed: %v (retry in %s)", i+1, attempts, err, wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}
