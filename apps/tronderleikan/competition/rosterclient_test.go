package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestRosterClientPersonExists kjører den EKTE HTTP-klienten mot en httptest-
// server som etterligner roster sitt person-oppslag (SPEC §7 ref-validering).
func TestRosterClientPersonExists(t *testing.T) {
	tenant := uuid.New()
	known := uuid.New()
	unknown := uuid.New()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// Speiler roster-ruten: /api/roster/tenants/{tenant}/persons/{id}
		want := "/api/roster/tenants/" + tenant.String() + "/persons/"
		switch r.URL.Path {
		case want + known.String():
			w.WriteHeader(http.StatusOK)
		case want + unknown.String():
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := NewRosterClient(srv.URL)
	ctx := context.Background()

	t.Run("kjent person -> true + token videresendt", func(t *testing.T) {
		ok, err := c.PersonExists(ctx, tenant, known, "org-token")
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if gotAuth != "Bearer org-token" {
			t.Errorf("Authorization = %q, vil ha 'Bearer org-token'", gotAuth)
		}
	})
	t.Run("ukjent person -> false, ingen feil", func(t *testing.T) {
		ok, err := c.PersonExists(ctx, tenant, unknown, "t")
		if err != nil || ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	t.Run("uventet status -> ErrRosterUnavailable", func(t *testing.T) {
		_, err := c.PersonExists(ctx, tenant, uuid.New(), "t")
		if !errors.Is(err, ErrRosterUnavailable) {
			t.Fatalf("err = %v, vil ha ErrRosterUnavailable", err)
		}
	})
}

func TestRosterClientNetworkError(t *testing.T) {
	// Peker på en port ingen lytter på -> nettverksfeil -> ErrRosterUnavailable.
	c := NewRosterClient("http://127.0.0.1:1")
	_, err := c.PersonExists(context.Background(), uuid.New(), uuid.New(), "t")
	if !errors.Is(err, ErrRosterUnavailable) {
		t.Fatalf("err = %v, vil ha ErrRosterUnavailable", err)
	}
}
