package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Deltakertyper (SPEC §2): en participant er enten en Person eller et Team.
const (
	ParticipantPerson = "person"
	ParticipantTeam   = "team"
)

// Participant er en deltaker i ETT Game (SPEC §2). Egen entitet fra dag 1 slik
// at individ og lag behandles likt (aldri Person direkte i resultat-tabeller).
// PersonID er en VERDI-ref til roster (SPEC §8), TeamID en FK innad.
type Participant struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	GameID    uuid.UUID  `json:"game_id"`
	Type      string     `json:"type"`
	PersonID  *uuid.UUID `json:"person_id,omitempty"`
	TeamID    *uuid.UUID `json:"team_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ParticipantInput er påmeldings-payloaden. Nøyaktig ett av person_id/team_id
// settes, styrt av type.
type ParticipantInput struct {
	GameID   uuid.UUID  `json:"game_id"`
	Type     string     `json:"type"`
	PersonID *uuid.UUID `json:"person_id"`
	TeamID   *uuid.UUID `json:"team_id"`
}

func (in ParticipantInput) normalize() (ParticipantInput, error) {
	if in.GameID == uuid.Nil {
		return ParticipantInput{}, errors.Join(ErrInvalidInput, errors.New("game_id er påkrevd"))
	}
	switch in.Type {
	case ParticipantPerson:
		if in.PersonID == nil || *in.PersonID == uuid.Nil {
			return ParticipantInput{}, errors.Join(ErrInvalidInput, errors.New("person_id er påkrevd for type=person"))
		}
		if in.TeamID != nil {
			return ParticipantInput{}, errors.Join(ErrInvalidInput, errors.New("team_id kan ikke settes for type=person"))
		}
		return ParticipantInput{GameID: in.GameID, Type: ParticipantPerson, PersonID: in.PersonID}, nil
	case ParticipantTeam:
		if in.TeamID == nil || *in.TeamID == uuid.Nil {
			return ParticipantInput{}, errors.Join(ErrInvalidInput, errors.New("team_id er påkrevd for type=team"))
		}
		if in.PersonID != nil {
			return ParticipantInput{}, errors.Join(ErrInvalidInput, errors.New("person_id kan ikke settes for type=team"))
		}
		return ParticipantInput{GameID: in.GameID, Type: ParticipantTeam, TeamID: in.TeamID}, nil
	default:
		return ParticipantInput{}, errors.Join(ErrInvalidInput, fmt.Errorf("ugyldig type %q (person|team)", in.Type))
	}
}

const participantCols = `id, tenant_id, game_id, type, person_id, team_id, created_at`

func scanParticipant(row pgx.Row) (Participant, error) {
	var p Participant
	if err := row.Scan(&p.ID, &p.TenantID, &p.GameID, &p.Type, &p.PersonID, &p.TeamID, &p.CreatedAt); err != nil {
		return Participant{}, err
	}
	return p, nil
}

// RegisterParticipant melder en person/lag på et game (SPEC §7) og fyrer
// participation.registered (SPEC §9). Person-ref er allerede validert mot roster
// i handleren. Guards: game må finnes og være open (ikke finalized).
func (s *PgStore) RegisterParticipant(ctx context.Context, tenantID uuid.UUID, in ParticipantInput) (Participant, error) {
	in, err := in.normalize()
	if err != nil {
		return Participant{}, err
	}
	id, err := newID()
	if err != nil {
		return Participant{}, err
	}
	var p Participant
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := ensureGameOpen(ctx, tx, tenantID, in.GameID); err != nil {
			return err
		}
		row := tx.QueryRow(ctx,
			`INSERT INTO participant (id, tenant_id, game_id, type, person_id, team_id)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+participantCols,
			id, tenantID, in.GameID, in.Type, in.PersonID, in.TeamID)
		var scanErr error
		p, scanErr = scanParticipant(row)
		if scanErr != nil {
			if isUniqueViolation(scanErr) {
				return fmt.Errorf("%w: deltaker allerede påmeldt dette game", ErrConflict)
			}
			if isForeignKeyViolation(scanErr) {
				// team_id peker på et team som ikke finnes (person_id har ingen FK).
				return fmt.Errorf("%w: team finnes ikke", ErrRefNotFound)
			}
			return fmt.Errorf("insert participant: %w", scanErr)
		}
		env, evErr := participationRegisteredEvent(p)
		return writeEvent(ctx, tx, env, evErr)
	})
	if err != nil {
		return Participant{}, err
	}
	return p, nil
}

func (s *PgStore) ListParticipants(ctx context.Context, tenantID, gameID uuid.UUID) ([]Participant, error) {
	var out []Participant
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT `+participantCols+` FROM participant WHERE tenant_id = $1 AND game_id = $2 ORDER BY created_at, id`,
			tenantID, gameID)
		if err != nil {
			return fmt.Errorf("query participants: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanParticipant(rows)
			if err != nil {
				return fmt.Errorf("scan participant: %w", err)
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PgStore) GetParticipant(ctx context.Context, tenantID, id uuid.UUID) (Participant, error) {
	var p Participant
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT `+participantCols+` FROM participant WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		var scanErr error
		p, scanErr = scanParticipant(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return scanErr
	})
	if err != nil {
		return Participant{}, err
	}
	return p, nil
}

func (s *PgStore) DeleteParticipant(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`DELETE FROM participant WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		if err != nil {
			return fmt.Errorf("delete participant: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ensureGameOpen sjekker at gamet finnes i tenanten og ikke er finalized.
// Kjøres inni en tenant-scopet tx (RLS aktiv).
func ensureGameOpen(ctx context.Context, tx pgx.Tx, tenantID, gameID uuid.UUID) error {
	var status string
	err := tx.QueryRow(ctx,
		`SELECT status FROM game WHERE tenant_id = $1 AND id = $2`, tenantID, gameID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: game %s", ErrRefNotFound, gameID)
	}
	if err != nil {
		return fmt.Errorf("read game status: %w", err)
	}
	if status == GameStatusFinalized {
		return ErrGameFinalized
	}
	return nil
}
