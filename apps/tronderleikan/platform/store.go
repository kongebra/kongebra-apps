package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB er delmengden av *pgxpool.Pool tjenesten trenger (fakes i test).
// Begin brukes for tenant-insert + outbox-write i SAMME transaksjon (SPEC §9).
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ErrNotFound returneres av oppslag når raden ikke finnes (mappes til 404).
var ErrNotFound = errors.New("tenant finnes ikke")

// Repo er tenant-registerets Postgres-lag. Ingen RLS/SET LOCAL app.tenant_id:
// registeret er ikke tenant-scopet (SPEC §8).
type Repo struct {
	db DB
}

// NewRepo lager en Repo over db.
func NewRepo(db DB) *Repo { return &Repo{db: db} }

const tenantCols = `id, name, slug, zitadel_org_id, public_visibility, created_at, updated_at`

func scanTenant(row pgx.Row) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.ZitadelOrgID, &t.PublicVisibility, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// InsertTx skriver en tenant-rad innenfor tx (samme tx som outbox-eventet).
// Timestamps genereres i app (som id-en) og skrives eksplisitt, så den
// returnerte Tenant-en er nøyaktig uten en ekstra spørring.
func InsertTx(ctx context.Context, tx pgx.Tx, t Tenant) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO tenants (id, name, slug, zitadel_org_id, public_visibility, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.ID, t.Name, t.Slug, t.ZitadelOrgID, t.PublicVisibility, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

// ExistsBySlug sier om en tenant med slug allerede finnes (rask forhåndssjekk
// før provisjonering; UNIQUE-constraintet er den harde garantien).
func (r *Repo) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM tenants WHERE slug = $1)`, slug).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check slug exists: %w", err)
	}
	return exists, nil
}

// List returnerer alle tenants, nyeste først (UUIDv7 er tidsordnet).
func (r *Repo) List(ctx context.Context) ([]Tenant, error) {
	rows, err := r.db.Query(ctx, `SELECT `+tenantCols+` FROM tenants ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var tenants []Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return tenants, nil
}

// GetByID henter én tenant på id. ErrNotFound hvis den ikke finnes.
func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (Tenant, error) {
	t, err := scanTenant(r.db.QueryRow(ctx, `SELECT `+tenantCols+` FROM tenants WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("get tenant by id: %w", err)
	}
	return t, nil
}

// GetBySlug henter én tenant på slug (SPEC §7 slug-oppslag). ErrNotFound hvis
// den ikke finnes.
func (r *Repo) GetBySlug(ctx context.Context, slug string) (Tenant, error) {
	t, err := scanTenant(r.db.QueryRow(ctx, `SELECT `+tenantCols+` FROM tenants WHERE slug = $1`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("get tenant by slug: %w", err)
	}
	return t, nil
}

// Update setter name + public_visibility og bumper updated_at. ErrNotFound hvis
// raden ikke finnes. Slug og zitadel_org_id er uforanderlige (identitet).
func (r *Repo) Update(ctx context.Context, id uuid.UUID, name string, publicVisibility bool) (Tenant, error) {
	t, err := scanTenant(r.db.QueryRow(ctx,
		`UPDATE tenants SET name = $2, public_visibility = $3, updated_at = now() WHERE id = $1 RETURNING `+tenantCols,
		id, name, publicVisibility,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("update tenant: %w", err)
	}
	return t, nil
}

// Delete fjerner tenant-raden. ErrNotFound hvis den ikke finnes.
//
// ponytail: sletter KUN registerraden, ikke Zitadel-orgen. Å slette en org
// kaskaderer brukere/roller og er en irreversibel operasjon vi ikke
// auto-kjører i v1. Oppgraderingssti: en egen "decommission tenant"-flyt som
// river Zitadel-orgen + varsler konsumenter via event, som egen arbeidspakke.
func (r *Repo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
