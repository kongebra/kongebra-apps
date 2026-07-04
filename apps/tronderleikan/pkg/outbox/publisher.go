package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TenantHeader er NATS-headeren som bærer tenant-ID (SPEC §9: tenant i header/payload).
const TenantHeader = "Tl-Tenant-Id"

// DB er delmengden av *pgxpool.Pool publisheren trenger (fakes i test).
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// JetStream er delmengden av jetstream.JetStream publisheren trenger (fakes i test).
type JetStream interface {
	PublishMsg(ctx context.Context, msg *nats.Msg, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Publisher poller usendte outbox-rader og publiserer dem til NATS JetStream.
//
// Idempotens: Nats-Msg-Id settes til event-id, så JetStream dedup-er
// re-publiseringer (f.eks. etter krasj mellom publish og mark-sent).
// Konsumenter skal i tillegg dedup-e på event_id (SPEC §9).
type Publisher struct {
	db DB
	js JetStream

	// PollInterval er ventetiden mellom tomme/normale poll-runder (default 1s).
	PollInterval time.Duration
	// BatchSize er maks antall rader per runde (default 100).
	BatchSize int
}

// NewPublisher lager en publisher med fornuftige defaults.
func NewPublisher(db DB, js JetStream) *Publisher {
	return &Publisher{db: db, js: js, PollInterval: time.Second, BatchSize: 100}
}

// Run poller til ctx kanselleres og returnerer da ctx.Err().
// Enkel backoff: ved feil dobles ventetiden opp til 30s, resettes ved suksess.
// Full batch => neste runde umiddelbart (det ligger sannsynligvis mer i kø).
func (p *Publisher) Run(ctx context.Context) error {
	const maxBackoff = 30 * time.Second
	delay := p.PollInterval
	for {
		n, err := p.PublishPending(ctx)
		switch {
		case err != nil:
			delay = min(delay*2, maxBackoff)
		case n == p.BatchSize:
			delay = 0
		default:
			delay = p.PollInterval
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// PublishPending publiserer én batch usendte rader (eldste først - UUIDv7 er
// tidsordnet, så ORDER BY id = innsettingsrekkefølge) og returnerer antall
// publisert. Hver rad markeres sendt umiddelbart etter ack, så en feil midt i
// batchen re-publiserer aldri allerede ack-ede rader.
func (p *Publisher) PublishPending(ctx context.Context) (int, error) {
	// ponytail: ingen SELECT ... FOR UPDATE SKIP LOCKED - vi antar én
	// publisher per tjeneste (single replica). Kjører flere replicas er
	// dobbeltpublisering uansett ufarlig (JetStream-dedup på Nats-Msg-Id);
	// oppgraderingssti = poll i tx med FOR UPDATE SKIP LOCKED.
	rows, err := p.db.Query(ctx,
		`SELECT id, tenant_id, subject, payload FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT $1`,
		p.BatchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("query outbox: %w", err)
	}
	type pending struct {
		id       uuid.UUID
		tenantID uuid.UUID
		subject  string
		payload  []byte
	}
	var batch []pending
	for rows.Next() {
		var r pending
		if err := rows.Scan(&r.id, &r.tenantID, &r.subject, &r.payload); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan outbox row: %w", err)
		}
		batch = append(batch, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate outbox rows: %w", err)
	}

	published := 0
	for _, r := range batch {
		msg := &nats.Msg{Subject: r.subject, Data: r.payload, Header: nats.Header{}}
		msg.Header.Set(nats.MsgIdHdr, r.id.String())
		msg.Header.Set(TenantHeader, r.tenantID.String())
		if _, err := p.js.PublishMsg(ctx, msg); err != nil {
			return published, fmt.Errorf("publish %s: %w", r.subject, err)
		}
		if _, err := p.db.Exec(ctx,
			`UPDATE outbox SET published_at = now() WHERE id = $1`, r.id,
		); err != nil {
			return published, fmt.Errorf("mark outbox row published: %w", err)
		}
		published++
	}
	return published, nil
}
