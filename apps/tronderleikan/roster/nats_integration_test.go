package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/outbox"
)

// natsEnv kobler til NATS JetStream + Postgres for ende-til-ende event-tester.
// Krever ROSTER_TEST_NATS_URL (+ ROSTER_TEST_DATABASE_URL via testEnv). Skip ellers.
func natsEnv(t *testing.T) jetstream.JetStream {
	t.Helper()
	url := os.Getenv("ROSTER_TEST_NATS_URL")
	if url == "" {
		t.Skip("ROSTER_TEST_NATS_URL ikke satt - hopper over NATS-E2E")
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
	// Frisk stream per kjøring så tidligere meldinger ikke lekker inn.
	_ = js.DeleteStream(ctx, StreamName)
	if err := ensureStream(ctx, js); err != nil {
		t.Fatalf("ensureStream: %v", err)
	}
	return js
}

// TestE2EPersonEventReachesNATS: Create -> outbox -> publisher -> JetStream.
// Beviser at person.created faktisk lander på NATS med riktig tenant + payload.
func TestE2EPersonEventReachesNATS(t *testing.T) {
	admin, app := testEnv(t)
	_ = admin
	js := natsEnv(t)
	ctx := context.Background()

	store := NewPgStore(app)
	tenant := uuid.New()
	p, err := store.Create(ctx, tenant, PersonInput{Name: "E2E Ada"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Publisher flytter usendte outbox-rader til JetStream (SPEC §9).
	published, err := outbox.NewPublisher(app, js).PublishPending(ctx)
	if err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if published == 0 {
		t.Fatal("publisher fant ingen outbox-rader å publisere")
	}

	// Konsumer person.created og verifiser innhold.
	env := consumeOne(t, js, event.Subject("roster", "person", "created"))
	if env.TenantID != tenant {
		t.Errorf("event tenant = %s, vil ha %s", env.TenantID, tenant)
	}
	var d personCreatedData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if d.PersonID != p.ID || d.Name != "E2E Ada" {
		t.Errorf("event data = %+v, vil ha person %s / E2E Ada", d, p.ID)
	}
}

// TestE2ETenantConsumerUpdatesVisibility: en platform tenant.updated med
// public_visibility=false skal, via roster sin konsument, sette tenant_projection
// slik at IsPublic returnerer false (SPEC §6/§7 event-drevet decoupling).
func TestE2ETenantConsumerUpdatesVisibility(t *testing.T) {
	admin, app := testEnv(t)
	_ = admin
	js := natsEnv(t)
	ctx := context.Background()
	tenant := uuid.New()

	vis := NewPgVisibility(app)
	// Default (ukjent tenant) = offentlig (SPEC §6 default på).
	if public, err := vis.IsPublic(ctx, tenant); err != nil || !public {
		t.Fatalf("default IsPublic = %v/%v, vil ha true/nil", public, err)
	}

	// Start konsumenten.
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = runTenantConsumer(cctx, js, app) }()

	// Publiser en platform tenant.updated (public_visibility=false).
	env, err := event.New(tenant, event.Subject("platform", "tenant", "updated"),
		map[string]any{"public_visibility": false})
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	payload, _ := json.Marshal(env)
	msg := &nats.Msg{Subject: env.Type, Data: payload, Header: nats.Header{}}
	msg.Header.Set(nats.MsgIdHdr, env.EventID.String())
	if _, err := js.PublishMsg(ctx, msg); err != nil {
		t.Fatalf("publish tenant event: %v", err)
	}

	// Vent til projeksjonen er oppdatert.
	deadline := time.Now().Add(5 * time.Second)
	for {
		public, err := vis.IsPublic(ctx, tenant)
		if err != nil {
			t.Fatalf("IsPublic: %v", err)
		}
		if !public {
			break // konsumenten har oppdatert projeksjonen
		}
		if time.Now().After(deadline) {
			t.Fatal("tenant-konsumenten oppdaterte ikke public_visibility innen 5s")
		}
		time.Sleep(100 * time.Millisecond)
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
