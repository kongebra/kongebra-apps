package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integrasjonstester mot ekte Postgres. Kjøres kun når COMPETITION_TEST_DATABASE_URL
// peker på en admin/superuser-DSN (ellers skip - CI PR-sjekken har ingen DB).
// RLS testes ærlig: superuser BYPASSER RLS, så testene kjører via en dedikert
// ikke-superuser-rolle (SET ROLE på hver pooled connection) - slik prod-brukeren
// (SPEC §8: egen DB-bruker per tjeneste) faktisk møter policyene.
const testRole = "competition_rls_test"

func testEnv(t *testing.T) (admin, app *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("COMPETITION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("COMPETITION_TEST_DATABASE_URL ikke satt - hopper over integrasjonstest")
	}
	ctx := context.Background()

	if err := runMigrations(dsn); err != nil {
		t.Fatalf("migrasjoner: %v", err)
	}

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	t.Cleanup(admin.Close)

	// Ikke-superuser test-rolle + rettigheter. Idempotent.
	stmts := []string{
		`DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '` + testRole + `') THEN CREATE ROLE ` + testRole + ` NOSUPERUSER; END IF; END $$`,
		`GRANT USAGE ON SCHEMA public TO ` + testRole,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON tournament, game, team, team_member, participant, placement_result, outbox, tenant_projection TO ` + testRole,
	}
	for _, s := range stmts {
		if _, err := admin.Exec(ctx, s); err != nil {
			t.Fatalf("oppsett-rolle (%s): %v", s, err)
		}
	}
	if _, err := admin.Exec(ctx, `TRUNCATE tournament, game, team, team_member, participant, placement_result, outbox, tenant_projection`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	// Hver connection kjører som ikke-superuser-rollen -> RLS håndheves.
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+testRole)
		return err
	}
	app, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("app pool: %v", err)
	}
	t.Cleanup(app.Close)
	return admin, app
}

// seedGame oppretter tournament + game i en tenant og returnerer game-ID.
func seedGame(t *testing.T, store *PgStore, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tour, err := store.CreateTournament(ctx, tenant, TournamentInput{Name: "TL 2026", Year: intptr(2026)})
	if err != nil {
		t.Fatalf("CreateTournament: %v", err)
	}
	game, err := store.CreateGame(ctx, tenant, GameInput{TournamentID: tour.ID, Title: "Quiz", Category: "quiz"})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	return game.ID
}

func TestIntegrationFullFlowAndEvents(t *testing.T) {
	admin, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenant := uuid.New()

	tour, err := store.CreateTournament(ctx, tenant, TournamentInput{Name: "TL 2026", Year: intptr(2026)})
	if err != nil {
		t.Fatalf("CreateTournament: %v", err)
	}
	assertOutbox(t, admin, "tl.competition.tournament.created", 1)

	game, err := store.CreateGame(ctx, tenant, GameInput{TournamentID: tour.ID, Title: "Quiz", Category: "quiz", RequiresApproval: true})
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if game.Status != GameStatusOpen || !game.RequiresApproval {
		t.Fatalf("game = %+v", game)
	}
	assertOutbox(t, admin, "tl.competition.game.created", 1)

	// Game mot ukjent tournament -> ErrRefNotFound (intra-service FK).
	if _, err := store.CreateGame(ctx, tenant, GameInput{TournamentID: uuid.New(), Title: "x", Category: "y"}); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("CreateGame ukjent tournament = %v, vil ha ErrRefNotFound", err)
	}

	// Finalize -> event + låst.
	fin, err := store.FinalizeGame(ctx, tenant, game.ID)
	if err != nil || fin.Status != GameStatusFinalized {
		t.Fatalf("FinalizeGame = %+v/%v", fin, err)
	}
	assertOutbox(t, admin, "tl.competition.game.finalized", 1)

	// Registrering mot finalized game -> ErrGameFinalized.
	pid := uuid.New()
	if _, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: game.ID, Type: ParticipantPerson, PersonID: &pid}); !errors.Is(err, ErrGameFinalized) {
		t.Fatalf("Register mot finalized = %v, vil ha ErrGameFinalized", err)
	}
}

func TestIntegrationParticipantPersonVsTeam(t *testing.T) {
	admin, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenant := uuid.New()
	gameID := seedGame(t, store, tenant)

	// Person-deltaker (person_id lagres som verdi - ingen FK).
	personID := uuid.New()
	pp, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: gameID, Type: ParticipantPerson, PersonID: &personID})
	if err != nil || pp.Type != ParticipantPerson || pp.PersonID == nil || *pp.PersonID != personID || pp.TeamID != nil {
		t.Fatalf("person-deltaker = %+v/%v", pp, err)
	}
	assertOutbox(t, admin, "tl.competition.participation.registered", 1)

	// Samme person igjen -> ErrConflict (unik per game).
	if _, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: gameID, Type: ParticipantPerson, PersonID: &personID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplikat person = %v, vil ha ErrConflict", err)
	}

	// Team med medlemmer (person_id-verdier).
	m1, m2 := uuid.New(), uuid.New()
	team, err := store.CreateTeam(ctx, tenant, TeamInput{GameID: gameID, Name: "Laget", Members: []uuid.UUID{m1, m2}})
	if err != nil || len(team.Members) != 2 {
		t.Fatalf("CreateTeam = %+v/%v", team, err)
	}
	// Verifiser medlemmer lagret.
	got, err := store.GetTeam(ctx, tenant, team.ID)
	if err != nil || len(got.Members) != 2 {
		t.Fatalf("GetTeam = %+v/%v", got, err)
	}

	// Team-deltaker.
	tp, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: gameID, Type: ParticipantTeam, TeamID: &team.ID})
	if err != nil || tp.Type != ParticipantTeam || tp.TeamID == nil || *tp.TeamID != team.ID || tp.PersonID != nil {
		t.Fatalf("team-deltaker = %+v/%v", tp, err)
	}

	// Team-deltaker mot ukjent team -> ErrRefNotFound.
	ghost := uuid.New()
	if _, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: gameID, Type: ParticipantTeam, TeamID: &ghost}); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("ukjent team = %v, vil ha ErrRefNotFound", err)
	}
}

func TestIntegrationPlacementTies(t *testing.T) {
	admin, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenant := uuid.New()
	gameID := seedGame(t, store, tenant)

	// Tre person-deltakere.
	var parts []uuid.UUID
	for range 3 {
		pid := uuid.New()
		p, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: gameID, Type: ParticipantPerson, PersonID: &pid})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		parts = append(parts, p.ID)
	}

	// Plasseringsliste MED TIES: to på 1. plass, én på 3.
	results, err := store.RecordResults(ctx, tenant, gameID, []PlacementInput{
		{ParticipantID: parts[0], Rank: 1},
		{ParticipantID: parts[1], Rank: 1},
		{ParticipantID: parts[2], Rank: 3},
	})
	if err != nil {
		t.Fatalf("RecordResults: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, vil ha 3", len(results))
	}
	assertOutbox(t, admin, "tl.competition.result.recorded", 1)

	// Les tilbake og verifiser at ties faktisk er lagret (to rader med rank 1).
	stored, err := store.ListResults(ctx, tenant, gameID)
	if err != nil {
		t.Fatalf("ListResults: %v", err)
	}
	rank1 := 0
	for _, r := range stored {
		if r.Rank == 1 {
			rank1++
		}
	}
	if rank1 != 2 {
		t.Fatalf("rank-1 plasseringer = %d, vil ha 2 (ties skal lagres)", rank1)
	}

	// Replace-semantikk: ny liste erstatter den gamle.
	replaced, err := store.RecordResults(ctx, tenant, gameID, []PlacementInput{{ParticipantID: parts[0], Rank: 1}})
	if err != nil || len(replaced) != 1 {
		t.Fatalf("replace = %+v/%v", replaced, err)
	}
	after, _ := store.ListResults(ctx, tenant, gameID)
	if len(after) != 1 {
		t.Fatalf("etter replace = %d rader, vil ha 1", len(after))
	}

	// Plassering mot deltaker som ikke tilhører gamet -> ErrRefNotFound.
	if _, err := store.RecordResults(ctx, tenant, gameID, []PlacementInput{{ParticipantID: uuid.New(), Rank: 1}}); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("fremmed deltaker = %v, vil ha ErrRefNotFound", err)
	}
}

func TestIntegrationRLSTenantIsolation(t *testing.T) {
	admin, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenantA, tenantB := uuid.New(), uuid.New()

	if _, err := store.CreateTournament(ctx, tenantA, TournamentInput{Name: "A-turnering"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if _, err := store.CreateTournament(ctx, tenantB, TournamentInput{Name: "B-turnering"}); err != nil {
		t.Fatalf("Create B: %v", err)
	}

	// App-lag: tenant B ser ikke A.
	listB, err := store.ListTournaments(ctx, tenantB)
	if err != nil || len(listB) != 1 || listB[0].Name != "B-turnering" {
		t.Fatalf("List(B) = %+v/%v, vil ha kun B-turnering", listB, err)
	}

	// RLS-lag: rå count uten WHERE med app.tenant_id = A skal kun se A (RLS).
	if n := rawCountWithTenant(t, app, "tournament", tenantA); n != 1 {
		t.Fatalf("RLS: rå count tenant=A ga %d, vil ha 1 (B lekker gjennom!)", n)
	}
	// Sanity: admin (superuser, bypasser RLS) ser begge.
	var total int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM tournament`).Scan(&total); err != nil {
		t.Fatalf("admin count: %v", err)
	}
	if total != 2 {
		t.Fatalf("admin ser %d, vil ha 2", total)
	}
}

func TestIntegrationRLSWriteCheck(t *testing.T) {
	_, app := testEnv(t)
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()

	// Skriv en tournament-rad for tenant B mens app.tenant_id = A: WITH CHECK
	// i RLS-policyen skal avvise det (SPEC §8: RLS som sikkerhetsnett).
	conn, err := app.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantA.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	id, _ := uuid.NewV7()
	if _, err := tx.Exec(ctx,
		`INSERT INTO tournament (id, tenant_id, name) VALUES ($1, $2, $3)`, id, tenantB, "smuglet"); err == nil {
		t.Fatal("RLS WITH CHECK slapp gjennom skriving til fremmed tenant")
	}
}

// assertOutbox sjekker at det finnes minst want rader med subjectet.
func assertOutbox(t *testing.T, admin *pgxpool.Pool, subject string, want int) {
	t.Helper()
	var n int
	if err := admin.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox WHERE subject = $1`, subject).Scan(&n); err != nil {
		t.Fatalf("count outbox %s: %v", subject, err)
	}
	if n < want {
		t.Errorf("outbox %s = %d rader, vil ha minst %d (event ikke skrevet i tx?)", subject, n, want)
	}
}

// rawCountWithTenant teller rader via en rå spørring uten WHERE-filter, med
// app.tenant_id satt - kun RLS avgjør hva som telles.
func rawCountWithTenant(t *testing.T, app *pgxpool.Pool, table string, tenant uuid.UUID) int {
	t.Helper()
	ctx := context.Background()
	conn, err := app.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("raw count: %v", err)
	}
	return n
}
