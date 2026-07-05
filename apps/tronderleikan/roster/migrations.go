package main

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registrerer "pgx"-driveren for database/sql (goose)
	"github.com/pressly/goose/v3"
)

// migrationsFS er goose-migrasjonene for roster (SPEC §8: goose per tjeneste).
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// runMigrations kjører alle opp-migrasjoner mot databasen (kjøres ved oppstart
// lokalt og ved deploy). Bruker en kortlevd database/sql-tilkobling via
// pgx-stdlib-driveren - goose trenger *sql.DB, resten av tjenesten bruker pgxpool.
func runMigrations(databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open sql db for migrations: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
