package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ErrRosterUnavailable: roster-tjenesten svarte ikke eller ga uventet status ved
// ref-validering. Mapper til HTTP 502 (competition kan ikke bekrefte referansen).
var ErrRosterUnavailable = errors.New("roster service unavailable")

// RefValidator validerer at en person-referanse finnes i roster FØR competition
// persister den (SPEC §7: "refs valideres ved skriving, ellers events").
//
// ponytail: synkron punkt-validering nå. Robust nok for v1 (roster er kilden).
// Oppgraderingssti: konsumer tl.roster.person.deleted og rydd/anonymiser
// competition-referanser event-drevet (reconvergens), slik at en person slettet
// ETTER påmelding håndteres uten synkron kobling.
type RefValidator interface {
	// PersonExists sier om person-ID-en finnes i tenanten i roster.
	// bearerToken videresendes til roster (organizer-tokenet fra kalleren) slik
	// at roster sin read-gate slipper oppslaget gjennom uavhengig av
	// public_visibility. Feiler roster (nede/uventet) -> ErrRosterUnavailable.
	PersonExists(ctx context.Context, tenantID, personID uuid.UUID, bearerToken string) (bool, error)
}

// RosterClient kaller roster sitt person-oppslag over HTTP. baseURL er
// ROSTER_URL (SPEC §11: aldri hardkodet - fra env).
type RosterClient struct {
	baseURL string
	client  *http.Client
}

// NewRosterClient lager en klient med en fornuftig timeout.
func NewRosterClient(baseURL string) *RosterClient {
	return &RosterClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *RosterClient) PersonExists(ctx context.Context, tenantID, personID uuid.UUID, bearerToken string) (bool, error) {
	url := fmt.Sprintf("%s/api/roster/tenants/%s/persons/%s", c.baseURL, tenantID, personID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("build roster request: %w", err)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrRosterUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		// 401/403/5xx osv.: competition kan ikke bekrefte referansen -> feil tydelig.
		return false, fmt.Errorf("%w: uventet status %d fra roster", ErrRosterUnavailable, resp.StatusCode)
	}
}

var _ RefValidator = (*RosterClient)(nil)
