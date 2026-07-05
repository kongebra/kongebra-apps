package main

import (
	"fmt"
	"os"
)

// Config er tjenestens runtime-konfig, utelukkende fra env (SPEC §5, §11).
// Issuer/domenet hardkodes ALDRI - det leses fra AUTH_ISSUER (SPEC §5).
// RosterURL peker på roster-tjenesten (ref-validering ved skriving, SPEC §7).
type Config struct {
	Port         string // HTTP-port (PORT, default 8080)
	DatabaseURL  string // Postgres DSN (DATABASE_URL)
	NatsURL      string // NATS-adresse (NATS_URL)
	AuthIssuer   string // OIDC-issuer (AUTH_ISSUER) - validert i pkg/authn
	AuthAudience string // forventet JWT-audience/project-ID (AUTH_AUDIENCE)
	RosterURL    string // base-URL til roster-tjenesten (ROSTER_URL) for ref-validering
}

// loadConfig leser konfig fra env og feiler tidlig om noe påkrevd mangler.
func loadConfig() (Config, error) {
	c := Config{
		Port:         envOr("PORT", "8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		NatsURL:      os.Getenv("NATS_URL"),
		AuthIssuer:   os.Getenv("AUTH_ISSUER"),
		AuthAudience: os.Getenv("AUTH_AUDIENCE"),
		RosterURL:    os.Getenv("ROSTER_URL"),
	}
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.NatsURL == "" {
		missing = append(missing, "NATS_URL")
	}
	if c.AuthIssuer == "" {
		missing = append(missing, "AUTH_ISSUER")
	}
	if c.AuthAudience == "" {
		missing = append(missing, "AUTH_AUDIENCE")
	}
	if c.RosterURL == "" {
		missing = append(missing, "ROSTER_URL")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("mangler påkrevde env-varer: %v", missing)
	}
	return c, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
