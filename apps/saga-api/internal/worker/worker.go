// Package worker drains the job queue with a dispatcher + bounded goroutine
// pool. The dispatcher owns RequeueStale and per-tier capacity; it acquires a
// tier slot BEFORE claiming, so a claimed job is always progressing and never
// blocks on a semaphore while holding a lease (which would freeze its heartbeat
// and let RequeueStale double-run it). A per-job fence token (owner) guards
// every write, so a rescued-and-reclaimed job's original worker no-ops.
package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/api"
	"saga-api/internal/module"
	"saga-api/internal/queue"
)

const (
	pollInterval = 2 * time.Second
	// heartbeatEvery throttles lease refreshes during token streaming.
	heartbeatEvery = 30 * time.Second
)

// leaseTimeout must exceed the max time a single LLM call - and thus the gap
// between heartbeats during the map phase - can run, so a legitimately-running
// job is never rescued out from under its worker.
func leaseTimeout(chunk time.Duration) time.Duration { return chunk + 5*time.Minute }

// Run starts one dispatcher + a bounded set of job goroutines. A tier slot is
// acquired BEFORE Claim, so a claimed job never blocks on a semaphore while
// holding a lease (which would freeze its heartbeat and let RequeueStale
// double-run it). cloudSlots caps concurrent cloud jobs; local is always 1.
func Run(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus, cloudSlots int) {
	local := make(chan struct{}, 1)
	cloud := make(chan struct{}, cloudSlots)
	var wg sync.WaitGroup
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-t.C:
		}
		if n, err := queue.RequeueStale(ctx, pool, leaseTimeout(deps.ChunkTimeout)); err != nil {
			log.Printf("worker: requeue stale: %v", err)
		} else if n > 0 {
			log.Printf("worker: rescued %d stale job(s)", n)
		}
		dispatch(ctx, pool, deps, bus, local, "local", &wg)
		dispatch(ctx, pool, deps, bus, cloud, "cloud", &wg)
	}
}

func dispatch(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus,
	slots chan struct{}, tier string, wg *sync.WaitGroup) {
	for {
		select {
		case slots <- struct{}{}: // acquire BEFORE claim
		default:
			return // tier full this tick
		}
		owner := uuid.NewString()
		job, err := queue.Claim(ctx, pool, owner, []string{tier})
		if err != nil || job == nil {
			<-slots // nothing to run, release
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			process(ctx, pool, deps, bus, job, owner)
		}()
	}
}

// process runs a single already-claimed job to a terminal state. owner is the
// fence token: every write is guarded by it, so a job rescued and reclaimed by
// another worker mid-run is not double-written by this worker.
func process(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus, job *queue.Job, owner string) {
	log.Printf("worker: job %d (%s) attempt %d owner %s", job.ID, job.Module, job.Attempts, owner)

	mod, ok := module.Get(job.Module)
	if !ok {
		// ponytail: unknown module goes through the normal retry dance and
		// parks as failed after MaxAttempts; add a queue.FailHard if a second
		// permanent-error case ever shows up. This means the first hit can
		// emit a non-terminal "failed" here (unlike the module-error branch
		// below, which distinguishes retrying/failed) - self-corrects once
		// attempts reach MaxAttempts, acceptable since an unknown module
		// never becomes known between retries.
		msg := fmt.Sprintf("unknown module %q", job.Module)
		if err := queue.Fail(ctx, pool, job.ID, msg, owner); err != nil {
			log.Printf("worker: job %d fail: %v", job.ID, err)
			return
		}
		bus.Publish(job.ID, module.Event{Stage: "failed", Detail: msg})
		return
	}

	lastBeat := time.Now()
	emit := func(ev module.Event) {
		bus.Publish(job.ID, ev)
		if ev.Token == "" {
			p := ev.Stage
			if ev.Detail != "" {
				p += ": " + ev.Detail
			}
			if err := queue.SetProgress(ctx, pool, job.ID, p, owner); err == nil {
				lastBeat = time.Now()
			}
		} else if time.Since(lastBeat) > heartbeatEvery {
			if err := queue.SetProgress(ctx, pool, job.ID, "summarizing", owner); err == nil {
				lastBeat = time.Now()
			}
		}
	}

	res, err := mod.Run(ctx, job.Input, deps, emit)
	if err != nil {
		if ferr := queue.Fail(ctx, pool, job.ID, err.Error(), owner); ferr != nil {
			log.Printf("worker: job %d fail: %v", job.ID, ferr)
			return
		}
		// queue.Fail requeues while attempts remain; only the terminal attempt
		// parks the job as failed. Emit a terminal "failed" only then, so the
		// web UI (which closes its SSE stream on "failed") keeps streaming
		// across auto-retries and sees "retrying" instead.
		stage := "retrying"
		if job.Attempts >= queue.MaxAttempts {
			stage = "failed"
		}
		bus.Publish(job.ID, module.Event{Stage: stage, Detail: err.Error()})
		return
	}
	if res.VideoTitle != "" || res.VideoDescription != "" {
		if merr := queue.SetVideoMeta(ctx, pool, job.ID, res.VideoTitle, res.VideoDescription); merr != nil {
			log.Printf("worker: job %d set video meta: %v", job.ID, merr)
		}
	}
	done, err := queue.CompleteOwned(ctx, pool, job.ID, res.Markdown, owner)
	if err != nil {
		log.Printf("worker: job %d complete: %v", job.ID, err)
		return
	}
	if !done {
		// Fenced out: the job was rescued and reclaimed by another worker while
		// we ran. That worker owns the result now; do not publish a stale done.
		log.Printf("worker: job %d completion fenced out (owner %s lost lease)", job.ID, owner)
		return
	}
	bus.Publish(job.ID, module.Event{Stage: "done"})
}
