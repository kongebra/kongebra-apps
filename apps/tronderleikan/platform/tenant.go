// Package main er platform-tjenesten (SPEC §7): tenant-registry,
// Zitadel-provisjonering og admin-plane-API. Tenant-registeret ER
// tenant-tabellen og er derfor - bevisst - ikke selv tenant-scopet (SPEC §8):
// ingen RLS, ingen X-Tenant-ID. Resten av mønsteret (UUIDv7, outbox, OTLP)
// følges likevel.
package main

import (
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Tenant er et miljø/organisasjon (SPEC §5), 1:1 med en Zitadel Organization.
type Tenant struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	ZitadelOrgID     string    `json:"zitadel_org_id"`
	PublicVisibility bool      `json:"public_visibility"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// slugPattern: små bokstaver a-z, tall og enkelt-bindestrek som skilletegn.
// Ingen ledende/etterfølgende eller doble bindestreker. Brukes i URL-er
// (SPEC §10, /t/<slug>/...) og må derfor være URL-trygg og stabil.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const (
	slugMinLen = 2
	slugMaxLen = 63 // som en DNS-label; god margin for lesbare slugs
	nameMaxLen = 200
)

// ErrInvalidSlug og ErrInvalidName er valideringsfeil (mappes til 400 i API-et).
var (
	ErrInvalidSlug = errors.New("ugyldig slug: kun a-z, 0-9 og enkelt-bindestrek, 2-63 tegn")
	ErrInvalidName = errors.New("ugyldig navn: må være 1-200 tegn")
)

// ValidateSlug håndhever slug-reglene. Ren funksjon - enhetstestet.
func ValidateSlug(slug string) error {
	if len(slug) < slugMinLen || len(slug) > slugMaxLen || !slugPattern.MatchString(slug) {
		return ErrInvalidSlug
	}
	return nil
}

// ValidateName håndhever navnereglene (ikke-tomt, ikke absurd langt).
func ValidateName(name string) error {
	if name == "" || len(name) > nameMaxLen {
		return ErrInvalidName
	}
	return nil
}
