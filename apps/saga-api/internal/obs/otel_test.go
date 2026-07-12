package obs

import (
	"context"
	"testing"
)

// TestSetupDisabled verifies the empty-endpoint path never depends on a
// running collector: it must return a usable shutdown func and no error,
// and that shutdown must be a true no-op (returns nil, does not block or
// dial out).
func TestSetupDisabled(t *testing.T) {
	shutdown, err := Setup(context.Background(), "", "test")
	if err != nil {
		t.Fatalf("Setup(disabled) returned err: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Setup(disabled) returned nil shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("disabled shutdown() returned err: %v", err)
	}
	if Tracer == nil {
		t.Fatal("Setup(disabled) left Tracer nil")
	}
	if Met.JobsTotal == nil {
		t.Fatal("Setup(disabled) left Met instruments nil")
	}
}
