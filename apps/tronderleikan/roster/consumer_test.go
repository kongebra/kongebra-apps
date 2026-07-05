package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

func TestParseTenantEvent(t *testing.T) {
	tenant := uuid.New()

	mk := func(t *testing.T, data any) []byte {
		t.Helper()
		env, err := event.New(tenant, event.Subject("platform", "tenant", "updated"), data)
		if err != nil {
			t.Fatalf("event.New: %v", err)
		}
		raw, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return raw
	}

	t.Run("public_visibility=false leses", func(t *testing.T) {
		id, public, err := parseTenantEvent(mk(t, map[string]any{"public_visibility": false}))
		if err != nil {
			t.Fatalf("parseTenantEvent: %v", err)
		}
		if id != tenant || public {
			t.Errorf("id=%s public=%v, vil ha %s/false", id, public, tenant)
		}
	})

	t.Run("public_visibility=true leses", func(t *testing.T) {
		_, public, err := parseTenantEvent(mk(t, map[string]any{"public_visibility": true}))
		if err != nil || !public {
			t.Errorf("public=%v err=%v, vil ha true/nil", public, err)
		}
	})

	t.Run("manglende felt -> default true (SPEC §6)", func(t *testing.T) {
		_, public, err := parseTenantEvent(mk(t, map[string]any{}))
		if err != nil || !public {
			t.Errorf("public=%v err=%v, vil ha true/nil", public, err)
		}
	})

	t.Run("ugyldig json -> feil", func(t *testing.T) {
		if _, _, err := parseTenantEvent([]byte("{ikke json")); err == nil {
			t.Error("parseTenantEvent godtok ugyldig json")
		}
	})
}
