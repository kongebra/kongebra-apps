package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Visibility slår opp om en tenant er offentlig synlig (SPEC §6 public_visibility).
// Roster eier ikke tenant-registeret (platform gjør, SPEC §7) - dette er en
// read-model roster lærer via platform sine tenant-events (se consumer.go).
type Visibility interface {
	// IsPublic returnerer true hvis anonym lesetilgang er tillatt for tenanten.
	IsPublic(ctx context.Context, tenantID uuid.UUID) (bool, error)
}

// PgVisibility leser tenant_projection. Ukjent tenant = default true
// (SPEC §6: public_visibility default på); platform emitter en rad først når en
// tenant skrur den AV.
type PgVisibility struct {
	pool *pgxpool.Pool
}

func NewPgVisibility(pool *pgxpool.Pool) *PgVisibility { return &PgVisibility{pool: pool} }

func (v *PgVisibility) IsPublic(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var public bool
	err := v.pool.QueryRow(ctx,
		`SELECT public_visibility FROM tenant_projection WHERE tenant_id = $1`,
		tenantID).Scan(&public)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil // ukjent tenant -> SPEC-default (på)
	}
	if err != nil {
		return false, fmt.Errorf("query tenant_projection: %w", err)
	}
	return public, nil
}

// upsertVisibility skriver public_visibility for en tenant (kalt av konsumenten
// når en platform tenant-event kommer inn). Idempotent.
func upsertVisibility(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID, public bool) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO tenant_projection (tenant_id, public_visibility, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (tenant_id) DO UPDATE
		   SET public_visibility = EXCLUDED.public_visibility, updated_at = now()`,
		tenantID, public)
	if err != nil {
		return fmt.Errorf("upsert tenant_projection: %w", err)
	}
	return nil
}

var _ Visibility = (*PgVisibility)(nil)
