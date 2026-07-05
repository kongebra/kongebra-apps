package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Team er et lag i ETT Game (SPEC §2). Members er VERDI-referanser til
// roster.person (SPEC §8) - validert mot roster FØR persist (SPEC §7).
type Team struct {
	ID        uuid.UUID   `json:"id"`
	TenantID  uuid.UUID   `json:"tenant_id"`
	GameID    uuid.UUID   `json:"game_id"`
	Name      string      `json:"name"`
	Members   []uuid.UUID `json:"members"` // person_id-er (roster)
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// TeamInput er skrive-payloaden for å opprette et lag med medlemmer.
type TeamInput struct {
	GameID  uuid.UUID   `json:"game_id"`
	Name    string      `json:"name"`
	Members []uuid.UUID `json:"members"`
}

func (in TeamInput) normalize() (TeamInput, error) {
	name, err := validateText(in.Name, "name", 200)
	if err != nil {
		return TeamInput{}, errors.Join(ErrInvalidInput, err)
	}
	if in.GameID == uuid.Nil {
		return TeamInput{}, errors.Join(ErrInvalidInput, errors.New("game_id er påkrevd"))
	}
	// Dedup medlemmer, avvis nil-UUID.
	seen := map[uuid.UUID]struct{}{}
	members := make([]uuid.UUID, 0, len(in.Members))
	for _, m := range in.Members {
		if m == uuid.Nil {
			return TeamInput{}, errors.Join(ErrInvalidInput, errors.New("medlem person_id kan ikke være tom"))
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		members = append(members, m)
	}
	return TeamInput{GameID: in.GameID, Name: name, Members: members}, nil
}

func (s *PgStore) CreateTeam(ctx context.Context, tenantID uuid.UUID, in TeamInput) (Team, error) {
	in, err := in.normalize()
	if err != nil {
		return Team{}, err
	}
	id, err := newID()
	if err != nil {
		return Team{}, err
	}
	var t Team
	err = s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`INSERT INTO team (id, tenant_id, game_id, name)
			 VALUES ($1, $2, $3, $4) RETURNING id, tenant_id, game_id, name, created_at, updated_at`,
			id, tenantID, in.GameID, in.Name)
		if err := row.Scan(&t.ID, &t.TenantID, &t.GameID, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
			if isForeignKeyViolation(err) {
				return fmt.Errorf("%w: game %s", ErrRefNotFound, in.GameID)
			}
			return fmt.Errorf("insert team: %w", err)
		}
		for _, personID := range in.Members {
			if _, err := tx.Exec(ctx,
				`INSERT INTO team_member (team_id, tenant_id, person_id) VALUES ($1, $2, $3)`,
				t.ID, tenantID, personID); err != nil {
				return fmt.Errorf("insert team member: %w", err)
			}
		}
		t.Members = in.Members
		if t.Members == nil {
			t.Members = []uuid.UUID{}
		}
		// ponytail: intet team.created-event i katalogen (SPEC §9). Lag blir
		// synlige via participation.registered når laget meldes på et game.
		return nil
	})
	if err != nil {
		return Team{}, err
	}
	return t, nil
}

func (s *PgStore) ListTeams(ctx context.Context, tenantID, gameID uuid.UUID) ([]Team, error) {
	var out []Team
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, tenant_id, game_id, name, created_at, updated_at
			 FROM team WHERE tenant_id = $1 AND game_id = $2 ORDER BY name, id`,
			tenantID, gameID)
		if err != nil {
			return fmt.Errorf("query teams: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var t Team
			if err := rows.Scan(&t.ID, &t.TenantID, &t.GameID, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
				return fmt.Errorf("scan team: %w", err)
			}
			out = append(out, t)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Medlemmer per lag (egen spørring, RLS aktiv i samme tx).
		for i := range out {
			members, err := teamMembers(ctx, tx, tenantID, out[i].ID)
			if err != nil {
				return err
			}
			out[i].Members = members
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PgStore) GetTeam(ctx context.Context, tenantID, id uuid.UUID) (Team, error) {
	var t Team
	err := s.readTx(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx,
			`SELECT id, tenant_id, game_id, name, created_at, updated_at
			 FROM team WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		if err := row.Scan(&t.ID, &t.TenantID, &t.GameID, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("get team: %w", err)
		}
		members, err := teamMembers(ctx, tx, tenantID, t.ID)
		if err != nil {
			return err
		}
		t.Members = members
		return nil
	})
	if err != nil {
		return Team{}, err
	}
	return t, nil
}

func (s *PgStore) DeleteTeam(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.tx(ctx, tenantID, func(tx pgx.Tx) error {
		// ON DELETE CASCADE rydder team_member og participant-rader for laget.
		tag, err := tx.Exec(ctx,
			`DELETE FROM team WHERE tenant_id = $1 AND id = $2`, tenantID, id)
		if err != nil {
			return fmt.Errorf("delete team: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// teamMembers henter person_id-ene for et lag (sortert deterministisk).
func teamMembers(ctx context.Context, tx pgx.Tx, tenantID, teamID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx,
		`SELECT person_id FROM team_member WHERE tenant_id = $1 AND team_id = $2 ORDER BY person_id`,
		tenantID, teamID)
	if err != nil {
		return nil, fmt.Errorf("query team members: %w", err)
	}
	defer rows.Close()
	members := []uuid.UUID{}
	for rows.Next() {
		var pid uuid.UUID
		if err := rows.Scan(&pid); err != nil {
			return nil, fmt.Errorf("scan team member: %w", err)
		}
		members = append(members, pid)
	}
	return members, rows.Err()
}
