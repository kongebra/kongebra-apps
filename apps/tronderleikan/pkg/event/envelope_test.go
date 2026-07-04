package event

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNew(t *testing.T) {
	tenant := uuid.MustParse("019112ea-0000-7000-8000-000000000001")
	before := time.Now().UTC()
	env, err := New(tenant, "tl.competition.result.recorded", map[string]string{"game_id": "g1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if env.EventID.Version() != 7 {
		t.Errorf("EventID version = %d, vil ha UUIDv7", env.EventID.Version())
	}
	if env.TenantID != tenant {
		t.Errorf("TenantID = %s, vil ha %s", env.TenantID, tenant)
	}
	if env.Type != "tl.competition.result.recorded" {
		t.Errorf("Type = %q", env.Type)
	}
	if env.OccurredAt.Before(before) || env.OccurredAt.After(time.Now().UTC()) {
		t.Errorf("OccurredAt = %s utenfor forventet vindu", env.OccurredAt)
	}
	if string(env.Data) != `{"game_id":"g1"}` {
		t.Errorf("Data = %s", env.Data)
	}
}

func TestNewRejectsUnmarshalableData(t *testing.T) {
	if _, err := New(uuid.New(), "tl.x.y.z", make(chan int)); err == nil {
		t.Fatal("New godtok data som ikke kan marshales")
	}
}

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	env, err := New(uuid.New(), "tl.roster.person.created", struct {
		Name string `json:"name"`
	}{Name: "Kari"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// kontrakts-sjekk: JSON-feltnavnene er kontrakten på wire (SPEC §9)
	for _, field := range []string{`"event_id"`, `"tenant_id"`, `"type"`, `"occurred_at"`, `"data"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("mangler felt %s i %s", field, raw)
		}
	}
	var got Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.EventID != env.EventID || got.TenantID != env.TenantID || got.Type != env.Type {
		t.Errorf("roundtrip mismatch: %+v != %+v", got, env)
	}
	if !got.OccurredAt.Equal(env.OccurredAt) {
		t.Errorf("OccurredAt roundtrip: %s != %s", got.OccurredAt, env.OccurredAt)
	}
}

func TestSubject(t *testing.T) {
	got := Subject("competition", "result", "recorded")
	if got != "tl.competition.result.recorded" {
		t.Errorf("Subject = %q", got)
	}
}
