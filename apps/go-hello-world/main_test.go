package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// whoamiJSON skal gi gyldig JSON med alle fire feltene, inkl. tomme node/namespace
// (lokal kjøring uten Downward API).
func TestWhoamiJSON(t *testing.T) {
	cases := []struct {
		name                          string
		pod, node, namespace, version string
	}{
		{"k8s", "go-hello-world-abc", "node-1", "go-hello-world-prod", "2026-06-27-abc1234"},
		{"local-empty-downward-api", "laptop", "", "", "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got map[string]string
			if err := json.Unmarshal([]byte(whoamiJSON(c.pod, c.node, c.namespace, c.version)), &got); err != nil {
				t.Fatalf("ugyldig JSON: %v", err)
			}
			for k, want := range map[string]string{"pod": c.pod, "node": c.node, "namespace": c.namespace, "version": c.version} {
				if got[k] != want {
					t.Errorf("felt %q = %q, vil ha %q", k, got[k], want)
				}
			}
		})
	}
}

// logRequest skal slippe requesten videre til neste handler (passthrough).
func TestLogRequestPassthrough(t *testing.T) {
	called := false
	h := logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/whoami", nil))
	if !called {
		t.Fatal("neste handler ble ikke kalt")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, vil ha %d", rec.Code, http.StatusTeapot)
	}
}
