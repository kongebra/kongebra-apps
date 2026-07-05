//go:build e2e

// E2E-integrasjonstest for platform-tjenesten. Kjøres KUN med -tags e2e mot
// ekte infra (Postgres + NATS JetStream + seedet Zitadel), aldri i vanlig CI.
//
// Krever env: DATABASE_URL, NATS_URL, ZITADEL_API_URL, ZITADEL_PAT/PAT_FILE.
// Se docs handoff / scratchpad-compose. Testen provisjonerer en ekte tenant og
// verifiserer: Zitadel-org + grant + admin opprettet, tenant-rad i DB,
// tl.platform.tenant.provisioned publisert på NATS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/outbox"
)

func TestE2EProvisionTenant(t *testing.T) {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		t.Skipf("mangler e2e-konfig (%v) - kjør med DATABASE_URL/NATS_URL/ZITADEL_* satt", err)
	}
	ctx := context.Background()

	if err := runMigrations(ctx, cfg.DatabaseURL); err != nil {
		t.Fatalf("migrasjoner: %v", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	defer nc.Drain()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: streamName, Subjects: []string{streamSubjects}}); err != nil {
		t.Fatalf("ensure stream: %v", err)
	}

	dir, err := newZitadelDirectory(ctx, cfg.ZitadelTarget, cfg.ZitadelToken)
	if err != nil {
		t.Fatalf("zitadel: %v", err)
	}
	defer dir.Close()
	prov := NewProvisioner(dir, cfg.PlatformOrgName, cfg.ProjectName, t.Logf)
	repo := NewRepo(pool)
	svc := NewService(pool, repo, prov, t.Logf)

	// Subscribe FØR publisering (core-sub fanger JetStream-publiserte meldinger).
	sub, err := nc.SubscribeSync(event.Subject(eventService, eventEntity, eventProvisioned))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	suffix := time.Now().UTC().Format("20060102-150405")
	slug := "e2e-" + suffix
	adminEmail := fmt.Sprintf("admin+%s@e2e.local", suffix)
	in := CreateTenantInput{
		Name:             "E2E Tenant " + suffix,
		Slug:             slug,
		PublicVisibility: true,
		Admin:            AdminSpec{Email: adminEmail, GivenName: "E2E", FamilyName: "Admin", Password: "Password1!"},
	}

	tenant, err := svc.CreateTenant(ctx, in)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	t.Logf("created tenant id=%s zitadel_org=%s", tenant.ID, tenant.ZitadelOrgID)
	if tenant.ZitadelOrgID == "" {
		t.Fatal("tenant mangler zitadel_org_id")
	}

	// Publiser outbox -> NATS.
	if _, err := outbox.NewPublisher(pool, js).PublishPending(ctx); err != nil {
		t.Fatalf("publish: %v", err)
	}
	msg, err := sub.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("ventet event på NATS: %v", err)
	}
	var env event.Envelope
	if err := json.Unmarshal(msg.Data, &env); err != nil {
		t.Fatalf("event unmarshal: %v", err)
	}
	if env.Type != "tl.platform.tenant.provisioned" || env.TenantID != tenant.ID {
		t.Fatalf("uventet event: type=%s tenant=%s", env.Type, env.TenantID)
	}
	var data tenantProvisionedData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("event-data: %v", err)
	}
	if data.ZitadelOrgID != tenant.ZitadelOrgID || data.Slug != slug {
		t.Fatalf("event-data mismatch: %+v", data)
	}
	t.Logf("event mottatt på NATS: %s tenant=%s org=%s", env.Type, env.TenantID, data.ZitadelOrgID)

	// DB-rad.
	got, err := repo.GetBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ZitadelOrgID != tenant.ZitadelOrgID {
		t.Fatalf("DB zitadel_org_id = %s, vil ha %s", got.ZitadelOrgID, tenant.ZitadelOrgID)
	}

	// Zitadel-tilstand: org, grant og admin.
	platformOrgID, _, err := prov.platformProject(ctx)
	if err != nil {
		t.Fatalf("platformProject: %v", err)
	}
	_, projectID, _ := prov.platformProject(ctx)
	orgID, found, err := dir.FindOrgByName(ctx, in.Name)
	if err != nil || !found || orgID != tenant.ZitadelOrgID {
		t.Fatalf("Zitadel-org: found=%v id=%s err=%v", found, orgID, err)
	}
	grantID, roles, found, err := dir.FindProjectGrant(ctx, platformOrgID, projectID, orgID)
	if err != nil || !found {
		t.Fatalf("project-grant mangler: found=%v err=%v", found, err)
	}
	if !sameStringSet(roles, grantableRoles) {
		t.Fatalf("grant-roller = %v, vil ha %v", roles, grantableRoles)
	}
	t.Logf("Zitadel project-grant ok: %s roller=%v", grantID, roles)
	userID, found, err := dir.FindUserByEmail(ctx, orgID, adminEmail)
	if err != nil || !found {
		t.Fatalf("org-admin mangler: found=%v err=%v", found, err)
	}
	t.Logf("Zitadel org-admin ok: %s", userID)

	// Idempotens: ny provisjonering av samme org gir samme org-id.
	res2, err := prov.Provision(ctx, TenantSpec{OrgName: in.Name, Admin: in.Admin})
	if err != nil {
		t.Fatalf("re-provision: %v", err)
	}
	if res2.ZitadelOrgID != tenant.ZitadelOrgID {
		t.Fatalf("idempotens brutt: org %s != %s", res2.ZitadelOrgID, tenant.ZitadelOrgID)
	}
	t.Logf("idempotens ok: re-provision ga samme org %s", res2.ZitadelOrgID)
}
