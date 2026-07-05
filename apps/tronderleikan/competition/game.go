package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Game-statuser (SPEC §3): open kan endres/punches, finalized er låst.
const (
	GameStatusOpen      = "open"
	GameStatusFinalized = "finalized"
)

// Game er en konkurranse i et Tournament (SPEC §2, §3). Category er fritt
// definerbar per tenant og brukes av rating (fase 3). RequiresApproval gjelder
// open-entry-innsendinger (SPEC §3, default av).
type Game struct {
	ID               uuid.UUID `json:"id"`
	TenantID         uuid.UUID `json:"tenant_id"`
	TournamentID     uuid.UUID `json:"tournament_id"`
	Title            string    `json:"title"`
	Description      *string   `json:"description,omitempty"`
	Category         string    `json:"category"`
	RequiresApproval bool      `json:"requires_approval"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// GameInput er skrive-payloaden for opprett/oppdater. TournamentID kreves kun ved
// opprett (ignoreres ved oppdater - et game flyttes ikke mellom tournaments i v1).
type GameInput struct {
	TournamentID     uuid.UUID `json:"tournament_id"`
	Title            string    `json:"title"`
	Description      *string   `json:"description"`
	Category         string    `json:"category"`
	RequiresApproval bool      `json:"requires_approval"`
}

func (in GameInput) normalize(requireTournament bool) (GameInput, error) {
	title, err := validateText(in.Title, "title", 200)
	if err != nil {
		return GameInput{}, errors.Join(ErrInvalidInput, err)
	}
	category, err := validateText(in.Category, "category", 100)
	if err != nil {
		return GameInput{}, errors.Join(ErrInvalidInput, err)
	}
	if requireTournament && in.TournamentID == uuid.Nil {
		return GameInput{}, errors.Join(ErrInvalidInput, errors.New("tournament_id er påkrevd"))
	}
	return GameInput{
		TournamentID:     in.TournamentID,
		Title:            title,
		Description:      emptyToNil(in.Description),
		Category:         category,
		RequiresApproval: in.RequiresApproval,
	}, nil
}

const gameCols = `id, tenant_id, tournament_id, title, description, category, requires_approval, status, created_at, updated_at`

func scanGame(row pgx.Row) (Game, error) {
	var g Game
	if err := row.Scan(&g.ID, &g.TenantID, &g.TournamentID, &g.Title, &g.Description,
		&g.Category, &g.RequiresApproval, &g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return Game{}, err
	}
	return g, nil
}

func (s *PgStore) CreateGame(ctx context.Context, tenantID uuid.UUID, in GameInput) (Game, error) {
	in, err := in.normalize(true)
	if err != nil {
		return Game{}, err
	}
	id, err := newID()
	if err != nil {
		return Game{}, err
	}
	var g Game
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`INSERT INTO game (id, tenant_id, tournament_id, title, description, category, requires_approval)
			 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+gameCols,
			id, tenantID, in.TournamentID, in.Title, in.Description, in.Category, in.RequiresApproval)
		g, err = scanGame(row)
		if err != nil {
			// Ukjent tournament (FK) -> tydelig ref-feil (SPEC §7).
			if isForeignKeyViolation(err) {
				return fmt.Errorf("%w: tournament %s", ErrRefNotFound, in.TournamentID)
			}
			return fmt.Errorf("insert game: %w", err)
		}
		env, evErr := gameCreatedEvent(g)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Game{}, err
	}
	return g, nil
}

func (s *PgStore) ListGames(ctx context.Context, tenantID uuid.UUID, tournamentID *uuid.UUID) ([]Game, error) {
	var out []Game
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		var rows pgx.Rows
		var err error
		if tournamentID != nil {
			rows, err = tx.Query(ctx,
				`SELECT `+gameCols+` FROM game WHERE tenant_id = $1 AND tournament_id = $2 ORDER BY title, id`,
				tenantID, *tournamentID)
		} else {
			rows, err = tx.Query(ctx,
				`SELECT `+gameCols+` FROM game WHERE tenant_id = $1 ORDER BY title, id`,
				tenantID)
		}
		if err != nil {
			return fmt.Errorf("query games: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGame(rows)
			if err != nil {
				return fmt.Errorf("scan game: %w", err)
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PgStore) GetGame(ctx context.Context, tenantID, id uuid.UUID) (Game, error) {
	var g Game
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT `+gameCols+` FROM game WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		var scanErr error
		g, scanErr = scanGame(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return scanErr
	})
	if err != nil {
		return Game{}, err
	}
	return g, nil
}

func (s *PgStore) UpdateGame(ctx context.Context, tenantID, id uuid.UUID, in GameInput) (Game, error) {
	in, err := in.normalize(false)
	if err != nil {
		return Game{}, err
	}
	var g Game
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`UPDATE game SET title = $3, description = $4, category = $5, requires_approval = $6, updated_at = now()
			 WHERE tenant_id = $1 AND id = $2 RETURNING `+gameCols,
			tenantID, id, in.Title, in.Description, in.Category, in.RequiresApproval)
		var scanErr error
		g, scanErr = scanGame(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("update game: %w", scanErr)
		}
		env, evErr := gameUpdatedEvent(g)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Game{}, err
	}
	return g, nil
}

// FinalizeGame låser et game (status -> finalized) og fyrer game.finalized
// (SPEC §9). Idempotent: allerede finalized game returneres uendret uten nytt
// event. Ukjent game -> ErrNotFound.
func (s *PgStore) FinalizeGame(ctx context.Context, tenantID, id uuid.UUID) (Game, error) {
	var g Game
	err := s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		// Les gjeldende status først (RLS aktiv).
		row := tx.QueryRow(ctx,
			`SELECT `+gameCols+` FROM game WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id)
		cur, scanErr := scanGame(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if scanErr != nil {
			return fmt.Errorf("read game for finalize: %w", scanErr)
		}
		if cur.Status == GameStatusFinalized {
			g = cur // allerede låst - ingen endring, intet nytt event (idempotent)
			return nil
		}
		row = tx.QueryRow(ctx,
			`UPDATE game SET status = $3, updated_at = now()
			 WHERE tenant_id = $1 AND id = $2 RETURNING `+gameCols,
			tenantID, id, GameStatusFinalized)
		var updErr error
		g, updErr = scanGame(row)
		if updErr != nil {
			return fmt.Errorf("finalize game: %w", updErr)
		}
		env, evErr := gameFinalizedEvent(g)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Game{}, err
	}
	return g, nil
}

func (s *PgStore) DeleteGame(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		// ON DELETE CASCADE rydder teams/participants/plasseringer.
		tag, err := tx.Exec(ctx,
			`DELETE FROM game WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		if err != nil {
			return fmt.Errorf("delete game: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}
