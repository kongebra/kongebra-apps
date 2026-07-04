// Package outbox implementerer transactional outbox (SPEC §9):
// domene-events skrives til outbox-tabellen i samme transaksjon som
// domene-endringen, og en publisher-goroutine flytter dem til NATS JetStream.
package outbox

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

// Migrations inneholder goose-migrasjonen for outbox-tabellen.
// Tjenester kopierer SQL-fila inn i sin egen migrasjonskatalog (hver tjeneste
// eier sin egen versjonsrekkefølge), eller leser den herfra ved oppsett.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Write insert-er et envelope i outbox-tabellen innenfor samme transaksjon
// som domene-endringen. Subject = envelope.Type (event-katalogen i SPEC §9
// bruker samme streng som NATS-subject).
func Write(ctx context.Context, tx pgx.Tx, env event.Envelope) error {
	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO outbox (id, tenant_id, subject, payload) VALUES ($1, $2, $3, $4)`,
		env.EventID, env.TenantID, env.Type, payload,
	)
	if err != nil {
		return fmt.Errorf("insert outbox row: %w", err)
	}
	return nil
}
