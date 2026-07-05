package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Env-varer platform konfigureres fra. Ingen hemmeligheter i kode; issuer og
// Zitadel-domenet leses ALLTID fra env (SPEC §5, ufravikelig).
const (
	EnvDatabaseURL = "DATABASE_URL"
	EnvNatsURL     = "NATS_URL"
	EnvPort        = "PORT"

	// Zitadel-provisjonering (machine-user med IAM_OWNER).
	EnvZitadelAPIURL  = "ZITADEL_API_URL"
	EnvZitadelPAT     = "ZITADEL_PAT"
	EnvZitadelPATFile = "ZITADEL_PAT_FILE"

	// Grunntilstanden seeden la (zitadel-seed, pakke 0.4). Overstyrbar, med
	// defaults som matcher seedens defaults.
	EnvPlatformOrg = "SEED_PLATFORM_ORG_NAME"
	EnvProjectName = "SEED_PROJECT_NAME"
)

const (
	defaultPlatformOrg = "TronderLeikan Platform"
	defaultProjectName = "tronderleikan"
	defaultPort        = "8080"
)

// Target er en Zitadel-instans dekomponert til det zitadel-go-klienten trenger.
type Target struct {
	Domain string // hostname uten skjema/port
	Port   string // "8300", "443", ...
	TLS    bool   // false for http (lokal), true for https (cluster)
}

// Config er den ferdig-validerte konfigurasjonen.
type Config struct {
	DatabaseURL     string
	NatsURL         string
	Port            string
	ZitadelTarget   Target
	ZitadelToken    string
	PlatformOrgName string
	ProjectName     string
}

// LoadConfig leser og validerer konfigurasjon fra miljøet.
func LoadConfig(getenv func(string) string) (Config, error) {
	dbURL := getenv(EnvDatabaseURL)
	if dbURL == "" {
		return Config{}, fmt.Errorf("env %s må være satt", EnvDatabaseURL)
	}
	natsURL := getenv(EnvNatsURL)
	if natsURL == "" {
		return Config{}, fmt.Errorf("env %s må være satt", EnvNatsURL)
	}

	rawURL := getenv(EnvZitadelAPIURL)
	if rawURL == "" {
		return Config{}, fmt.Errorf("env %s må være satt - Zitadel-domenet hardkodes aldri (SPEC §5)", EnvZitadelAPIURL)
	}
	target, err := parseTarget(rawURL)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", EnvZitadelAPIURL, err)
	}

	token, err := resolveToken(getenv, os.ReadFile)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:     dbURL,
		NatsURL:         natsURL,
		Port:            valueOr(getenv(EnvPort), defaultPort),
		ZitadelTarget:   target,
		ZitadelToken:    token,
		PlatformOrgName: valueOr(getenv(EnvPlatformOrg), defaultPlatformOrg),
		ProjectName:     valueOr(getenv(EnvProjectName), defaultProjectName),
	}, nil
}

// resolveToken henter PAT-en fra env (inline) eller fra en fil. Nøyaktig én av
// kildene må gi en ikke-tom verdi.
func resolveToken(getenv func(string) string, readFile func(string) ([]byte, error)) (string, error) {
	inline := strings.TrimSpace(getenv(EnvZitadelPAT))
	file := strings.TrimSpace(getenv(EnvZitadelPATFile))

	switch {
	case inline != "" && file != "":
		return "", fmt.Errorf("sett kun én av %s eller %s, ikke begge", EnvZitadelPAT, EnvZitadelPATFile)
	case inline != "":
		return inline, nil
	case file != "":
		b, err := readFile(file)
		if err != nil {
			return "", fmt.Errorf("lese %s (%s): %w", EnvZitadelPATFile, file, err)
		}
		token := strings.TrimSpace(string(b))
		if token == "" {
			return "", fmt.Errorf("%s (%s) er tom", EnvZitadelPATFile, file)
		}
		return token, nil
	default:
		return "", fmt.Errorf("mangler credentials: sett %s eller %s", EnvZitadelPAT, EnvZitadelPATFile)
	}
}

// parseTarget dekomponerer en full URL til domain/port/tls for zitadel-go.
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
	if u.Path != "" && u.Path != "/" {
		return Target{}, fmt.Errorf("URL %q skal ikke ha sti (%q) - kun skjema://host[:port]", rawURL, u.Path)
	}
	if u.RawQuery != "" {
		return Target{}, fmt.Errorf("URL %q skal ikke ha query - kun skjema://host[:port]", rawURL)
	}
	if u.Fragment != "" {
		return Target{}, fmt.Errorf("URL %q skal ikke ha fragment - kun skjema://host[:port]", rawURL)
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
