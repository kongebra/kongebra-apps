package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tournament er den årlige rammen rundt Games (SPEC §2). Tenant-eid.
type Tournament struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Name        string    `json:"name"`
	Year        *int      `json:"year,omitempty"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TournamentInput er skrive-payloaden for opprett/oppdater.
type TournamentInput struct {
	Name        string  `json:"name"`
	Year        *int    `json:"year"`
	Description *string `json:"description"`
}

// ErrInvalidInput returneres når input-validering feiler (mapper til HTTP 400).
var ErrInvalidInput = errors.New("invalid input")

// normalize validerer og normaliserer input (trimmer navn, tomme optionale
// blir nil). Returnerer feil wrappet i ErrInvalidInput ved ugyldig.
func (in TournamentInput) normalize() (TournamentInput, error) {
	name, err := validateText(in.Name, "name", 200)
	if err != nil {
		return TournamentInput{}, errors.Join(ErrInvalidInput, err)
	}
	if in.Year != nil && (*in.Year < 1900 || *in.Year > 3000) {
		return TournamentInput{}, errors.Join(ErrInvalidInput, errors.New("year er utenfor gyldig område"))
	}
	return TournamentInput{
		Name:        name,
		Year:        in.Year,
		Description: emptyToNil(in.Description),
	}, nil
}

const tournamentCols = `id, tenant_id, name, year, description, created_at, updated_at`

func scanTournament(row pgx.Row) (Tournament, error) {
	var t Tournament
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Year, &t.Description,
		&t.CreatedAt, &t.UpdatedAt); err != nil {
		return Tournament{}, err
	}
	return t, nil
}

func (s *PgStore) CreateTournament(ctx context.Context, tenantID uuid.UUID, in TournamentInput) (Tournament, error) {
	in, err := in.normalize()
	if err != nil {
		return Tournament{}, err
	}
	id, err := newID()
	if err != nil {
		return Tournament{}, err
	}
	var t Tournament
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`INSERT INTO tournament (id, tenant_id, name, year, description)
			 VALUES ($1, $2, $3, $4, $5) RETURNING `+tournamentCols,
			id, tenantID, in.Name, in.Year, in.Description)
		t, err = scanTournament(row)
		if err != nil {
			return fmt.Errorf("insert tournament: %w", err)
		}
		env, evErr := tournamentCreatedEvent(t)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Tournament{}, err
	}
	return t, nil
}

func (s *PgStore) ListTournaments(ctx context.Context, tenantID uuid.UUID) ([]Tournament, error) {
	var out []Tournament
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+tournamentCols+` FROM tournament WHERE tenant_id = $1 ORDER BY year DESC NULLS LAST, name, id`,
			tenantID)
		if err != nil {
			return fmt.Errorf("query tournaments: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTournament(rows)
			if err != nil {
				return fmt.Errorf("scan tournament: %w", err)
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PgStore) GetTournament(ctx context.Context, tenantID, id uuid.UUID) (Tournament, error) {
	var t Tournament
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT `+tournamentCols+` FROM tournament WHERE tenant_id = $1 AND id = $2`,
			tenantID, id)
		var scanErr error
		t, scanErr = scanTournament(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return scanErr
	})
	if err != nil {
		return Tournament{}, err
	}
	return t, nil
}

func (s *PgStore) UpdateTournament(ctx context.Context, tenantID, id uuid.UUID, in TournamentInput) (Tournament, error) {
	in, err := in.normalize()
	if err != nil {
		return Tournament{}, err
	}
	var t Tournament
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`UPDATE tournament SET name = $3, year = $4, description = $5, updated_at = now()
			 WHERE tenant_id = $1 AND id = $2 RETURNING `+tournamentCols,
			tenantID, id, in.Name, in.Year, in.Description)
		var scanErr error
		t, scanErr = scanTournament(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("update tournament: %w", scanErr)
		}
		// ponytail: ingen tournament.updated i event-katalogen (SPEC §9) ennå.
		// Legg den til (via ADR) når en konsument trenger tournament-endringer.
		return nil
	})
	if err != nil {
		return Tournament{}, err
	}
	return t, nil
}

func (s *PgStore) DeleteTournament(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		// ON DELETE CASCADE rydder games/teams/participants/plasseringer.
		tag, err := tx.Exec(ctx,
			`DELETE FROM tournament WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		if err != nil {
			return fmt.Errorf("delete tournament: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// --- delte input-helpers ---

// validateText trimmer og lengdesjekker en påkrevd tekstverdi.
func validateText(v, field string, maxLen int) (string, error) {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return "", fmt.Errorf("%s er påkrevd", field)
	}
	if len(trimmed) > maxLen {
		return "", fmt.Errorf("%s er for langt (maks %d tegn)", field, maxLen)
	}
	return trimmed, nil
}

// emptyToNil gjør en peker til tom/whitespace-streng om til nil, ellers trimmet.
func emptyToNil(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
