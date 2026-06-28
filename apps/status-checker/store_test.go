package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStoreSnapshotIsolation(t *testing.T) {
	s := newStore()
	s.set([]Result{{Name: "a", Status: StatusUp}})
	snap := s.snapshot()
	snap[0].Name = "mutated"
	if s.snapshot()[0].Name != "a" {
		t.Fatal("snapshot skal være isolert fra intern state")
	}
}

func TestCheckerRunOnceParallelAndUnknownBefore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newChecker([]Target{
		{Name: "a", URL: srv.URL, HealthPath: "/"},
		{Name: "b", URL: srv.URL, HealthPath: "/"},
	}, 30*time.Second, 5*time.Second)

	// Før første kjøring: alle unknown.
	for _, r := range c.Snapshot() {
		if r.Status != StatusUnknown {
			t.Fatalf("%s: forventet unknown før runOnce, fikk %s", r.Name, r.Status)
		}
	}

	c.runOnce(context.Background())
	snap := c.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("forventet 2 resultater, fikk %d", len(snap))
	}
	for _, r := range snap {
		if r.Status != StatusUp {
			t.Errorf("%s: status = %s, vil ha up", r.Name, r.Status)
		}
	}
}
