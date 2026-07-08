package main

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

// fakeDirectory er en streng in-memory Directory. Create*-metodene FEILER hvis
// ressursen finnes fra før - så en Seeder som ikke sjekker-før-oppretter vil
// feile på kjøring nr. 2. Det gjør idempotens til noe testen faktisk beviser,
// ikke bare antar.
type fakeDirectory struct {
	orgs         map[string]string   // name -> id
	projects     map[string]string   // orgID/name -> id
	roles        map[string]bool     // orgID/projectID/roleKey
	grants       map[string]string   // ownerOrg/project/grantedOrg -> grantID
	users        map[string]string   // orgID/email -> id
	userGrants   map[string]bool     // orgID/userID/projectID/grantID
	grantRoles   map[string][]string // grantID -> roleKeys (for assertions)
	oidcApps     map[string]string   // orgID/projectID/name -> appID
	oidcClientID map[string]string   // appID -> clientID
	oidcRedirects map[string][]string // appID -> redirect URIs
	createdCount map[string]int      // teller Create*-kall per type
	seq          int
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{
		orgs: map[string]string{}, projects: map[string]string{}, roles: map[string]bool{},
		grants: map[string]string{}, users: map[string]string{}, userGrants: map[string]bool{},
		grantRoles: map[string][]string{}, oidcApps: map[string]string{}, oidcClientID: map[string]string{}, oidcRedirects: map[string][]string{},
		createdCount: map[string]int{},
	}
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

func (f *fakeDirectory) CreateProject(_ context.Context, orgID, name string) (string, error) {
	key := orgID + "/" + name
	if _, ok := f.projects[key]; ok {
		return "", fmt.Errorf("IKKE-IDEMPOTENT: project %q opprettet to ganger", key)
	}
	id := f.nextID("project")
	f.projects[key] = id
	f.createdCount["project"]++
	return id, nil
}

func (f *fakeDirectory) EnsureProjectRole(_ context.Context, orgID, projectID string, role RoleDef) error {
	key := orgID + "/" + projectID + "/" + role.Key
	if !f.roles[key] {
		f.createdCount["role"]++
	}
	f.roles[key] = true
	return nil
}

func (f *fakeDirectory) FindProjectGrant(_ context.Context, ownerOrgID, projectID, grantedOrgID string) (string, []string, bool, error) {
	id, ok := f.grants[ownerOrgID+"/"+projectID+"/"+grantedOrgID]
	if !ok {
		return "", nil, false, nil
	}
	return id, f.grantRoles[id], true, nil
}

func (f *fakeDirectory) UpdateProjectGrant(_ context.Context, _, _, grantID string, roleKeys []string) error {
	f.grantRoles[grantID] = roleKeys
	f.createdCount["grantUpdate"]++
	return nil
}

func (f *fakeDirectory) CreateProjectGrant(_ context.Context, ownerOrgID, projectID, grantedOrgID string, roleKeys []string) (string, error) {
	key := ownerOrgID + "/" + projectID + "/" + grantedOrgID
	if _, ok := f.grants[key]; ok {
		return "", fmt.Errorf("IKKE-IDEMPOTENT: project-grant %q opprettet to ganger", key)
	}
	id := f.nextID("grant")
	f.grants[key] = id
	f.grantRoles[id] = roleKeys
	f.createdCount["grant"]++
	return id, nil
}

func (f *fakeDirectory) FindUserByEmail(_ context.Context, orgID, email string) (string, bool, error) {
	id, ok := f.users[orgID+"/"+email]
	return id, ok, nil
}

func (f *fakeDirectory) CreateUser(_ context.Context, orgID string, u UserSpec, _ string) (string, error) {
	key := orgID + "/" + u.Email
	if _, ok := f.users[key]; ok {
		return "", fmt.Errorf("IKKE-IDEMPOTENT: user %q opprettet to ganger", key)
	}
	id := f.nextID("user")
	f.users[key] = id
	f.createdCount["user"]++
	return id, nil
}

func (f *fakeDirectory) EnsureUserGrant(_ context.Context, orgID, userID, projectID, projectGrantID string, _ []string) error {
	key := orgID + "/" + userID + "/" + projectID + "/" + projectGrantID
	if !f.userGrants[key] {
		f.createdCount["userGrant"]++
	}
	f.userGrants[key] = true
	return nil
}

func (f *fakeDirectory) FindOIDCApp(_ context.Context, orgID, projectID, name string) (string, string, []string, bool, error) {
	appID, ok := f.oidcApps[orgID+"/"+projectID+"/"+name]
	if !ok {
		return "", "", nil, false, nil
	}
	return appID, f.oidcClientID[appID], f.oidcRedirects[appID], true, nil
}

func (f *fakeDirectory) CreateOIDCApp(_ context.Context, orgID, projectID string, spec OIDCAppSpec) (string, string, error) {
	key := orgID + "/" + projectID + "/" + spec.Name
	if _, ok := f.oidcApps[key]; ok {
		return "", "", fmt.Errorf("NOT-IDEMPOTENT: OIDC app %q created twice", key)
	}
	appID := f.nextID("app")
	f.oidcApps[key] = appID
	f.oidcClientID[appID] = f.nextID("client") + "@tronderleikan"
	f.oidcRedirects[appID] = spec.RedirectURIs
	f.createdCount["oidcApp"]++
	return appID, f.oidcClientID[appID], nil
}

func (f *fakeDirectory) UpdateOIDCApp(_ context.Context, _, _, appID string, spec OIDCAppSpec) error {
	f.oidcRedirects[appID] = spec.RedirectURIs
	f.createdCount["oidcUpdate"]++
	return nil
}

func testConfig() Config {
	cfg, err := LoadConfig(envFrom(map[string]string{
		EnvAPIURL:       "http://localhost:8300",
		EnvPAT:          "tok",
		EnvTestPassword: "Password1!",
	}))
	if err != nil {
		panic(err)
	}
	return cfg
}

func TestSeedIdempotent(t *testing.T) {
	fake := newFakeDirectory()
	seeder := NewSeeder(fake, nil)
	cfg := testConfig()
	ctx := context.Background()

	res1, err := seeder.Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("kjøring 1: %v", err)
	}
	firstCreates := map[string]int{}
	for k, v := range fake.createdCount {
		firstCreates[k] = v
	}

	// Kjøring 2: fake.Create* feiler hvis noe opprettes på nytt.
	res2, err := seeder.Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("kjøring 2 (idempotens brutt): %v", err)
	}

	// Ingen nye Create*-kall på andre kjøring.
	for k, v := range fake.createdCount {
		if v != firstCreates[k] {
			t.Fatalf("%s: create-count endret seg (%d -> %d) på kjøring 2 - ikke idempotent", k, firstCreates[k], v)
		}
	}

	// Sluttilstanden er identisk.
	if res1.PlatformOrgID != res2.PlatformOrgID || res1.ProjectID != res2.ProjectID ||
		res1.TenantOrgID != res2.TenantOrgID || res1.ProjectGrantID != res2.ProjectGrantID {
		t.Fatalf("ID-er endret seg mellom kjøringer: %+v vs %+v", res1, res2)
	}
	for email, id := range res1.UserIDs {
		if res2.UserIDs[email] != id {
			t.Fatalf("user-id for %s endret seg: %s -> %s", email, id, res2.UserIDs[email])
		}
	}
}

func TestSeedCreatesExpectedState(t *testing.T) {
	fake := newFakeDirectory()
	cfg := testConfig()
	if _, err := NewSeeder(fake, nil).Seed(context.Background(), cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if len(fake.orgs) != 2 {
		t.Fatalf("vil ha 2 orgs (plattform + tenant), fikk %d", len(fake.orgs))
	}
	if fake.createdCount["project"] != 1 {
		t.Fatalf("vil ha 1 project, fikk %d", fake.createdCount["project"])
	}
	if fake.createdCount["role"] != 4 {
		t.Fatalf("vil ha 4 roller, fikk %d", fake.createdCount["role"])
	}
	if fake.createdCount["grant"] != 1 {
		t.Fatalf("vil ha 1 project-grant, fikk %d", fake.createdCount["grant"])
	}
	if fake.createdCount["user"] != 3 {
		t.Fatalf("vil ha 3 brukere, fikk %d", fake.createdCount["user"])
	}
	if fake.createdCount["userGrant"] != 3 {
		t.Fatalf("vil ha 3 user-grants, fikk %d", fake.createdCount["userGrant"])
	}
}

// Et eksisterende project-grant med feil/ufullstendig rollesett skal
// konvergere til ønsket sett - ikke bare aksepteres (Copilot-funn).
func TestSeedConvergesProjectGrantRoles(t *testing.T) {
	fake := newFakeDirectory()
	cfg := testConfig()
	ctx := context.Background()

	res, err := NewSeeder(fake, nil).Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("første seed: %v", err)
	}
	grantID := res.ProjectGrantID

	// Korrupt tilstand: grantet mangler roller.
	fake.grantRoles[grantID] = []string{authn.RolePlayer}

	if _, err := NewSeeder(fake, nil).Seed(ctx, cfg); err != nil {
		t.Fatalf("andre seed: %v", err)
	}

	if got := fake.grantRoles[grantID]; !sameStringSet(got, tenantGrantableRoles) {
		t.Fatalf("grant-roller konvergerte ikke: fikk %v, vil ha %v", got, tenantGrantableRoles)
	}
	if fake.createdCount["grantUpdate"] != 1 {
		t.Fatalf("vil ha nøyaktig 1 grant-update, fikk %d", fake.createdCount["grantUpdate"])
	}
	if fake.createdCount["grant"] != 1 {
		t.Fatalf("skal ikke opprette nytt grant ved konvergering, fikk %d", fake.createdCount["grant"])
	}
}

// sameStringSet: rekkefølge/duplikater skal ikke påvirke likhet.
func TestSameStringSet(t *testing.T) {
	if !sameStringSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Fatal("lik uansett rekkefølge")
	}
	if sameStringSet([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("delmengde er ikke lik")
	}
	if sameStringSet([]string{"a", "b"}, []string{"a"}) {
		t.Fatal("supersett er ikke lik")
	}
}

// Rollenøklene er kontrakt mot authn.Validator (SPEC §6). Fanger opp drift.
func TestProjectRolesMatchSpec(t *testing.T) {
	want := []string{authn.RolePlayer, authn.RoleOrganizer, authn.RoleTenantAdmin, authn.RolePlatformAdmin}
	got := make([]string, 0, len(projectRoles))
	for _, r := range projectRoles {
		got = append(got, r.Key)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		t.Fatalf("project-roller = %v, vil ha %v", got, want)
	}
}

// platform_admin skal ALDRI være grantbar til en tenant-org (SPEC §6:
// "Kun tildelbar i plattform-orgen").
func TestPlatformAdminNotGrantableToTenant(t *testing.T) {
	if slices.Contains(tenantGrantableRoles, authn.RolePlatformAdmin) {
		t.Fatal("platform_admin skal ikke være i tenantGrantableRoles")
	}
	for _, r := range []string{authn.RolePlayer, authn.RoleOrganizer, authn.RoleTenantAdmin} {
		if !slices.Contains(tenantGrantableRoles, r) {
			t.Fatalf("%s mangler i tenantGrantableRoles", r)
		}
	}
}

func TestSeedCreatesOIDCApps(t *testing.T) {
	fake := newFakeDirectory()
	cfg := testConfig()
	cfg.OIDCApps = []OIDCAppSpec{
		{Name: "tronderleikan-web", RedirectURIs: []string{"https://leikan.newb.no/auth/callback"}},
		{Name: "tronderleikan-admin", RedirectURIs: []string{"https://leikan-admin.newb.no/admin/auth/callback"}},
	}
	res, err := NewSeeder(fake, nil).Seed(context.Background(), cfg)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if fake.createdCount["oidcApp"] != 2 {
		t.Fatalf("want 2 OIDC apps created, got %d", fake.createdCount["oidcApp"])
	}
	if res.ClientIDs["tronderleikan-web"] == "" || res.ClientIDs["tronderleikan-admin"] == "" {
		t.Fatalf("client IDs not captured: %+v", res.ClientIDs)
	}
}

func TestSeedOIDCAppsIdempotentAndConverge(t *testing.T) {
	fake := newFakeDirectory()
	cfg := testConfig()
	cfg.OIDCApps = []OIDCAppSpec{
		{Name: "tronderleikan-web", RedirectURIs: []string{"https://leikan.newb.no/auth/callback", "https://leikan-dev.newb.no/auth/callback"}},
	}
	ctx := context.Background()

	res1, err := NewSeeder(fake, nil).Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	// Corrupt: drop one redirect URI so run 2 must converge.
	appID := fake.oidcApps["org-1/project-2/tronderleikan-web"]
	if appID == "" {
		// find it without hardcoding ids
		for k, v := range fake.oidcApps {
			if len(k) > 0 {
				appID = v
			}
		}
	}
	fake.oidcRedirects[appID] = []string{"https://leikan.newb.no/auth/callback"}

	res2, err := NewSeeder(fake, nil).Seed(ctx, cfg)
	if err != nil {
		t.Fatalf("run 2 (idempotency broken): %v", err)
	}
	if fake.createdCount["oidcApp"] != 1 {
		t.Fatalf("app re-created on run 2: count=%d", fake.createdCount["oidcApp"])
	}
	if fake.createdCount["oidcUpdate"] != 1 {
		t.Fatalf("want exactly 1 converge-update, got %d", fake.createdCount["oidcUpdate"])
	}
	if !sameStringSet(fake.oidcRedirects[appID], cfg.OIDCApps[0].RedirectURIs) {
		t.Fatalf("redirects did not converge: %v", fake.oidcRedirects[appID])
	}
	if res1.ClientIDs["tronderleikan-web"] != res2.ClientIDs["tronderleikan-web"] {
		t.Fatalf("client_id changed between runs")
	}
}
