package main

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Store holder siste snapshot lock-fritt via atomic.Pointer. Lesere får et konsistent
// snapshot uten mutex på hot read-path; skriver bytter hele snapshotet per tick.
type Store struct {
	snap atomic.Pointer[[]Result]
}

func newStore() *Store {
	s := &Store{}
	empty := []Result{}
	s.snap.Store(&empty)
	return s
}

func (s *Store) set(results []Result) { s.snap.Store(&results) }

func (s *Store) snapshot() []Result {
	p := s.snap.Load()
	out := make([]Result, len(*p))
	copy(out, *p)
	return out
}

// Checker eier targets + en delt http.Client + en Store. Run poller på ticker.
type Checker struct {
	targets  []Target
	interval time.Duration
	timeout  time.Duration
	client   *http.Client
	store    *Store
	record   recordFn // valgfri: kalles etter hvert snapshot (OTEL-metrics)
}

func newChecker(targets []Target, interval, timeout time.Duration) *Checker {
	c := &Checker{
		targets:  targets,
		interval: interval,
		timeout:  timeout,
		client:   newHTTPClient(),
		store:    newStore(),
	}
	// Initial snapshot: alle unknown til første sjekk lander.
	init := make([]Result, len(targets))
	for i, t := range targets {
		init[i] = Result{Name: t.Name, URL: t.URL, Status: StatusUnknown}
	}
	c.store.set(init)
	return c
}

// runOnce sjekker alle targets parallelt og bytter snapshot atomisk når alle er ferdige.
func (c *Checker) runOnce(ctx context.Context) {
	results := make([]Result, len(c.targets))
	var wg sync.WaitGroup
	for i, t := range c.targets {
		wg.Add(1)
		go func(i int, t Target) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()
			results[i] = check(cctx, c.client, t) // hver goroutine skriver sin egen indeks
		}(i, t)
	}
	wg.Wait()
	c.store.set(results)
	if c.record != nil {
		c.record(results)
	}
}

// Run kjører en sjekk umiddelbart, så på ticker, til ctx er done.
func (c *Checker) Run(ctx context.Context) {
	c.runOnce(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runOnce(ctx)
		}
	}
}

func (c *Checker) Snapshot() []Result { return c.store.snapshot() }
