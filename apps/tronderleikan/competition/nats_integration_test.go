package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/outbox"
)

// natsEnv kobler til NATS JetStream for ende-til-ende event-tester.
// Krever COMPETITION_TEST_NATS_URL (+ COMPETITION_TEST_DATABASE_URL via testEnv).
func natsEnv(t *testing.T) jetstream.JetStream {
	t.Helper()
	url := os.Getenv("COMPETITION_TEST_NATS_URL")
	if url == "" {
		t.Skip("COMPETITION_TEST_NATS_URL ikke satt - hopper over NATS-E2E")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(func() { nc.Drain() }) //nolint:errcheck
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	ctx := context.Background()
	_ = js.DeleteStream(ctx, StreamName) // frisk stream per kjøring
	if err := ensureStream(ctx, js); err != nil {
		t.Fatalf("ensureStream: %v", err)
	}
	return js
}

// TestE2EResultRecordedReachesNATS kjører hele flyten mot ekte Postgres + NATS:
// tournament -> game -> deltakere -> plasseringer (med ties) -> outbox ->
// publisher -> JetStream, og verifiser result.recorded-eventet på NATS.
func TestE2EResultRecordedReachesNATS(t *testing.T) {
	_, app := testEnv(t)
	js := natsEnv(t)
	ctx := context.Background()

	store := NewPgStore(app)
	tenant := uuid.New()
	gameID := seedGame(t, store, tenant)

	var parts []uuid.UUID
	for range 2 {
		pid := uuid.New()
		p, err := store.RegisterParticipant(ctx, tenant, ParticipantInput{GameID: gameID, Type: ParticipantPerson, PersonID: &pid})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		parts = append(parts, p.ID)
	}
	// Ties: begge på 1. plass.
	if _, err := store.RecordResults(ctx, tenant, gameID, []PlacementInput{
		{ParticipantID: parts[0], Rank: 1},
		{ParticipantID: parts[1], Rank: 1},
	}); err != nil {
		t.Fatalf("RecordResults: %v", err)
	}

	// Publisher flytter usendte outbox-rader til JetStream (SPEC §9).
	if published, err := outbox.NewPublisher(app, js).PublishPending(ctx); err != nil {
		t.Fatalf("PublishPending: %v", err)
	} else if published == 0 {
		t.Fatal("publisher fant ingen outbox-rader")
	}

	env := consumeOne(t, js, event.Subject("competition", "result", "recorded"))
	if env.TenantID != tenant {
		t.Errorf("event tenant = %s, vil ha %s", env.TenantID, tenant)
	}
	var d resultRecordedData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.GameID != gameID || len(d.Placements) != 2 {
		t.Fatalf("event data = %+v", d)
	}
	ties := 0
	for _, p := range d.Placements {
		if p.Rank == 1 {
			ties++
		}
	}
	if ties != 2 {
		t.Errorf("event ties = %d rank-1, vil ha 2", ties)
	}
}

// TestE2ERefValidationViaHTTP driver den EKTE HTTP-handleren + PgStore mot ekte
// Postgres, med en httptest-server som står inn for roster (SPEC §7). Bekrefter
// at en person_id som ikke finnes i roster avvises (422) og IKKE persisteres,
// mens en kjent person går gjennom (201). Kun auth-token og selve
// roster-tjenesten er fakes; ref-validering + persist er ekte.
func TestE2ERefValidationViaHTTP(t *testing.T) {
	_, app := testEnv(t)
	ctx := context.Background()
	store := NewPgStore(app)
	tenant := uuid.New()
	gameID := seedGame(t, store, tenant)

	known := uuid.New()
	rosterSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/roster/tenants/"+tenant.String()+"/persons/"+known.String() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer rosterSrv.Close()

	a := &api{
		store:     store,
		vis:       NewPgVisibility(app),
		validator: fakeValidator{},
		roster:    NewRosterClient(rosterSrv.URL),
	}
	h := a.routes()
	path := "/api/competition/tenants/" + tenant.String() + "/games/" + gameID.String() + "/participants"

	// Ukjent person -> 422, ingen deltaker persistert.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodPost, path, "organizer",
		`{"type":"person","person_id":"`+uuid.New().String()+`"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ukjent person: status = %d, vil ha 422 (body %s)", rec.Code, rec.Body.String())
	}
	if parts, _ := store.ListParticipants(ctx, tenant, gameID); len(parts) != 0 {
		t.Fatalf("deltaker persistert tross avvist ref: %d", len(parts))
	}

	// Kjent person -> 201, persistert.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodPost, path, "organizer",
		`{"type":"person","person_id":"`+known.String()+`"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("kjent person: status = %d, vil ha 201 (body %s)", rec.Code, rec.Body.String())
	}
	if parts, _ := store.ListParticipants(ctx, tenant, gameID); len(parts) != 1 {
		t.Fatalf("kjent person ikke persistert: %d deltakere", len(parts))
	}
}

// consumeOne henter én melding på subjectet via en ephemeral consumer.
func consumeOne(t *testing.T, js jetstream.JetStream, subject string) event.Envelope {
	t.Helper()
	ctx := context.Background()
	cons, err := js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		FilterSubjects: []string{subject},
		AckPolicy:      jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	msg, err := cons.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		t.Fatalf("ingen melding på %s innen 5s: %v", subject, err)
	}
	_ = msg.Ack()
	var env event.Envelope
	if err := json.Unmarshal(msg.Data(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}
