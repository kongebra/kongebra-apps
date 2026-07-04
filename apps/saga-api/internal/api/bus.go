// Package api: HTTP handlers and the in-memory event bus that fans module
// progress out to SSE subscribers. Events are transient; the jobs table is
// the durable state (a reconnecting client gets a snapshot from there).
package api

import (
	"sync"

	"saga-api/internal/module"
)

type Bus struct {
	mu   sync.Mutex
	subs map[int64]map[chan module.Event]struct{}
}

func NewBus() *Bus {
	return &Bus{subs: map[int64]map[chan module.Event]struct{}{}}
}

func (b *Bus) Subscribe(jobID int64) (<-chan module.Event, func()) {
	ch := make(chan module.Event, 256)
	b.mu.Lock()
	if b.subs[jobID] == nil {
		b.subs[jobID] = map[chan module.Event]struct{}{}
	}
	b.subs[jobID][ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if set, ok := b.subs[jobID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, jobID)
			}
		}
		b.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// Publish never blocks: a slow subscriber loses events (token stream is
// cosmetic; durable state lives in the jobs table).
func (b *Bus) Publish(jobID int64, ev module.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[jobID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
