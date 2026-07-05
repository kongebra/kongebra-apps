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
	// ErrNotFound: ingen rad med ID-en i tenanten (mapper til HTTP 404).
	ErrNotFound = errors.New("not found")
	// ErrRefNotFound: en referert entitet finnes ikke (tournament for et game,
	// team/participant for en registrering osv.). Mapper til HTTP 422.
	ErrRefNotFound = errors.New("referenced entity does not exist")
	// ErrConflict: unik-brudd (f.eks. deltaker allerede påmeldt). HTTP 409.
	ErrConflict = errors.New("conflict")
	// ErrGameFinalized: skriveoperasjon mot et låst (finalized) game. HTTP 409.
	ErrGameFinalized = errors.New("game is finalized")
)

// Store er competition-persistensen slik handlerne ser den (fakes i handler-test).
type Store interface {
	// Tournament
	CreateTournament(ctx context.Context, tenantID uuid.UUID, in TournamentInput) (Tournament, error)
	ListTournaments(ctx context.Context, tenantID uuid.UUID) ([]Tournament, error)
	GetTournament(ctx context.Context, tenantID, id uuid.UUID) (Tournament, error)
	UpdateTournament(ctx context.Context, tenantID, id uuid.UUID, in TournamentInput) (Tournament, error)
	DeleteTournament(ctx context.Context, tenantID, id uuid.UUID) error

	// Game
	CreateGame(ctx context.Context, tenantID uuid.UUID, in GameInput) (Game, error)
	ListGames(ctx context.Context, tenantID uuid.UUID, tournamentID *uuid.UUID) ([]Game, error)
	GetGame(ctx context.Context, tenantID, id uuid.UUID) (Game, error)
	UpdateGame(ctx context.Context, tenantID, id uuid.UUID, in GameInput) (Game, error)
	FinalizeGame(ctx context.Context, tenantID, id uuid.UUID) (Game, error)
	DeleteGame(ctx context.Context, tenantID, id uuid.UUID) error

	// Team (person-refs i medlemslisten valideres mot roster FØR dette kalles)
	CreateTeam(ctx context.Context, tenantID uuid.UUID, in TeamInput) (Team, error)
	ListTeams(ctx context.Context, tenantID, gameID uuid.UUID) ([]Team, error)
	GetTeam(ctx context.Context, tenantID, id uuid.UUID) (Team, error)
	DeleteTeam(ctx context.Context, tenantID, id uuid.UUID) error

	// Participant (person-ref valideres mot roster FØR dette kalles)
	RegisterParticipant(ctx context.Context, tenantID uuid.UUID, in ParticipantInput) (Participant, error)
	ListParticipants(ctx context.Context, tenantID, gameID uuid.UUID) ([]Participant, error)
	GetParticipant(ctx context.Context, tenantID, id uuid.UUID) (Participant, error)
	DeleteParticipant(ctx context.Context, tenantID, id uuid.UUID) error

	// PlacementResult
	RecordResults(ctx context.Context, tenantID, gameID uuid.UUID, placements []PlacementInput) ([]PlacementResult, error)
	ListResults(ctx context.Context, tenantID, gameID uuid.UUID) ([]PlacementResult, error)
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

// readTx kjører en lese-operasjon i en tenant-scopet tx (RLS aktiv). Bruker
// samme tenant-sikkerhetsnett som skriv, men committer bare lesingen.
func (s *PgStore) readTx(ctx context.Context, tenantID uuid.UUID, fn func(pgx.Tx) error) error {
	return s.tx(ctx, tenantID, fn)
}

// writeEvent skriver et event til outbox i den gjeldende tx-en (SPEC §9).
func writeEvent(ctx context.Context, tx pgx.Tx, env event.Envelope, err error) error {
	if err != nil {
		return fmt.Errorf("build event: %w", err)
	}
	return outbox.Write(ctx, tx, env)
}

// newID genererer en UUIDv7 (SPEC §8: UUIDv7 overalt, generert i app).
func newID() (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate id: %w", err)
	}
	return id, nil
}

// isUniqueViolation sier om feilen er en Postgres unique_violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation sier om feilen er en Postgres foreign_key_violation
// (23503) - en referert intra-service rad (tournament/team/participant) mangler.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

var _ Store = (*PgStore)(nil)
