package main

import (
	"context"
	"fmt"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

// Zitadel-provisjonering (SPEC §5): ett project `tronderleikan` eid av
// plattform-orgen, én project-grant per tenant-org, første org-admin opprettes
// via Zitadel API. tenant.zitadel_org_id lagres på tenanten.
//
// Directory + beslutningslogikken (sjekk-før-opprett) her er en bevisst KOPI av
// det tilsvarende mønsteret i zitadel-seed. Den er IKKE trukket ut i pkg/ fordi
// pkg importeres av alle tjenester (roster, competition, ...) og bare platform
// snakker med Zitadel - å legge zitadel-go/v3 (stor gRPC-avhengighet) i den
// delte lib-en ville påført dem alle den avhengigheten. Kopi holder pkg tynn.
// Kontrakten (rolle-nøkler) deles fortsatt via authn-konstantene.

// grantableRoles er rollene en tenant-org får tildele via project-grant.
// platform_admin er bevisst utelatt (SPEC §6: "Kun tildelbar i plattform-orgen").
var grantableRoles = []string{
	authn.RolePlayer,
	authn.RoleOrganizer,
	authn.RoleTenantAdmin,
}

// AdminSpec er den første org-admin-en som opprettes sammen med tenant-orgen.
type AdminSpec struct {
	Email      string
	GivenName  string
	FamilyName string
	Password   string
}

// TenantSpec er provisjonerings-inputen for én tenant.
type TenantSpec struct {
	OrgName string // = tenant-navnet; blir Zitadel Organization-navnet
	Admin   AdminSpec
}

// ProvisionResult er utfallet av en provisjonering.
type ProvisionResult struct {
	ZitadelOrgID   string
	ProjectGrantID string
	AdminUserID    string
}

// Directory er det platform trenger fra Zitadel, i vanlige Go-typer. Alle
// Find*/Create*/Ensure*-metodene er idempotente byggeklosser (som i seeden):
// Find* endrer ingenting, Create* oppretter, Ensure* tåler at ressursen finnes.
// zitadelDirectory implementerer dette mot zitadel-go; provisionsForTest bruker
// en fake, så beslutningslogikken testes uten en levende Zitadel.
type Directory interface {
	FindOrgByName(ctx context.Context, name string) (id string, found bool, err error)
	CreateOrg(ctx context.Context, name string) (id string, err error)

	FindProjectByName(ctx context.Context, orgID, name string) (id string, found bool, err error)

	FindProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string) (grantID string, currentRoles []string, found bool, err error)
	CreateProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (grantID string, err error)
	UpdateProjectGrant(ctx context.Context, ownerOrgID, projectID, grantID string, roleKeys []string) error

	FindUserByEmail(ctx context.Context, orgID, email string) (id string, found bool, err error)
	CreateUser(ctx context.Context, orgID string, admin AdminSpec, password string) (id string, err error)
	EnsureUserGrant(ctx context.Context, orgID, userID, projectID, projectGrantID string, roleKeys []string) error
}

// Provisioner utfører idempotent tenant-provisjonering mot en Directory.
// platformOrgName + projectName identifiserer den seedede grunntilstanden
// (zitadel-seed, pakke 0.4) som platform bygger tenants oppå.
type Provisioner struct {
	dir             Directory
	platformOrgName string
	projectName     string
	log             func(format string, args ...any)
}

// NewProvisioner lager en Provisioner. log kan være nil (da logges ingenting).
func NewProvisioner(dir Directory, platformOrgName, projectName string, log func(format string, args ...any)) *Provisioner {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Provisioner{dir: dir, platformOrgName: platformOrgName, projectName: projectName, log: log}
}

// platformProject slår opp plattform-org-id og project-id fra den seedede
// grunntilstanden. Feiler tydelig hvis seeden ikke er kjørt (SPEC §5: project
// og roller defineres én gang av seeden, platform forutsetter dem).
func (p *Provisioner) platformProject(ctx context.Context) (platformOrgID, projectID string, err error) {
	platformOrgID, found, err := p.dir.FindOrgByName(ctx, p.platformOrgName)
	if err != nil {
		return "", "", fmt.Errorf("finn plattform-org %q: %w", p.platformOrgName, err)
	}
	if !found {
		return "", "", fmt.Errorf("plattform-org %q finnes ikke - er zitadel-seed kjørt?", p.platformOrgName)
	}
	projectID, found, err = p.dir.FindProjectByName(ctx, platformOrgID, p.projectName)
	if err != nil {
		return "", "", fmt.Errorf("finn project %q: %w", p.projectName, err)
	}
	if !found {
		return "", "", fmt.Errorf("project %q finnes ikke i plattform-org - er zitadel-seed kjørt?", p.projectName)
	}
	return platformOrgID, projectID, nil
}

// ProjectAudience returnerer project-id-en, som JWT-audience for authn-validering
// (Zitadel legger project-roles i tokenet når project-id er i audience).
// Utledes fra Zitadel i stedet for en egen env-var, så det ikke finnes en
// hardkodet/driftende project-id noe sted.
func (p *Provisioner) ProjectAudience(ctx context.Context) (string, error) {
	_, projectID, err := p.platformProject(ctx)
	return projectID, err
}

// Provision sikrer idempotent at tenant-orgen finnes med project-grant og en
// første org-admin (SPEC §5). To kall med samme spec gir samme sluttilstand.
func (p *Provisioner) Provision(ctx context.Context, spec TenantSpec) (ProvisionResult, error) {
	var res ProvisionResult

	platformOrgID, projectID, err := p.platformProject(ctx)
	if err != nil {
		return res, err
	}

	// 1. Tenant-org (find-or-create). 1:1 med tenanten (SPEC §5).
	tenantOrgID, err := p.ensureOrg(ctx, spec.OrgName)
	if err != nil {
		return res, fmt.Errorf("tenant-org: %w", err)
	}
	res.ZitadelOrgID = tenantOrgID

	// 2. Project-grant til tenant-orgen, med konvergering av rollesettet.
	grantID, err := p.ensureProjectGrant(ctx, platformOrgID, projectID, tenantOrgID, grantableRoles)
	if err != nil {
		return res, fmt.Errorf("project-grant: %w", err)
	}
	res.ProjectGrantID = grantID

	// 3. Første org-admin (find-or-create) + tenant_admin-tildeling.
	adminID, err := p.ensureUser(ctx, tenantOrgID, spec.Admin)
	if err != nil {
		return res, fmt.Errorf("org-admin: %w", err)
	}
	res.AdminUserID = adminID
	if err := p.dir.EnsureUserGrant(ctx, tenantOrgID, adminID, projectID, grantID, []string{authn.RoleTenantAdmin}); err != nil {
		return res, fmt.Errorf("admin-grant: %w", err)
	}
	p.log("provisioned tenant org=%s grant=%s admin=%s", tenantOrgID, grantID, adminID)
	return res, nil
}

func (p *Provisioner) ensureOrg(ctx context.Context, name string) (string, error) {
	id, found, err := p.dir.FindOrgByName(ctx, name)
	if err != nil {
		return "", err
	}
	if found {
		p.log("org exists: %s (%s)", name, id)
		return id, nil
	}
	id, err = p.dir.CreateOrg(ctx, name)
	if err != nil {
		return "", err
	}
	p.log("org created: %s (%s)", name, id)
	return id, nil
}

func (p *Provisioner) ensureProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (string, error) {
	id, currentRoles, found, err := p.dir.FindProjectGrant(ctx, ownerOrgID, projectID, grantedOrgID)
	if err != nil {
		return "", err
	}
	if found {
		if sameStringSet(currentRoles, roleKeys) {
			return id, nil
		}
		if err := p.dir.UpdateProjectGrant(ctx, ownerOrgID, projectID, id, roleKeys); err != nil {
			return "", err
		}
		p.log("project-grant roles converged: %s (%v -> %v)", id, currentRoles, roleKeys)
		return id, nil
	}
	id, err = p.dir.CreateProjectGrant(ctx, ownerOrgID, projectID, grantedOrgID, roleKeys)
	if err != nil {
		return "", err
	}
	p.log("project-grant created: %s", id)
	return id, nil
}

func (p *Provisioner) ensureUser(ctx context.Context, orgID string, admin AdminSpec) (string, error) {
	id, found, err := p.dir.FindUserByEmail(ctx, orgID, admin.Email)
	if err != nil {
		return "", err
	}
	if found {
		p.log("admin exists: %s (%s)", admin.Email, id)
		return id, nil
	}
	id, err = p.dir.CreateUser(ctx, orgID, admin, admin.Password)
	if err != nil {
		return "", err
	}
	p.log("admin created: %s (%s)", admin.Email, id)
	return id, nil
}

// sameStringSet sier om a og b har nøyaktig de samme elementene (rekkefølge/
// duplikater ignorert).
func sameStringSet(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	seen := make(map[string]struct{}, len(b))
	for _, v := range b {
		if _, ok := set[v]; !ok {
			return false
		}
		seen[v] = struct{}{}
	}
	return len(seen) == len(set)
}
