// Package event definerer det delte event-envelopet for TrønderLeikan (SPEC §9).
// Alle domene-events pakkes i et CloudEvents-inspirert JSON-envelope og
// publiseres på NATS-subjects av formen tl.<service>.<entity>.<event>.
package event

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Envelope er event-envelopet alle tjenester deler.
// Data er payloaden slik den ble skrevet av produsenten (rå JSON).
type Envelope struct {
	EventID    uuid.UUID       `json:"event_id"`
	TenantID   uuid.UUID       `json:"tenant_id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Data       json.RawMessage `json:"data"`
}

// New lager et envelope med generert UUIDv7 og occurred_at = nå (UTC).
// data marshales til JSON. eventType skal være et subject fra event-katalogen
// i SPEC §9 (bygg det med Subject).
func New(tenantID uuid.UUID, eventType string, data any) (Envelope, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Envelope{}, fmt.Errorf("generate event id: %w", err)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal event data: %w", err)
	}
	return Envelope{
		EventID:    id,
		TenantID:   tenantID,
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
		Data:       payload,
	}, nil
}

// Subject bygger NATS-subjectet tl.<service>.<entity>.<event> (SPEC §9).
// Event-katalogen bruker samme streng som envelope.Type.
func Subject(service, entity, eventName string) string {
	return fmt.Sprintf("tl.%s.%s.%s", service, entity, eventName)
}
