package main

import (
	"context"
	"fmt"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

// RoleDef er en project-role slik den defineres i Zitadel-prosjektet.
// Nøklene er kontrakt: authn.Validator leser NØYAKTIG disse ut av JWT-ens
// roles-claim (SPEC §6). Derfor er authn-konstantene eneste kilde til sannhet -
// endres en nøkkel, endres den ett sted og både seed og validering følger med.
type RoleDef struct {
	Key         string
	DisplayName string
	Group       string
}

// projectRoles er de 4 rollene fra SPEC §6, i stigende privilegie-rekkefølge.
var projectRoles = []RoleDef{
	{Key: authn.RolePlayer, DisplayName: "Player", Group: "tenant"},
	{Key: authn.RoleOrganizer, DisplayName: "Organizer", Group: "tenant"},
	{Key: authn.RoleTenantAdmin, DisplayName: "Tenant Admin", Group: "tenant"},
	{Key: authn.RolePlatformAdmin, DisplayName: "Platform Admin", Group: "platform"},
}

// tenantGrantableRoles er rollene en tenant-org får lov å tildele via
// project-grant. platform_admin er bevisst utelatt: den er "kun tildelbar i
// plattform-orgen" (SPEC §6).
var tenantGrantableRoles = []string{
	authn.RolePlayer,
	authn.RoleOrganizer,
	authn.RoleTenantAdmin,
}

// Directory er det seeden trenger fra en Zitadel-instans, uttrykt i vanlige
// Go-typer. Alle Find*/Ensure*-metodene er idempotente byggeklosser:
//   - Find* returnerer (id, found, err) uten å endre noe.
//   - Create* oppretter og returnerer ny id.
//   - Ensure* er selv idempotente (tåler at ressursen finnes fra før).
//
// Adapteren mot zitadel-go implementerer dette (se directory_zitadel.go);
// enhetstestene bruker en fake (se seed_test.go). Beslutningslogikken
// (sjekk-før-opprett) ligger i Seeder, ikke i adapteren - den er dermed
// testbar uten en levende Zitadel.
type Directory interface {
	FindOrgByName(ctx context.Context, name string) (id string, found bool, err error)
	CreateOrg(ctx context.Context, name string) (id string, err error)

	FindProjectByName(ctx context.Context, orgID, name string) (id string, found bool, err error)
	CreateProject(ctx context.Context, orgID, name string) (id string, err error)

	// EnsureProjectRole er idempotent: tåler at rollen finnes fra før.
	EnsureProjectRole(ctx context.Context, orgID, projectID string, role RoleDef) error

	FindProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string) (grantID string, found bool, err error)
	CreateProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (grantID string, err error)

	FindUserByEmail(ctx context.Context, orgID, email string) (id string, found bool, err error)
	CreateUser(ctx context.Context, orgID string, user UserSpec, password string) (id string, err error)

	// EnsureUserGrant er idempotent: tåler at grantet finnes fra før.
	// projectGrantID er tom for prosjektets eier-org (plattform), satt for
	// tenant-orgen (granted project).
	EnsureUserGrant(ctx context.Context, orgID, userID, projectID, projectGrantID string, roleKeys []string) error
}

// Seeder kjører den idempotente provisjoneringen mot en Directory.
type Seeder struct {
	dir Directory
	log func(format string, args ...any)
}

// NewSeeder lager en Seeder. log kan være nil (da logges ingenting).
func NewSeeder(dir Directory, log func(format string, args ...any)) *Seeder {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Seeder{dir: dir, log: log}
}

// Result oppsummerer sluttilstanden (nyttig for logging og tester).
type Result struct {
	PlatformOrgID  string
	ProjectID      string
	TenantOrgID    string
	ProjectGrantID string
	UserIDs        map[string]string // e-post -> Zitadel user-id
}

// Seed sikrer hele mål-tilstanden idempotent (SPEC §5, §6, §12):
// plattform-org, project, 4 roller, test-tenant-org m/project-grant, testbrukere
// med rolletildelinger. To kjøringer på rad gir samme sluttilstand.
func (s *Seeder) Seed(ctx context.Context, cfg Config) (Result, error) {
	res := Result{UserIDs: map[string]string{}}

	// 1. Plattform-org (eier prosjektet).
	platformOrgID, err := s.ensureOrg(ctx, cfg.PlatformOrgName)
	if err != nil {
		return res, fmt.Errorf("plattform-org: %w", err)
	}
	res.PlatformOrgID = platformOrgID

	// 2. Project i plattform-org.
	projectID, err := s.ensureProject(ctx, platformOrgID, cfg.ProjectName)
	if err != nil {
		return res, fmt.Errorf("project: %w", err)
	}
	res.ProjectID = projectID

	// 3. De 4 project-rollene (SPEC §6).
	for _, role := range projectRoles {
		if err := s.dir.EnsureProjectRole(ctx, platformOrgID, projectID, role); err != nil {
			return res, fmt.Errorf("role %q: %w", role.Key, err)
		}
		s.log("role ensured: %s", role.Key)
	}

	// 4. Test-tenant-org.
	tenantOrgID, err := s.ensureOrg(ctx, cfg.TenantOrgName)
	if err != nil {
		return res, fmt.Errorf("tenant-org: %w", err)
	}
	res.TenantOrgID = tenantOrgID

	// 5. Project-grant til tenant-orgen (beviser grant-modellen).
	grantID, err := s.ensureProjectGrant(ctx, platformOrgID, projectID, tenantOrgID, tenantGrantableRoles)
	if err != nil {
		return res, fmt.Errorf("project-grant: %w", err)
	}
	res.ProjectGrantID = grantID

	// 6. Testbrukere med rolletildelinger.
	for _, u := range cfg.Users {
		orgID := platformOrgID
		projectGrantID := "" // eier-org: ingen grant-id
		if u.InTenant {
			orgID = tenantOrgID
			projectGrantID = grantID
		}
		userID, err := s.ensureUser(ctx, orgID, u, cfg.TestPassword)
		if err != nil {
			return res, fmt.Errorf("user %s: %w", u.Email, err)
		}
		res.UserIDs[u.Email] = userID
		if err := s.dir.EnsureUserGrant(ctx, orgID, userID, projectID, projectGrantID, u.Roles); err != nil {
			return res, fmt.Errorf("user-grant %s: %w", u.Email, err)
		}
		s.log("user ensured: %s (%v)", u.Email, u.Roles)
	}

	return res, nil
}

func (s *Seeder) ensureOrg(ctx context.Context, name string) (string, error) {
	id, found, err := s.dir.FindOrgByName(ctx, name)
	if err != nil {
		return "", err
	}
	if found {
		s.log("org exists: %s (%s)", name, id)
		return id, nil
	}
	id, err = s.dir.CreateOrg(ctx, name)
	if err != nil {
		return "", err
	}
	s.log("org created: %s (%s)", name, id)
	return id, nil
}

func (s *Seeder) ensureProject(ctx context.Context, orgID, name string) (string, error) {
	id, found, err := s.dir.FindProjectByName(ctx, orgID, name)
	if err != nil {
		return "", err
	}
	if found {
		s.log("project exists: %s (%s)", name, id)
		return id, nil
	}
	id, err = s.dir.CreateProject(ctx, orgID, name)
	if err != nil {
		return "", err
	}
	s.log("project created: %s (%s)", name, id)
	return id, nil
}

func (s *Seeder) ensureProjectGrant(ctx context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (string, error) {
	id, found, err := s.dir.FindProjectGrant(ctx, ownerOrgID, projectID, grantedOrgID)
	if err != nil {
		return "", err
	}
	if found {
		s.log("project-grant exists: %s -> %s (%s)", projectID, grantedOrgID, id)
		return id, nil
	}
	id, err = s.dir.CreateProjectGrant(ctx, ownerOrgID, projectID, grantedOrgID, roleKeys)
	if err != nil {
		return "", err
	}
	s.log("project-grant created: %s -> %s (%s)", projectID, grantedOrgID, id)
	return id, nil
}

func (s *Seeder) ensureUser(ctx context.Context, orgID string, u UserSpec, password string) (string, error) {
	id, found, err := s.dir.FindUserByEmail(ctx, orgID, u.Email)
	if err != nil {
		return "", err
	}
	if found {
		s.log("user exists: %s (%s)", u.Email, id)
		return id, nil
	}
	id, err = s.dir.CreateUser(ctx, orgID, u, password)
	if err != nil {
		return "", err
	}
	s.log("user created: %s (%s)", u.Email, id)
	return id, nil
}
