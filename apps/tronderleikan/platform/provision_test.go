package main

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

const (
	testPlatformOrg = "TronderLeikan Platform"
	testProject     = "tronderleikan"
)

// fakeDirectory er en streng in-memory Directory: Create*-metodene FEILER hvis
// ressursen finnes fra før, så en Provisioner som ikke sjekker-før-oppretter
// vil feile på kjøring nr. 2. Idempotens blir da noe testen beviser.
type fakeDirectory struct {
	orgs         map[string]string   // name -> id
	projects     map[string]string   // orgID/name -> id
	grants       map[string]string   // owner/project/granted -> grantID
	grantRoles   map[string][]string // grantID -> roleKeys
	users        map[string]string   // orgID/email -> id
	userGrants   map[string][]string // orgID/userID/projectID/grantID -> roleKeys
	createdCount map[string]int
	seq          int
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{
		orgs: map[string]string{}, projects: map[string]string{}, grants: map[string]string{},
		grantRoles: map[string][]string{}, users: map[string]string{}, userGrants: map[string][]string{},
		createdCount: map[string]int{},
	}
}

// seeded returnerer en fakeDirectory med plattform-org + project på plass
// (som zitadel-seed ville lagt dem), klar for tenant-provisjonering.
func seeded() *fakeDirectory {
	f := newFakeDirectory()
	orgID := f.nextID("org")
	f.orgs[testPlatformOrg] = orgID
	f.projects[orgID+"/"+testProject] = f.nextID("project")
	return f
}

func (f *fakeDirectory) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func (f *fakeDirectory) FindOrgByName(_ context.Context, name string) (string, bool, error) {
	id, ok := f.orgs[name]
	return id, ok, nil
}

func (f *fakeDirectory) CreateOrg(_ context.Context, name string) (string, error) {
	if _, ok := f.orgs[name]; ok {
		return "", fmt.Errorf("IKKE-IDEMPOTENT: org %q opprettet to ganger", name)
	}
	id := f.nextID("org")
	f.orgs[name] = id
	f.createdCount["org"]++
	return id, nil
}

func (f *fakeDirectory) FindProjectByName(_ context.Context, orgID, name string) (string, bool, error) {
	id, ok := f.projects[orgID+"/"+name]
	return id, ok, nil
}

func (f *fakeDirectory) FindProjectGrant(_ context.Context, ownerOrgID, projectID, grantedOrgID string) (string, []string, bool, error) {
	id, ok := f.grants[ownerOrgID+"/"+projectID+"/"+grantedOrgID]
	if !ok {
		return "", nil, false, nil
	}
	return id, f.grantRoles[id], true, nil
}

func (f *fakeDirectory) CreateProjectGrant(_ context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (string, error) {
	key := ownerOrgID + "/" + projectID + "/" + grantedOrgID
	if _, ok := f.grants[key]; ok {
		return "", fmt.Errorf("IKKE-IDEMPOTENT: grant %q opprettet to ganger", key)
	}
	id := f.nextID("grant")
	f.grants[key] = id
	f.grantRoles[id] = roleKeys
	f.createdCount["grant"]++
	return id, nil
}

func (f *fakeDirectory) UpdateProjectGrant(_ context.Context, _, _, grantID string, roleKeys []string) error {
	f.grantRoles[grantID] = roleKeys
	f.createdCount["grantUpdate"]++
	return nil
}

func (f *fakeDirectory) FindUserByEmail(_ context.Context, orgID, email string) (string, bool, error) {
	id, ok := f.users[orgID+"/"+email]
	return id, ok, nil
}

func (f *fakeDirectory) CreateUser(_ context.Context, orgID string, admin AdminSpec, _ string) (string, error) {
	key := orgID + "/" + admin.Email
	if _, ok := f.users[key]; ok {
		return "", fmt.Errorf("IKKE-IDEMPOTENT: user %q opprettet to ganger", key)
	}
	id := f.nextID("user")
	f.users[key] = id
	f.createdCount["user"]++
	return id, nil
}

func (f *fakeDirectory) EnsureUserGrant(_ context.Context, orgID, userID, projectID, projectGrantID string, roleKeys []string) error {
	key := orgID + "/" + userID + "/" + projectID + "/" + projectGrantID
	if _, ok := f.userGrants[key]; !ok {
		f.createdCount["userGrant"]++
	}
	f.userGrants[key] = roleKeys
	return nil
}

func testSpec() TenantSpec {
	return TenantSpec{
		OrgName: "Inmeta Games",
		Admin: AdminSpec{
			Email:      "admin@inmeta.local",
			GivenName:  "In",
			FamilyName: "Meta",
			Password:   "Password1!",
		},
	}
}

func newTestProvisioner(dir Directory) *Provisioner {
	return NewProvisioner(dir, testPlatformOrg, testProject, nil)
}

func TestProvisionCreatesExpectedState(t *testing.T) {
	fake := seeded()
	res, err := newTestProvisioner(fake).Provision(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if res.ZitadelOrgID == "" || res.ProjectGrantID == "" || res.AdminUserID == "" {
		t.Fatalf("ufullstendig resultat: %+v", res)
	}
	if fake.createdCount["org"] != 1 {
		t.Errorf("vil opprette 1 tenant-org, fikk %d", fake.createdCount["org"])
	}
	if fake.createdCount["grant"] != 1 {
		t.Errorf("vil opprette 1 grant, fikk %d", fake.createdCount["grant"])
	}
	if fake.createdCount["user"] != 1 {
		t.Errorf("vil opprette 1 admin, fikk %d", fake.createdCount["user"])
	}
	if fake.createdCount["userGrant"] != 1 {
		t.Errorf("vil opprette 1 user-grant, fikk %d", fake.createdCount["userGrant"])
	}
	// Grant har de grantbare rollene (ikke platform_admin).
	if got := fake.grantRoles[res.ProjectGrantID]; !sameStringSet(got, grantableRoles) {
		t.Errorf("grant-roller = %v, vil ha %v", got, grantableRoles)
	}
	// Admin fikk tenant_admin.
	for _, roles := range fake.userGrants {
		if !slices.Equal(roles, []string{authn.RoleTenantAdmin}) {
			t.Errorf("admin-grant-roller = %v, vil ha [tenant_admin]", roles)
		}
	}
}

func TestProvisionIdempotent(t *testing.T) {
	fake := seeded()
	prov := newTestProvisioner(fake)
	ctx := context.Background()

	res1, err := prov.Provision(ctx, testSpec())
	if err != nil {
		t.Fatalf("kjøring 1: %v", err)
	}
	first := map[string]int{}
	for k, v := range fake.createdCount {
		first[k] = v
	}

	res2, err := prov.Provision(ctx, testSpec())
	if err != nil {
		t.Fatalf("kjøring 2 (idempotens brutt): %v", err)
	}
	for k, v := range fake.createdCount {
		if v != first[k] {
			t.Errorf("%s: create-count endret seg (%d -> %d) på kjøring 2", k, first[k], v)
		}
	}
	if res1 != res2 {
		t.Errorf("resultat endret seg mellom kjøringer: %+v vs %+v", res1, res2)
	}
}

func TestProvisionConvergesGrantRoles(t *testing.T) {
	fake := seeded()
	prov := newTestProvisioner(fake)
	ctx := context.Background()

	res, err := prov.Provision(ctx, testSpec())
	if err != nil {
		t.Fatalf("første provision: %v", err)
	}
	// Korrupt tilstand: grantet mangler roller.
	fake.grantRoles[res.ProjectGrantID] = []string{authn.RolePlayer}

	if _, err := prov.Provision(ctx, testSpec()); err != nil {
		t.Fatalf("andre provision: %v", err)
	}
	if got := fake.grantRoles[res.ProjectGrantID]; !sameStringSet(got, grantableRoles) {
		t.Errorf("grant-roller konvergerte ikke: fikk %v", got)
	}
	if fake.createdCount["grantUpdate"] != 1 {
		t.Errorf("vil ha 1 grant-update, fikk %d", fake.createdCount["grantUpdate"])
	}
}

func TestProvisionFailsWithoutSeed(t *testing.T) {
	// Tom directory: verken plattform-org eller project finnes.
	fake := newFakeDirectory()
	if _, err := newTestProvisioner(fake).Provision(context.Background(), testSpec()); err == nil {
		t.Fatal("Provision skal feile når zitadel-seed ikke er kjørt")
	}
}

func TestProjectAudienceReturnsProjectID(t *testing.T) {
	fake := seeded()
	aud, err := newTestProvisioner(fake).ProjectAudience(context.Background())
	if err != nil {
		t.Fatalf("ProjectAudience: %v", err)
	}
	platformOrgID := fake.orgs[testPlatformOrg]
	want := fake.projects[platformOrgID+"/"+testProject]
	if aud != want {
		t.Errorf("audience = %q, vil ha project-id %q", aud, want)
	}
}

func TestGrantableRolesExcludePlatformAdmin(t *testing.T) {
	if slices.Contains(grantableRoles, authn.RolePlatformAdmin) {
		t.Fatal("platform_admin skal ALDRI være grantbar til en tenant (SPEC §6)")
	}
	for _, r := range []string{authn.RolePlayer, authn.RoleOrganizer, authn.RoleTenantAdmin} {
		if !slices.Contains(grantableRoles, r) {
			t.Errorf("%s mangler i grantableRoles", r)
		}
	}
}
