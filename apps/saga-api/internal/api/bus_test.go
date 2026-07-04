package api

import (
	"testing"
	"time"

	"saga-api/internal/module"
)

func TestBusPublishSubscribe(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe(1)
	defer cancel()
	b.Publish(1, module.Event{Stage: "fetching"})
	b.Publish(2, module.Event{Stage: "other-job"})
	select {
	case ev := <-ch:
		if ev.Stage != "fetching" {
			t.Errorf("got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
	select {
	case ev := <-ch:
		t.Fatalf("leaked event from other job: %+v", ev)
	default:
	}
}

func TestBusPublishToNobodyDoesNotBlock(t *testing.T) {
	b := NewBus()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish(42, module.Event{Stage: "s"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked")
	}
}

func TestBusCancelIsIdempotent(t *testing.T) {
	b := NewBus()
	_, cancel := b.Subscribe(1)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("cancel panicked: %v", r)
		}
	}()
	cancel()
	cancel() // Should not panic
	// Verify that a subsequent Publish does not panic
	b.Publish(1, module.Event{Stage: "test"})
}
