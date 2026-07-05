package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/outbox"
)

// Event-koordinater for tenant.provisioned (SPEC §9 event-katalog):
// tl.platform.tenant.provisioned.
const (
	eventService     = "platform"
	eventEntity      = "tenant"
	eventProvisioned = "provisioned"
)

// ErrSlugTaken returneres når slugen allerede er i bruk (mappes til 409).
var ErrSlugTaken = errors.New("slug er allerede i bruk")

// ErrInvalidAdmin returneres når admin-feltene mangler (mappes til 400).
var ErrInvalidAdmin = errors.New("admin krever email, given_name, family_name og password")

// tenantProvisioner er Provisioner-delen Service trenger (fakes i test).
type tenantProvisioner interface {
	Provision(ctx context.Context, spec TenantSpec) (ProvisionResult, error)
}

// CreateTenantInput er inputen til CreateTenant (validert her, ikke i handleren).
type CreateTenantInput struct {
	Name             string
	Slug             string
	PublicVisibility bool
	Admin            AdminSpec
}

// Service er admin-plane-forretningslogikken: tenant-registry + provisjonering.
type Service struct {
	db   DB
	repo *Repo
	prov tenantProvisioner
	log  func(format string, args ...any)
}

// NewService lager en Service. log kan være nil.
func NewService(db DB, repo *Repo, prov tenantProvisioner, log func(format string, args ...any)) *Service {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Service{db: db, repo: repo, prov: prov, log: log}
}

// tenantProvisionedData er payloaden i tl.platform.tenant.provisioned-eventet.
type tenantProvisionedData struct {
	TenantID         uuid.UUID `json:"tenant_id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	ZitadelOrgID     string    `json:"zitadel_org_id"`
	PublicVisibility bool      `json:"public_visibility"`
}

// CreateTenant provisjonerer en Zitadel-org (+ grant + første admin) og skriver
// tenant-raden og tl.platform.tenant.provisioned-eventet i SAMME transaksjon
// (SPEC §9 transactional outbox).
//
// ponytail: Zitadel-provisjonering skjer FØR DB-tx-en og er ikke transaksjonell
// med den. Provision er idempotent (find-or-create), så en retry etter en
// DB-feil gjenbruker orgen i stedet for å lage en ny. Ved samtidige kall med
// samme slug vinner én på UNIQUE-constraintet; taperen etterlater en foreldreløs
// Zitadel-org. Akseptabelt i v1 (manuell admin-handling, lav samtidighet).
// Oppgraderingssti: reserver slug-raden først (status=pending) i egen tx, så
// provisjonér, så fullfør - eller en saga med kompenserende org-sletting.
func (s *Service) CreateTenant(ctx context.Context, in CreateTenantInput) (Tenant, error) {
	if err := ValidateName(in.Name); err != nil {
		return Tenant{}, err
	}
	if err := ValidateSlug(in.Slug); err != nil {
		return Tenant{}, err
	}
	if err := validateAdmin(in.Admin); err != nil {
		return Tenant{}, err
	}

	exists, err := s.repo.ExistsBySlug(ctx, in.Slug)
	if err != nil {
		return Tenant{}, err
	}
	if exists {
		return Tenant{}, ErrSlugTaken
	}

	res, err := s.prov.Provision(ctx, TenantSpec{OrgName: in.Name, Admin: in.Admin})
	if err != nil {
		return Tenant{}, fmt.Errorf("provisjonering: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Tenant{}, fmt.Errorf("generer tenant-id: %w", err)
	}
	now := time.Now().UTC()
	tenant := Tenant{
		ID:               id,
		Name:             in.Name,
		Slug:             in.Slug,
		ZitadelOrgID:     res.ZitadelOrgID,
		PublicVisibility: in.PublicVisibility,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	env, err := event.New(tenant.ID, event.Subject(eventService, eventEntity, eventProvisioned), tenantProvisionedData{
		TenantID:         tenant.ID,
		Name:             tenant.Name,
		Slug:             tenant.Slug,
		ZitadelOrgID:     tenant.ZitadelOrgID,
		PublicVisibility: tenant.PublicVisibility,
	})
	if err != nil {
		return Tenant{}, fmt.Errorf("bygg event: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Tenant{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := InsertTx(ctx, tx, tenant); err != nil {
		return Tenant{}, err
	}
	if err := outbox.Write(ctx, tx, env); err != nil {
		return Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Tenant{}, fmt.Errorf("commit tx: %w", err)
	}

	s.log("tenant created: %s (slug=%s org=%s)", tenant.ID, tenant.Slug, tenant.ZitadelOrgID)
	return tenant, nil
}

func validateAdmin(a AdminSpec) error {
	if a.Email == "" || a.GivenName == "" || a.FamilyName == "" || a.Password == "" {
		return ErrInvalidAdmin
	}
	return nil
}
