package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

// StreamName er JetStream-strømmen som fanger alle tl.*-events (SPEC §9).
// I prod deklareres strømmen av gitops (NATS helm, arbeidspakke 0.5); roster
// ensure-r den idempotent lokalt så fallback-oppsettet (compose + go run) og
// Aspire fungerer uten manuelt stream-oppsett.
const StreamName = "tl"

// platformTenantData er det roster konsumerer fra platform sine tenant-events.
// Forward-kontrakt: platform-tjenesten (arbeidspakke 1.1) MÅ inkludere
// public_visibility i payloaden til tl.platform.tenant.provisioned|updated.
// ponytail: kun feltet roster trenger. Utvid når roster trenger mer av tenant.
type platformTenantData struct {
	PublicVisibility *bool `json:"public_visibility"`
}

// tenantEventSubjects er platform-subjectene roster lytter på for å holde
// public_visibility-projeksjonen à jour (SPEC §7 event-drevet decoupling).
var tenantEventSubjects = []string{
	event.Subject("platform", "tenant", "provisioned"),
	event.Subject("platform", "tenant", "updated"),
}

// parseTenantEvent trekker ut tenant-ID og public_visibility fra et
// platform-tenant-envelope. Mangler public_visibility -> default true
// (SPEC §6: default på). Ren funksjon - enhetstestbar uten NATS/DB.
func parseTenantEvent(payload []byte) (uuid.UUID, bool, error) {
	var env event.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return uuid.Nil, false, fmt.Errorf("unmarshal envelope: %w", err)
	}
	if env.TenantID == uuid.Nil {
		return uuid.Nil, false, fmt.Errorf("tenant-event mangler tenant_id")
	}
	var data platformTenantData
	if len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return uuid.Nil, false, fmt.Errorf("unmarshal tenant data: %w", err)
		}
	}
	public := true
	if data.PublicVisibility != nil {
		public = *data.PublicVisibility
	}
	return env.TenantID, public, nil
}

// ensureStream oppretter/oppdaterer tl-strømmen (idempotent) slik at outbox-
// publiseringen har et sted å skrive og konsumenten noe å lese.
func ensureStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{"tl.>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
	})
	if err != nil {
		return fmt.Errorf("ensure stream %s: %w", StreamName, err)
	}
	return nil
}

// runTenantConsumer starter en durable JetStream-konsument på platform sine
// tenant-events og oppdaterer tenant_projection. Blokkerer til ctx kanselleres.
func runTenantConsumer(ctx context.Context, js jetstream.JetStream, pool *pgxpool.Pool) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Durable:        "roster-tenant-projection",
		FilterSubjects: tenantEventSubjects,
		AckPolicy:      jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("create tenant consumer: %w", err)
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		tenantID, public, err := parseTenantEvent(msg.Data())
		if err != nil {
			log.Printf("tenant-consumer: dropper ugyldig event: %v", err)
			_ = msg.Term() // ugyldig payload - redriver aldri
			return
		}
		if err := upsertVisibility(ctx, pool, tenantID, public); err != nil {
			log.Printf("tenant-consumer: upsert feilet, nak: %v", err)
			_ = msg.Nak() // transient (DB nede) - redeliver
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("consume tenant events: %w", err)
	}
	defer cc.Stop()
	<-ctx.Done()
	return ctx.Err()
}
