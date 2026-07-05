package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/outbox"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/tenantctx"
)

// Domenefeil - handlerlaget mapper disse til HTTP-statuser.
var (
	// ErrNotFound: ingen person med ID-en i tenanten.
	ErrNotFound = errors.New("person not found")
	// ErrAccountTaken: account_id er allerede koblet til en annen person i
	// tenanten (SPEC §4: unik per tenant). Mapper til HTTP 409.
	ErrAccountTaken = errors.New("account already linked to another person in this tenant")
)

// Store er roster-persistensen slik handlerne ser den (fakes i handler-test).
type Store interface {
	Create(ctx context.Context, tenantID uuid.UUID, in PersonInput) (Person, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (Person, error)
	List(ctx context.Context, tenantID uuid.UUID) ([]Person, error)
	Update(ctx context.Context, tenantID, id uuid.UUID, in PersonInput) (Person, error)
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
	SetAccount(ctx context.Context, tenantID, id uuid.UUID, accountID string) (Person, error)
	ClearAccount(ctx context.Context, tenantID, id uuid.UUID) (Person, error)
}

// PgStore er den Postgres-baserte Store-en. Hver skriveoperasjon kjører i én tx
// som (1) setter app.tenant_id for RLS (SPEC §8) og (2) skriver domene-endring +
// outbox-event atomisk (SPEC §9). Alle spørringer filtrerer i tillegg på
// tenant_id i app-laget - RLS er sikkerhetsnettet, ikke eneste vern.
type PgStore struct {
	pool *pgxpool.Pool
}

func NewPgStore(pool *pgxpool.Pool) *PgStore { return &PgStore{pool: pool} }

// tx åpner en tx, setter tenant-scope for RLS, kjører fn og committer.
// Ved feil rulles alt tilbake (også outbox-raden - atomisk med endringen).
func (s *PgStore) tx(ctx context.Context, tenantID uuid.UUID, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op etter commit
	if err := tenantctx.SetLocal(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

const personCols = `id, tenant_id, name, department, avatar_url, account_id, created_at, updated_at`

func scanPerson(row pgx.Row) (Person, error) {
	var p Person
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Department, &p.AvatarURL,
		&p.AccountID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Person{}, err
	}
	return p, nil
}

// writeEvent skriver et event til outbox i den gjeldende tx-en (SPEC §9).
func writeEvent(ctx context.Context, tx pgx.Tx, env event.Envelope, err error) error {
	if err != nil {
		return fmt.Errorf("build event: %w", err)
	}
	return outbox.Write(ctx, tx, env)
}

func (s *PgStore) Create(ctx context.Context, tenantID uuid.UUID, in PersonInput) (Person, error) {
	in, err := in.normalize()
	if err != nil {
		return Person{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Person{}, fmt.Errorf("generate person id: %w", err)
	}
	var p Person
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`INSERT INTO person (id, tenant_id, name, department, avatar_url)
			 VALUES ($1, $2, $3, $4, $5) RETURNING `+personCols,
			id, tenantID, in.Name, in.Department, in.AvatarURL)
		p, err = scanPerson(row)
		if err != nil {
			return fmt.Errorf("insert person: %w", err)
		}
		env, evErr := personCreatedEvent(p)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Person{}, err
	}
	return p, nil
}

func (s *PgStore) Get(ctx context.Context, tenantID, id uuid.UUID) (Person, error) {
	var p Person
	err := s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT `+personCols+` FROM person WHERE tenant_id = $1 AND id = $2`,
			tenantID, id)
		var scanErr error
		p, scanErr = scanPerson(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return scanErr
	})
	if err != nil {
		return Person{}, err
	}
	return p, nil
}

func (s *PgStore) List(ctx context.Context, tenantID uuid.UUID) ([]Person, error) {
	var persons []Person
	err := s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+personCols+` FROM person WHERE tenant_id = $1 ORDER BY name, id`,
			tenantID)
		if err != nil {
			return fmt.Errorf("query persons: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPerson(rows)
			if err != nil {
				return fmt.Errorf("scan person: %w", err)
			}
			persons = append(persons, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return persons, nil
}

func (s *PgStore) Update(ctx context.Context, tenantID, id uuid.UUID, in PersonInput) (Person, error) {
	in, err := in.normalize()
	if err != nil {
		return Person{}, err
	}
	var p Person
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`UPDATE person SET name = $3, department = $4, avatar_url = $5, updated_at = now()
			 WHERE tenant_id = $1 AND id = $2 RETURNING `+personCols,
			tenantID, id, in.Name, in.Department, in.AvatarURL)
		var scanErr error
		p, scanErr = scanPerson(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("update person: %w", scanErr)
		}
		env, evErr := personUpdatedEvent(p)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Person{}, err
	}
	return p, nil
}

func (s *PgStore) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`DELETE FROM person WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		if err != nil {
			return fmt.Errorf("delete person: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		env, evErr := personDeletedEvent(tenantID, id)
		return writeEvent(ctx, tx, env, evErr)
	})
}

// SetAccount kobler person til en Zitadel-konto (SPEC §4, manuell v1-kobling).
// Unik per tenant håndheves av partiell unik-indeks -> unique-violation mappes
// til ErrAccountTaken (HTTP 409).
func (s *PgStore) SetAccount(ctx context.Context, tenantID, id uuid.UUID, accountID string) (Person, error) {
	accountID, err := validateAccountID(accountID)
	if err != nil {
		return Person{}, err
	}
	return s.setAccount(ctx, tenantID, id, &accountID)
}

// ClearAccount fjerner account-koblingen (unlink). Idempotent på verdien.
func (s *PgStore) ClearAccount(ctx context.Context, tenantID, id uuid.UUID) (Person, error) {
	return s.setAccount(ctx, tenantID, id, nil)
}

func (s *PgStore) setAccount(ctx context.Context, tenantID, id uuid.UUID, accountID *string) (Person, error) {
	var p Person
	err := s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`UPDATE person SET account_id = $3, updated_at = now()
			 WHERE tenant_id = $1 AND id = $2 RETURNING `+personCols,
			tenantID, id, accountID)
		var scanErr error
		p, scanErr = scanPerson(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if scanErr != nil {
			if isUniqueViolation(scanErr) {
				return ErrAccountTaken
			}
			return fmt.Errorf("set account: %w", scanErr)
		}
		env, evErr := personAccountEvent(p)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Person{}, err
	}
	return p, nil
}

// isUniqueViolation sier om feilen er en Postgres unique_violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ Store = (*PgStore)(nil)
