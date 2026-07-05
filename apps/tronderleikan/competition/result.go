package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PlacementResult er én deltakers plassering i et Game (SPEC §3). Peker på
// participant (aldri Person direkte, SPEC §2). Ties er lov: flere plasseringer
// kan dele rank.
type PlacementResult struct {
	ID            uuid.UUID `json:"id"`
	TenantID      uuid.UUID `json:"tenant_id"`
	GameID        uuid.UUID `json:"game_id"`
	ParticipantID uuid.UUID `json:"participant_id"`
	Rank          int       `json:"rank"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PlacementInput er én linje i en plasseringsliste (participant + rank).
type PlacementInput struct {
	ParticipantID uuid.UUID `json:"participant_id"`
	Rank          int       `json:"rank"`
}

// normalizePlacements validerer en hel plasseringsliste: rank >= 1, ingen
// deltaker oppført to ganger (ties er PÅ rank, ikke duplikate deltakere).
// Ties (flere med samme rank) er eksplisitt tillatt (SPEC §2, §3).
func normalizePlacements(placements []PlacementInput) ([]PlacementInput, error) {
	if len(placements) == 0 {
		return nil, errors.Join(ErrInvalidInput, errors.New("placements kan ikke være tom"))
	}
	seen := map[uuid.UUID]struct{}{}
	out := make([]PlacementInput, 0, len(placements))
	for _, p := range placements {
		if p.ParticipantID == uuid.Nil {
			return nil, errors.Join(ErrInvalidInput, errors.New("participant_id er påkrevd"))
		}
		if p.Rank < 1 {
			return nil, errors.Join(ErrInvalidInput, fmt.Errorf("rank må være >= 1 (fikk %d)", p.Rank))
		}
		if _, dup := seen[p.ParticipantID]; dup {
			return nil, errors.Join(ErrInvalidInput, errors.New("samme participant oppført flere ganger"))
		}
		seen[p.ParticipantID] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

// RecordResults erstatter plasseringslisten for et game atomisk og fyrer
// result.recorded (SPEC §9). Guards: game må finnes og være open; hver
// participant må tilhøre dette gamet. Ties bevares (ingen unik på rank).
func (s *PgStore) RecordResults(ctx context.Context, tenantID, gameID uuid.UUID, placements []PlacementInput) ([]PlacementResult, error) {
	placements, err := normalizePlacements(placements)
	if err != nil {
		return nil, err
	}
	var out []PlacementResult
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := ensureGameOpen(ctx, tx, tenantID, gameID); err != nil {
			return err
		}
		// Alle deltakere må tilhøre dette gamet (RLS aktiv -> kun egen tenant).
		valid, err := gameParticipantIDs(ctx, tx, tenantID, gameID)
		if err != nil {
			return err
		}
		for _, p := range placements {
			if _, ok := valid[p.ParticipantID]; !ok {
				return fmt.Errorf("%w: participant %s tilhører ikke game %s", ErrRefNotFound, p.ParticipantID, gameID)
			}
		}
		// Erstatt eksisterende plasseringer (replace-semantikk).
		if _, err := tx.Exec(ctx,
			`DELETE FROM placement_result WHERE tenant_id = $1 AND game_id = $2`, tenantID, gameID); err != nil {
			return fmt.Errorf("clear placements: %w", err)
		}
		out = make([]PlacementResult, 0, len(placements))
		for _, p := range placements {
			id, err := newID()
			if err != nil {
				return err
			}
			row := tx.QueryRow(ctx,
				`INSERT INTO placement_result (id, tenant_id, game_id, participant_id, rank)
				 VALUES ($1, $2, $3, $4, $5)
				 RETURNING id, tenant_id, game_id, participant_id, rank, created_at, updated_at`,
				id, tenantID, gameID, p.ParticipantID, p.Rank)
			var pr PlacementResult
			if err := row.Scan(&pr.ID, &pr.TenantID, &pr.GameID, &pr.ParticipantID,
				&pr.Rank, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
				return fmt.Errorf("insert placement: %w", err)
			}
			out = append(out, pr)
		}
		env, evErr := resultRecordedEvent(tenantID, gameID, out)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PgStore) ListResults(ctx context.Context, tenantID, gameID uuid.UUID) ([]PlacementResult, error) {
	var out []PlacementResult
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, tenant_id, game_id, participant_id, rank, created_at, updated_at
			 FROM placement_result WHERE tenant_id = $1 AND game_id = $2 ORDER BY rank, participant_id`,
			tenantID, gameID)
		if err != nil {
			return fmt.Errorf("query placements: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var pr PlacementResult
			if err := rows.Scan(&pr.ID, &pr.TenantID, &pr.GameID, &pr.ParticipantID,
				&pr.Rank, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
				return fmt.Errorf("scan placement: %w", err)
			}
			out = append(out, pr)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// gameParticipantIDs henter settet av participant-ID-er som tilhører et game.
func gameParticipantIDs(ctx context.Context, tx pgx.Tx, tenantID, gameID uuid.UUID) (map[uuid.UUID]struct{}, error) {
	rows, err := tx.Query(ctx,
		`SELECT id FROM participant WHERE tenant_id = $1 AND game_id = $2`, tenantID, gameID)
	if err != nil {
		return nil, fmt.Errorf("query game participants: %w", err)
	}
	defer rows.Close()
	ids := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan participant id: %w", err)
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}
