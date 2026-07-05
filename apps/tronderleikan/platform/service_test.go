package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

// --- fakes for DB + tx (testcontainer-fri: alt i minne) ---

type fakeRow struct{ exists bool }

func (r fakeRow) Scan(dest ...any) error {
	*(dest[0].(*bool)) = r.exists
	return nil
}

type fakeTx struct {
	pgx.Tx
	tenantArgs []any
	outboxArgs []any
	committed  bool
	rolledBack bool
}

func (t *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(sql, "INSERT INTO tenants"):
		t.tenantArgs = args
	case strings.HasPrefix(sql, "INSERT INTO outbox"):
		t.outboxArgs = args
	default:
		return pgconn.CommandTag{}, errors.New("uventet SQL: " + sql)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTx) Rollback(context.Context) error { t.rolledBack = true; return nil }

type fakePool struct {
	slugExists bool
	tx         *fakeTx
	beginCalls int
}

func (p *fakePool) Begin(context.Context) (pgx.Tx, error) {
	p.beginCalls++
	p.tx = &fakeTx{}
	return p.tx, nil
}

func (p *fakePool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("Query ikke forventet i denne testen")
}

func (p *fakePool) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if !strings.Contains(sql, "SELECT EXISTS") {
		panic("uventet QueryRow: " + sql)
	}
	return fakeRow{exists: p.slugExists}
}

func (p *fakePool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("Exec ikke forventet i denne testen")
}

type fakeProvisioner struct {
	spec    TenantSpec
	calls   int
	result  ProvisionResult
	failErr error
}

func (f *fakeProvisioner) Provision(_ context.Context, spec TenantSpec) (ProvisionResult, error) {
	f.calls++
	f.spec = spec
	if f.failErr != nil {
		return ProvisionResult{}, f.failErr
	}
	return f.result, nil
}

func validInput() CreateTenantInput {
	return CreateTenantInput{
		Name:             "Inmeta Games",
		Slug:             "inmeta",
		PublicVisibility: true,
		Admin: AdminSpec{
			Email:      "admin@inmeta.local",
			GivenName:  "In",
			FamilyName: "Meta",
			Password:   "Password1!",
		},
	}
}

func TestCreateTenantWritesTenantAndEventInSameTx(t *testing.T) {
	pool := &fakePool{}
	prov := &fakeProvisioner{result: ProvisionResult{ZitadelOrgID: "org-x", ProjectGrantID: "grant-x", AdminUserID: "user-x"}}
	svc := NewService(pool, NewRepo(pool), prov, nil)

	tenant, err := svc.CreateTenant(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Provisjonering ble kalt med tenant-navnet + admin.
	if prov.calls != 1 {
		t.Fatalf("Provision-kall = %d, vil ha 1", prov.calls)
	}
	if prov.spec.OrgName != "Inmeta Games" || prov.spec.Admin.Email != "admin@inmeta.local" {
		t.Errorf("provision-spec = %+v", prov.spec)
	}

	// Zitadel-org-id ble persistert på tenanten.
	if tenant.ZitadelOrgID != "org-x" {
		t.Errorf("zitadel_org_id = %q, vil ha org-x", tenant.ZitadelOrgID)
	}

	tx := pool.tx
	if tx == nil || !tx.committed {
		t.Fatal("tx ble ikke commitet")
	}
	if tx.tenantArgs == nil || tx.outboxArgs == nil {
		t.Fatal("tenant- og outbox-rad må skrives i samme tx")
	}

	// Tenant-raden bærer den genererte id-en.
	tenantID := tx.tenantArgs[0].(uuid.UUID)
	if tenantID != tenant.ID {
		t.Errorf("insertet id %s != returnert id %s", tenantID, tenant.ID)
	}

	// Outbox-eventet: riktig subject + tenant_id + payload (SPEC §9).
	var env event.Envelope
	if err := json.Unmarshal(tx.outboxArgs[3].([]byte), &env); err != nil {
		t.Fatalf("outbox-payload er ikke et envelope: %v", err)
	}
	if env.Type != "tl.platform.tenant.provisioned" {
		t.Errorf("event-type = %q, vil ha tl.platform.tenant.provisioned", env.Type)
	}
	if env.TenantID != tenant.ID {
		t.Errorf("event tenant_id = %s, vil ha %s", env.TenantID, tenant.ID)
	}
	var data tenantProvisionedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("event-data: %v", err)
	}
	if data.ZitadelOrgID != "org-x" || data.Slug != "inmeta" {
		t.Errorf("event-data = %+v", data)
	}
}

func TestCreateTenantRejectsDuplicateSlugBeforeProvisioning(t *testing.T) {
	pool := &fakePool{slugExists: true}
	prov := &fakeProvisioner{}
	svc := NewService(pool, NewRepo(pool), prov, nil)

	_, err := svc.CreateTenant(context.Background(), validInput())
	if !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("err = %v, vil ha ErrSlugTaken", err)
	}
	if prov.calls != 0 {
		t.Error("provisjonering skal ikke skje når slugen er tatt")
	}
	if pool.beginCalls != 0 {
		t.Error("ingen tx skal åpnes når slugen er tatt")
	}
}

func TestCreateTenantValidatesInput(t *testing.T) {
	pool := &fakePool{}
	prov := &fakeProvisioner{}
	svc := NewService(pool, NewRepo(pool), prov, nil)

	cases := map[string]CreateTenantInput{
		"tom slug":            func() CreateTenantInput { in := validInput(); in.Slug = ""; return in }(),
		"ugyldig slug":        func() CreateTenantInput { in := validInput(); in.Slug = "Inmeta Games"; return in }(),
		"tomt navn":           func() CreateTenantInput { in := validInput(); in.Name = ""; return in }(),
		"mangler admin-epost": func() CreateTenantInput { in := validInput(); in.Admin.Email = ""; return in }(),
	}
	for name, in := range cases {
		if _, err := svc.CreateTenant(context.Background(), in); err == nil {
			t.Errorf("%s: forventet valideringsfeil", name)
		}
	}
	if prov.calls != 0 {
		t.Error("provisjonering skal ikke skje ved valideringsfeil")
	}
}

func TestCreateTenantProvisioningFailureAbortsBeforeTx(t *testing.T) {
	pool := &fakePool{}
	prov := &fakeProvisioner{failErr: errors.New("zitadel nede")}
	svc := NewService(pool, NewRepo(pool), prov, nil)

	if _, err := svc.CreateTenant(context.Background(), validInput()); err == nil {
		t.Fatal("forventet feil når provisjonering feiler")
	}
	if pool.beginCalls != 0 {
		t.Error("ingen tx skal åpnes når provisjonering feiler")
	}
}
