package main

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/kongebra/kongebra-apps/apps/tronderleikan/pkg/event"
)

func TestParseTenantEvent(t *testing.T) {
	tenant := uuid.New()

	build := func(data any) []byte {
		env, err := event.New(tenant, event.Subject("platform", "tenant", "updated"), data)
		if err != nil {
			t.Fatalf("event.New: %v", err)
		}
		b, _ := json.Marshal(env)
		return b
	}

	t.Run("public_visibility=false leses", func(t *testing.T) {
		id, public, err := parseTenantEvent(build(map[string]any{"public_visibility": false}))
		if err != nil || id != tenant || public {
			t.Fatalf("id=%s public=%v err=%v", id, public, err)
		}
	})
	t.Run("manglende public_visibility -> default true", func(t *testing.T) {
		id, public, err := parseTenantEvent(build(map[string]any{"slug": "x"}))
		if err != nil || id != tenant || !public {
			t.Fatalf("id=%s public=%v err=%v", id, public, err)
		}
	})
	t.Run("ugyldig json -> feil", func(t *testing.T) {
		if _, _, err := parseTenantEvent([]byte("{not json")); err == nil {
			t.Error("ventet feil")
		}
	})
	t.Run("manglende tenant_id -> feil", func(t *testing.T) {
		env, _ := event.New(uuid.Nil, "tl.platform.tenant.updated", map[string]any{})
		b, _ := json.Marshal(env)
		if _, _, err := parseTenantEvent(b); err == nil {
			t.Error("ventet feil for tom tenant_id")
		}
	})
}
