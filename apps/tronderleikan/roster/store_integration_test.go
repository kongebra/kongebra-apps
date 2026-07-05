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

// Integrasjonstester mot ekte Postgres. Kjøres kun når ROSTER_TEST_DATABASE_URL
// peker på en admin/superuser-DSN (ellers skip - CI PR-sjekken har ingen DB).
// RLS testes ærlig: superuser BYPASSER RLS, så testene kjører via en dedikert
// ikke-superuser-rolle (SET ROLE på hver pooled connection) - slik prod-brukeren
// (SPEC §8: egen DB-bruker per tjeneste) faktisk møter policyene.
const testRole = "roster_rls_test"

func testEnv(t *testing.T) (admin, app *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("ROSTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ROSTER_TEST_DATABASE_URL ikke satt - hopper over integrasjonstest")
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
		`GRANT SELECT, INSERT, UPDATE, DELETE ON person, outbox, tenant_projection TO ` + testRole,
	}
	for _, s := range stmts {
		if _, err := admin.Exec(ctx, s); err != nil {
			t.Fatalf("oppsett-rolle (%s): %v", s, err)
		}
	}
	if _, err := admin.Exec(ctx, `TRUNCATE person, outbox, tenant_projection`); err != nil {
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

func TestIntegrationCRUDAndEvents(t *testing.T) {
	admin, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenant := uuid.New()

	// Create -> event i outbox (SAMME tx)
	p, err := store.Create(ctx, tenant, PersonInput{Name: "Ada", Department: strptr("Eng")})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == uuid.Nil || p.Name != "Ada" || p.Department == nil || *p.Department != "Eng" {
		t.Fatalf("opprettet person = %+v", p)
	}
	assertOutbox(t, admin, "tl.roster.person.created", 1)

	// Get
	got, err := store.Get(ctx, tenant, p.ID)
	if err != nil || got.ID != p.ID {
		t.Fatalf("Get = %+v/%v", got, err)
	}

	// Update
	up, err := store.Update(ctx, tenant, p.ID, PersonInput{Name: "Ada L."})
	if err != nil || up.Name != "Ada L." || up.Department != nil {
		t.Fatalf("Update = %+v/%v (department skulle bli nil)", up, err)
	}
	assertOutbox(t, admin, "tl.roster.person.updated", 1)

	// List
	list, err := store.List(ctx, tenant)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d/%v, vil ha 1", len(list), err)
	}

	// Delete
	if err := store.Delete(ctx, tenant, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, tenant, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get etter Delete = %v, vil ha ErrNotFound", err)
	}
	assertOutbox(t, admin, "tl.roster.person.deleted", 1)
}

func TestIntegrationRLSTenantIsolation(t *testing.T) {
	admin, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenantA, tenantB := uuid.New(), uuid.New()

	if _, err := store.Create(ctx, tenantA, PersonInput{Name: "A-person"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if _, err := store.Create(ctx, tenantB, PersonInput{Name: "B-person"}); err != nil {
		t.Fatalf("Create B: %v", err)
	}

	// App-lag: List(A) ser bare A.
	listA, err := store.List(ctx, tenantA)
	if err != nil || len(listA) != 1 || listA[0].Name != "A-person" {
		t.Fatalf("List(A) = %+v/%v, vil ha kun A-person", listA, err)
	}

	// RLS-lag (uavhengig av app-filter): med app.tenant_id = A skal en RÅ
	// count uten WHERE kun se A sine rader - RLS filtrerer bort B (SPEC §8).
	count := rawCountWithTenant(t, app, tenantA)
	if count != 1 {
		t.Fatalf("RLS: rå count med tenant=A ga %d, vil ha 1 (RLS slipper gjennom Bs rad!)", count)
	}
	// Sanity: totalt finnes 2 rader (sett fra admin/superuser som bypasser RLS).
	var total int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM person`).Scan(&total); err != nil {
		t.Fatalf("admin count: %v", err)
	}
	if total != 2 {
		t.Fatalf("admin ser %d rader totalt, vil ha 2", total)
	}
}

func TestIntegrationRLSWriteCheck(t *testing.T) {
	_, app := testEnv(t)
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()

	// Forsøk å skrive en rad for tenant B mens app.tenant_id = A: WITH CHECK
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
	_, err = tx.Exec(ctx,
		`INSERT INTO person (id, tenant_id, name) VALUES ($1, $2, $3)`,
		id, tenantB, "smuglet inn")
	if err == nil {
		t.Fatal("RLS WITH CHECK slapp gjennom skriving til fremmed tenant")
	}
}

func TestIntegrationAccountUniquePerTenant(t *testing.T) {
	_, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenantA, tenantB := uuid.New(), uuid.New()

	p1, _ := store.Create(ctx, tenantA, PersonInput{Name: "P1"})
	p2, _ := store.Create(ctx, tenantA, PersonInput{Name: "P2"})
	p3, _ := store.Create(ctx, tenantB, PersonInput{Name: "P3"})

	if _, err := store.SetAccount(ctx, tenantA, p1.ID, "sub-shared"); err != nil {
		t.Fatalf("SetAccount p1: %v", err)
	}
	// Samme konto til p2 i SAMME tenant -> 409 (unik per tenant, SPEC §4).
	if _, err := store.SetAccount(ctx, tenantA, p2.ID, "sub-shared"); !errors.Is(err, ErrAccountTaken) {
		t.Fatalf("SetAccount p2 = %v, vil ha ErrAccountTaken", err)
	}
	// Samme konto i ANNEN tenant -> OK (unik kun per tenant).
	linked, err := store.SetAccount(ctx, tenantB, p3.ID, "sub-shared")
	if err != nil || linked.AccountID == nil || *linked.AccountID != "sub-shared" {
		t.Fatalf("SetAccount p3 (annen tenant) = %+v/%v, vil ha kobling OK", linked, err)
	}

	// account_claimed-event skrevet for hver vellykket kobling (2 stk).
	// (p1 + p3; p2 feilet og rullet tilbake -> ingen event.)

	// Unlink er idempotent og fjerner koblingen.
	cleared, err := store.ClearAccount(ctx, tenantA, p1.ID)
	if err != nil || cleared.AccountID != nil {
		t.Fatalf("ClearAccount = %+v/%v, vil ha account_id nil", cleared, err)
	}
}

// assertOutbox sjekker at det finnes minst want rader med subjectet (admin
// bypasser RLS - outbox har uansett ingen RLS).
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

// rawCountWithTenant teller person-rader via en rå spørring uten WHERE-filter,
// med app.tenant_id satt - kun RLS avgjør hva som telles.
func rawCountWithTenant(t *testing.T, app *pgxpool.Pool, tenant uuid.UUID) int {
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
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM person`).Scan(&n); err != nil {
		t.Fatalf("raw count: %v", err)
	}
	return n
}
