package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registrerer "pgx"-driveren for database/sql
	"github.com/pressly/goose/v3"
)

// migrationsFS er tjenestens egne goose-migrasjoner (SPEC §8: goose per tjeneste).
// 0001_outbox.sql er kopiert fra pkg/outbox/migrations (hver tjeneste eier sin
// egen versjonsrekkefølge), 0002_tenants.sql er tenant-registeret.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// runMigrations kjører goose Up mot databasen ved oppstart (SPEC §8: migrasjoner
// kjøres ved deploy). Bruker en kortlevd database/sql-tilkobling (pgx stdlib);
// selve tjenesten bruker pgxpool.
func runMigrations(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open db for migrations: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
