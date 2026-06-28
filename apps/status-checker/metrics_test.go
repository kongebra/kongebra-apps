package main

import "testing"

// Røyktest: setupMetrics skal returnere en ikke-nil recorder uten å panikke,
// og recorderen skal tåle å bli kalt med resultater (inkl. nil latency).
func TestSetupMetricsRecord(t *testing.T) {
	rec, err := setupMetrics()
	if err != nil {
		t.Fatalf("setupMetrics: %v", err)
	}
	if rec == nil {
		t.Fatal("recorder er nil")
	}
	lat := int64(42)
	rec([]Result{
		{Name: "a", Status: StatusUp, LatencyMs: &lat},
		{Name: "b", Status: StatusDown}, // nil latency skal ikke panikke
	})
}
