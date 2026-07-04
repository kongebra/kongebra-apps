// Package worker drains the job queue. One goroutine in v1 (one GPU; the
// llm semaphore would serialize more workers anyway).
package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/api"
	"saga-api/internal/module"
	"saga-api/internal/queue"
)

const (
	pollInterval = 2 * time.Second
	leaseTimeout = 10 * time.Minute
	// heartbeatEvery throttles lease refreshes during token streaming.
	heartbeatEvery = 30 * time.Second
)

// Run blocks until ctx is cancelled.
func Run(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n, err := queue.RequeueStale(ctx, pool, leaseTimeout); err != nil {
			log.Printf("worker: requeue stale: %v", err)
		} else if n > 0 {
			log.Printf("worker: rescued %d stale job(s)", n)
		}
		for {
			worked, err := ProcessOne(ctx, pool, deps, bus)
			if err != nil {
				log.Printf("worker: %v", err)
				break
			}
			if !worked {
				break
			}
		}
	}
}

// ProcessOne claims and runs a single job. Returns false when the queue is empty.
func ProcessOne(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus) (bool, error) {
	job, err := queue.Claim(ctx, pool)
	if err != nil || job == nil {
		return false, err
	}
	log.Printf("worker: job %d (%s) attempt %d", job.ID, job.Module, job.Attempts)

	mod, ok := module.Get(job.Module)
	if !ok {
		// ponytail: unknown module goes through the normal retry dance and
		// parks as failed after MaxAttempts; add a queue.FailHard if a second
		// permanent-error case ever shows up.
		msg := fmt.Sprintf("unknown module %q", job.Module)
		if err := queue.Fail(ctx, pool, job.ID, msg); err != nil {
			return true, err
		}
		bus.Publish(job.ID, module.Event{Stage: "failed", Detail: msg})
		return true, nil
	}

	lastBeat := time.Now()
	emit := func(ev module.Event) {
		bus.Publish(job.ID, ev)
		if ev.Token == "" {
			p := ev.Stage
			if ev.Detail != "" {
				p += ": " + ev.Detail
			}
			if err := queue.SetProgress(ctx, pool, job.ID, p); err == nil {
				lastBeat = time.Now()
			}
		} else if time.Since(lastBeat) > heartbeatEvery {
			if err := queue.SetProgress(ctx, pool, job.ID, "summarizing"); err == nil {
				lastBeat = time.Now()
			}
		}
	}

	md, err := mod.Run(ctx, job.Input, deps, emit)
	if err != nil {
		if ferr := queue.Fail(ctx, pool, job.ID, err.Error()); ferr != nil {
			return true, ferr
		}
		bus.Publish(job.ID, module.Event{Stage: "failed", Detail: err.Error()})
		return true, nil
	}
	if err := queue.Complete(ctx, pool, job.ID, md); err != nil {
		return true, err
	}
	bus.Publish(job.ID, module.Event{Stage: "done"})
	return true, nil
}
