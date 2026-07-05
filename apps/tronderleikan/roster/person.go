package main

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Person er en tenant-eid roster-entitet (SPEC §4). IKKE en brukerkonto.
// AccountID kobler valgfritt personen til en Zitadel-konto (nullable, unik per
// tenant). Deltakelse i competition/bracket/timing/rating peker alltid på ID,
// aldri på AccountID.
type Person struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	Name       string    `json:"name"`
	Department *string   `json:"department,omitempty"`
	AvatarURL  *string   `json:"avatar_url,omitempty"`
	AccountID  *string   `json:"account_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PersonInput er skrive-payloaden for opprett/oppdater. Peker-feltene skiller
// "ikke satt" fra "satt til tom": ved oppdatering betyr nil at feltet beholdes.
// AccountID settes ikke her - egen account-kobling-endepunkt (SPEC §4).
type PersonInput struct {
	Name       string  `json:"name"`
	Department *string `json:"department"`
	AvatarURL  *string `json:"avatar_url"`
}

// ErrInvalidInput returneres når input-validering feiler (mapper til HTTP 400).
var ErrInvalidInput = errors.New("invalid input")

// validateName sjekker at navnet ikke er tomt/whitespace og innenfor lengde.
// Returnerer det trimmede navnet.
func validateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("name er påkrevd")
	}
	if len(trimmed) > 200 {
		return "", errors.New("name er for langt (maks 200 tegn)")
	}
	return trimmed, nil
}

// normalize validerer og normaliserer input (trimmer navn, tomme optionale
// strenger blir nil). Returnerer feil wrappet i ErrInvalidInput ved ugyldig.
func (in PersonInput) normalize() (PersonInput, error) {
	name, err := validateName(in.Name)
	if err != nil {
		return PersonInput{}, errors.Join(ErrInvalidInput, err)
	}
	return PersonInput{
		Name:       name,
		Department: emptyToNil(in.Department),
		AvatarURL:  emptyToNil(in.AvatarURL),
	}, nil
}

// emptyToNil gjør en peker til tom/whitespace-streng om til nil, ellers trimmet.
func emptyToNil(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// validateAccountID sjekker account-kobling-input (SPEC §4). Kontoen er Zitadel
// sub (streng). Tom = ugyldig; bruk unlink-endepunktet for å fjerne koblingen.
func validateAccountID(accountID string) (string, error) {
	trimmed := strings.TrimSpace(accountID)
	if trimmed == "" {
		return "", errors.Join(ErrInvalidInput, errors.New("account_id er påkrevd"))
	}
	if len(trimmed) > 200 {
		return "", errors.Join(ErrInvalidInput, errors.New("account_id er for langt"))
	}
	return trimmed, nil
}
