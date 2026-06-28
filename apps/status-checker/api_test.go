package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusHandlerShape(t *testing.T) {
	c := newChecker([]Target{{Name: "a", URL: "https://a.newb.no", HealthPath: "/health"}}, 30*time.Second, 5*time.Second)
	c.store.set([]Result{{Name: "a", URL: "https://a.newb.no", Status: StatusUp}})

	rec := httptest.NewRecorder()
	statusHandler(c)(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("kode = %d", rec.Code)
	}
	var body struct {
		CheckedAt string   `json:"checked_at"`
		Services  []Result `json:"services"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ugyldig JSON: %v", err)
	}
	if body.CheckedAt == "" {
		t.Error("checked_at mangler")
	}
	if len(body.Services) != 1 || body.Services[0].Name != "a" {
		t.Errorf("services feil: %+v", body.Services)
	}
}
