package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/authn"
)

// Env-varer seeden konfigureres fra. Issuer/domenet leses ALLTID fra env
// (SPEC §5, ufravikelig) - aldri hardkodet i koden.
const (
	// EnvAPIURL er full URL til Zitadel-instansen, f.eks. http://localhost:8300
	// (lokal Aspire) eller https://auth.newb.no (cluster).
	EnvAPIURL = "ZITADEL_API_URL"
	// EnvPAT er en Personal Access Token for en machine-user med IAM_OWNER.
	EnvPAT = "ZITADEL_PAT"
	// EnvPATFile peker på en fil som inneholder PAT-en (Zitadel FirstInstance
	// skriver den hit ved init; lokal Aspire bind-mounter fila til seeden).
	EnvPATFile = "ZITADEL_PAT_FILE"

	// Seed-parametre (navn/e-post) er overstyrbare, men har fornuftige defaults.
	EnvPlatformOrg  = "SEED_PLATFORM_ORG_NAME"
	EnvTenantOrg    = "SEED_TENANT_ORG_NAME"
	EnvProjectName  = "SEED_PROJECT_NAME"
	EnvTestPassword = "SEED_TEST_PASSWORD"
)

// Defaults som gir mening lokalt. Ingen hemmeligheter her: testpassordet må
// settes eksplisitt via env (apphost setter en lokal dev-verdi).
const (
	defaultPlatformOrg = "TronderLeikan Platform"
	defaultTenantOrg   = "TronderLeikan Demo Tenant"
	defaultProjectName = "tronderleikan"
)

// Target er en Zitadel-instans dekomponert til det zitadel-go-klienten trenger.
type Target struct {
	Domain string // hostname uten skjema/port, f.eks. "localhost"
	Port   string // "8300", "443", ...
	TLS    bool   // false for http (lokal), true for https (cluster)
}

// UserSpec beskriver en testbruker som skal finnes etter seeding.
type UserSpec struct {
	Email      string
	GivenName  string
	FamilyName string
	// InTenant styrer hvilken org brukeren opprettes i: false = plattform-org,
	// true = test-tenant-org.
	InTenant bool
	// Roles er project-role-nøklene brukeren skal tildeles (SPEC §6).
	Roles []string
}

// Config er den ferdig-validerte konfigurasjonen seeden kjører på.
type Config struct {
	Target          Target
	Token           string
	PlatformOrgName string
	TenantOrgName   string
	ProjectName     string
	TestPassword    string
	Users           []UserSpec
}

// LoadConfig leser og validerer konfigurasjon fra miljøet.
func LoadConfig(getenv func(string) string) (Config, error) {
	rawURL := getenv(EnvAPIURL)
	if rawURL == "" {
		return Config{}, fmt.Errorf("env %s må være satt - issuer/domenet hardkodes aldri (SPEC §5)", EnvAPIURL)
	}
	target, err := parseTarget(rawURL)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvAPIURL, err)
	}

	token, err := resolveToken(getenv, os.ReadFile)
	if err != nil {
		return Config{}, err
	}

	password := getenv(EnvTestPassword)
	if password == "" {
		return Config{}, fmt.Errorf("env %s må være satt (testbruker-passord; settes lokalt av apphost, aldri hardkodet)", EnvTestPassword)
	}

	cfg := Config{
		Target:          target,
		Token:           token,
		PlatformOrgName: valueOr(getenv(EnvPlatformOrg), defaultPlatformOrg),
		TenantOrgName:   valueOr(getenv(EnvTenantOrg), defaultTenantOrg),
		ProjectName:     valueOr(getenv(EnvProjectName), defaultProjectName),
		TestPassword:    password,
	}
	cfg.Users = defaultUsers()
	return cfg, nil
}

// defaultUsers definerer testbrukerne (SPEC §12): minst én platform_admin i
// plattform-orgen, og én tenant_admin + én player i test-tenant-orgen.
func defaultUsers() []UserSpec {
	return []UserSpec{
		{
			Email:      "platform-admin@tronderleikan.local",
			GivenName:  "Platform",
			FamilyName: "Admin",
			InTenant:   false,
			Roles:      []string{authn.RolePlatformAdmin},
		},
		{
			Email:      "tenant-admin@demo.tronderleikan.local",
			GivenName:  "Tenant",
			FamilyName: "Admin",
			InTenant:   true,
			Roles:      []string{authn.RoleTenantAdmin},
		},
		{
			Email:      "player@demo.tronderleikan.local",
			GivenName:  "Demo",
			FamilyName: "Player",
			InTenant:   true,
			Roles:      []string{authn.RolePlayer},
		},
	}
}

// resolveToken henter PAT-en fra env (inline) eller fra en fil. Nøyaktig én av
// kildene må gi en ikke-tom verdi.
func resolveToken(getenv func(string) string, readFile func(string) ([]byte, error)) (string, error) {
	inline := strings.TrimSpace(getenv(EnvPAT))
	file := strings.TrimSpace(getenv(EnvPATFile))

	switch {
	case inline != "" && file != "":
		return "", fmt.Errorf("sett kun én av %s eller %s, ikke begge", EnvPAT, EnvPATFile)
	case inline != "":
		return inline, nil
	case file != "":
		b, err := readFile(file)
		if err != nil {
			return "", fmt.Errorf("lese %s (%s): %w", EnvPATFile, file, err)
		}
		token := strings.TrimSpace(string(b))
		if token == "" {
			return "", fmt.Errorf("%s (%s) er tom", EnvPATFile, file)
		}
		return token, nil
	default:
		return "", fmt.Errorf("mangler credentials: sett %s eller %s", EnvPAT, EnvPATFile)
	}
}

// parseTarget dekomponerer en full URL til domain/port/tls for zitadel-go.
// http -> insecure (lokal), https -> TLS (cluster). Port utledes fra URL-en
// eller settes til skjema-defaulten.
func parseTarget(rawURL string) (Target, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Target{}, fmt.Errorf("ugyldig URL %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return Target{}, fmt.Errorf("URL %q mangler host", rawURL)
	}
	var tls bool
	switch u.Scheme {
	case "https":
		tls = true
	case "http":
		tls = false
	default:
		return Target{}, fmt.Errorf("ustøttet skjema %q (bruk http eller https)", u.Scheme)
	}
	port := u.Port()
	if port == "" {
		if tls {
			port = "443"
		} else {
			port = "80"
		}
	}
	return Target{Domain: u.Hostname(), Port: port, TLS: tls}, nil
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
