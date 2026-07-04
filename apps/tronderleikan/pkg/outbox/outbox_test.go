package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

// --- fakes (testcontainer-fri: alt i minne) ---

// storedRow er en outbox-rad i den falske databasen.
type storedRow struct {
	id        uuid.UUID
	tenantID  uuid.UUID
	subject   string
	payload   []byte
	published bool
}

// fakeStore later som outbox-tabellen: fakeTx skriver inn, fakeDB leser ut.
type fakeStore struct {
	rows    []storedRow
	execErr error
}

// fakeTx implementerer pgx.Tx (kun Exec brukes; resten paniker via nil-embed).
type fakeTx struct {
	pgx.Tx
	store *fakeStore
}

func (t *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(sql, "INSERT INTO outbox") {
		return pgconn.CommandTag{}, errors.New("uventet SQL: " + sql)
	}
	t.store.rows = append(t.store.rows, storedRow{
		id:       args[0].(uuid.UUID),
		tenantID: args[1].(uuid.UUID),
		subject:  args[2].(string),
		payload:  args[3].([]byte),
	})
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

// fakeDB implementerer publisherens DB-interface over fakeStore.
type fakeDB struct {
	store *fakeStore
}

func (d *fakeDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !strings.HasPrefix(sql, "SELECT id, tenant_id, subject, payload FROM outbox") {
		return nil, errors.New("uventet SQL: " + sql)
	}
	limit := args[0].(int)
	var pending []storedRow
	for _, r := range d.store.rows {
		if !r.published && len(pending) < limit {
			pending = append(pending, r)
		}
	}
	return &fakeRows{rows: pending, idx: -1}, nil
}

func (d *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if d.store.execErr != nil {
		return pgconn.CommandTag{}, d.store.execErr
	}
	if !strings.HasPrefix(sql, "UPDATE outbox SET published_at") {
		return pgconn.CommandTag{}, errors.New("uventet SQL: " + sql)
	}
	id := args[0].(uuid.UUID)
	for i := range d.store.rows {
		if d.store.rows[i].id == id {
			d.store.rows[i].published = true
		}
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// fakeRows implementerer pgx.Rows for en fast liste rader.
type fakeRows struct {
	rows []storedRow
	idx  int
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool {
	r.idx++
	return r.idx < len(r.rows)
}
func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.idx]
	*(dest[0].(*uuid.UUID)) = row.id
	*(dest[1].(*uuid.UUID)) = row.tenantID
	*(dest[2].(*string)) = row.subject
	*(dest[3].(*[]byte)) = row.payload
	return nil
}
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte    { return nil }
func (r *fakeRows) Conn() *pgx.Conn        { return nil }

// fakeJS samler publiserte meldinger, med mulighet for å feile de første N kallene.
type fakeJS struct {
	published []*nats.Msg
	failNext  int
}

func (j *fakeJS) PublishMsg(_ context.Context, msg *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if j.failNext > 0 {
		j.failNext--
		return nil, errors.New("nats nede")
	}
	j.published = append(j.published, msg)
	return &jetstream.PubAck{}, nil
}

// --- tester ---

func mustEnvelope(t *testing.T, tenant uuid.UUID) event.Envelope {
	t.Helper()
	env, err := event.New(tenant, "tl.competition.result.recorded", map[string]int{"place": 1})
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	return env
}

func TestWriteInsertsEnvelope(t *testing.T) {
	store := &fakeStore{}
	tenant := uuid.New()
	env := mustEnvelope(t, tenant)

	if err := Write(context.Background(), &fakeTx{store: store}, env); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("rows = %d, vil ha 1", len(store.rows))
	}
	row := store.rows[0]
	if row.id != env.EventID || row.tenantID != tenant || row.subject != env.Type {
		t.Errorf("rad = %+v, matcher ikke envelope %+v", row, env)
	}
	var stored event.Envelope
	if err := json.Unmarshal(row.payload, &stored); err != nil {
		t.Fatalf("payload er ikke et gyldig envelope: %v", err)
	}
	if stored.EventID != env.EventID {
		t.Errorf("payload event_id = %s, vil ha %s", stored.EventID, env.EventID)
	}
}

func TestPublishPendingPublishesAndMarks(t *testing.T) {
	store := &fakeStore{}
	tenant := uuid.New()
	env1 := mustEnvelope(t, tenant)
	env2 := mustEnvelope(t, tenant)
	tx := &fakeTx{store: store}
	for _, env := range []event.Envelope{env1, env2} {
		if err := Write(context.Background(), tx, env); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	js := &fakeJS{}
	p := NewPublisher(&fakeDB{store: store}, js)

	n, err := p.PublishPending(context.Background())
	if err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if n != 2 || len(js.published) != 2 {
		t.Fatalf("publisert %d/%d, vil ha 2", n, len(js.published))
	}

	msg := js.published[0]
	if msg.Subject != env1.Type {
		t.Errorf("subject = %q, vil ha %q", msg.Subject, env1.Type)
	}
	if got := msg.Header.Get(nats.MsgIdHdr); got != env1.EventID.String() {
		t.Errorf("Nats-Msg-Id = %q, vil ha event-id %q (idempotens-nøkkel)", got, env1.EventID)
	}
	if got := msg.Header.Get(TenantHeader); got != tenant.String() {
		t.Errorf("%s = %q, vil ha %q", TenantHeader, got, tenant)
	}

	// idempotent: ny runde har ingenting å publisere
	n, err = p.PublishPending(context.Background())
	if err != nil {
		t.Fatalf("PublishPending (2. runde): %v", err)
	}
	if n != 0 || len(js.published) != 2 {
		t.Errorf("2. runde publiserte %d (totalt %d), vil ha 0 (totalt 2)", n, len(js.published))
	}
}

func TestPublishPendingStopsOnPublishError(t *testing.T) {
	store := &fakeStore{}
	tx := &fakeTx{store: store}
	if err := Write(context.Background(), tx, mustEnvelope(t, uuid.New())); err != nil {
		t.Fatalf("Write: %v", err)
	}

	js := &fakeJS{failNext: 1}
	p := NewPublisher(&fakeDB{store: store}, js)

	if _, err := p.PublishPending(context.Background()); err == nil {
		t.Fatal("PublishPending svelget publish-feilen")
	}
	if store.rows[0].published {
		t.Error("rad markert publisert selv om publish feilet")
	}

	// retry etter at NATS er oppe igjen lykkes
	n, err := p.PublishPending(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("retry: n=%d err=%v, vil ha n=1", n, err)
	}
	if !store.rows[0].published {
		t.Error("rad ikke markert publisert etter vellykket retry")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	store := &fakeStore{}
	p := NewPublisher(&fakeDB{store: store}, &fakeJS{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, vil ha context.Canceled", err)
	}
}

func TestMigrationsEmbedded(t *testing.T) {
	data, err := Migrations.ReadFile("migrations/0001_outbox.sql")
	if err != nil {
		t.Fatalf("les embedded migrasjon: %v", err)
	}
	for _, want := range []string{"-- +goose Up", "CREATE TABLE outbox", "-- +goose Down"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("migrasjonen mangler %q", want)
		}
	}
}
