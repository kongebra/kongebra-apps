// Package worker drains the job queue with a dispatcher + bounded goroutine
// pool. The dispatcher owns RequeueStale and per-tier capacity; it acquires a
// tier slot BEFORE claiming, so a claimed job is always progressing and never
// blocks on a semaphore while holding a lease (which would freeze its heartbeat
// and let RequeueStale double-run it). A per-job fence token (owner) guards
// every write, so a rescued-and-reclaimed job's original worker no-ops.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"saga-api/internal/api"
	"saga-api/internal/catalog"
	"saga-api/internal/module"
	"saga-api/internal/obs"
	"saga-api/internal/queue"
	"saga-api/internal/store"
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
		if err != nil {
			log.Printf("worker: claim: %v", err)
		}
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
//
// A NEW ROOT span is started here (not derived from ctx) because ctx is the
// worker's long-lived shutdown-aware context shared across every job, not a
// per-request context - there is no meaningful parent trace to attach to.
// ponytail: a single job span carries all attributes (job.id, tier, model,
// timings); per-stage child spans (fetch/summarize/translate) are deferred -
// add them if per-stage latency ever needs isolating in Tempo.
func process(ctx context.Context, pool *pgxpool.Pool, deps module.Deps, bus *api.Bus, job *queue.Job, owner string) {
	log.Printf("worker: job %d (%s) attempt %d owner %s", job.ID, job.Module, job.Attempts, owner)

	claimedModel := modelFromInput(job.Input)
	spanCtx, span := obs.Tracer.Start(context.Background(), "job", trace.WithNewRoot(),
		trace.WithAttributes(
			attribute.Int64("job.id", job.ID),
			attribute.String("tier", job.Tier),
			attribute.String("model", claimedModel),
		))
	defer span.End()
	traceID := span.SpanContext().TraceID().String()

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
		span.SetStatus(codes.Error, msg)
		if err := queue.Fail(ctx, pool, job.ID, msg, owner); err != nil {
			log.Printf("worker: job %d fail: %v", job.ID, err)
			return
		}
		recordJobsTotal(spanCtx, job.Tier, claimedModel, "failed")
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
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		failAndPublish(ctx, spanCtx, pool, bus, job, owner, claimedModel, err.Error())
		return
	}
	span.SetAttributes(attribute.String("model", res.Run.Model))
	if res.VideoTitle != "" || res.VideoDescription != "" {
		if merr := queue.SetVideoMeta(ctx, pool, job.ID, res.VideoTitle, res.VideoDescription); merr != nil {
			log.Printf("worker: job %d set video meta: %v", job.ID, merr)
		}
	}
	// Transcripts are content-addressed and idempotent (INSERT ... ON CONFLICT
	// DO NOTHING), so saving one is safe outside the Complete transaction: a
	// retry never creates a duplicate row, and job_runs.transcript_sha256 only
	// needs the row to exist by commit time, not to commit atomically with it.
	if terr := store.SaveTranscript(ctx, pool, res.Transcript); terr != nil {
		span.RecordError(terr)
		span.SetStatus(codes.Error, terr.Error())
		failAndPublish(ctx, spanCtx, pool, bus, job, owner, res.Run.Model, terr.Error())
		return
	}
	done, err := completeWithRun(ctx, pool, job, res, owner, traceID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		failAndPublish(ctx, spanCtx, pool, bus, job, owner, res.Run.Model, err.Error())
		return
	}
	if !done {
		// Fenced out: the job was rescued and reclaimed by another worker while
		// we ran. That worker owns the result now; do not publish a stale done,
		// and completeWithRun already rolled back our job_runs INSERT so no
		// duplicate eval-store row is left behind.
		log.Printf("worker: job %d completion fenced out (owner %s lost lease)", job.ID, owner)
		return
	}
	recordJobMetrics(spanCtx, job.Tier, res.Run, res.Transcript)
	recordJobsTotal(spanCtx, job.Tier, res.Run.Model, "done")
	bus.Publish(job.ID, module.Event{Stage: "done"})
}

// modelFromInput best-effort extracts a "model" field from a job's raw input
// JSON, for the span attribute recorded before the module resolves its
// default model. Modules without a "model" input field, or malformed input,
// yield "" - not an error, since this is only ever a best-effort attribute.
func modelFromInput(input []byte) string {
	var v struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(input, &v); err != nil {
		return ""
	}
	return v.Model
}

// recordJobsTotal records one terminal job outcome. status is "done" or
// "failed" - non-terminal retries are not counted here (queue.Fail's retry
// path calls neither this nor recordJobMetrics).
func recordJobsTotal(ctx context.Context, tier, model, status string) {
	obs.Met.JobsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tier", tier),
		attribute.String("model", model),
		attribute.String("status", status),
	))
}

// recordJobMetrics records the per-job duration/throughput/cost histograms
// on a successful completion.
func recordJobMetrics(ctx context.Context, tier string, run store.Run, transcript store.Transcript) {
	attrs := metric.WithAttributes(
		attribute.String("model", run.Model),
		attribute.String("tier", tier),
		attribute.String("status", "done"),
	)
	obs.Met.SummaryDuration.Record(ctx, float64(run.SummarizeMs), attrs)
	obs.Met.TokensPerSecond.Record(ctx, run.GenTokS, attrs)
	if run.TranslateMs > 0 {
		obs.Met.TranslateDuration.Record(ctx, float64(run.TranslateMs), attrs)
	}
	obs.Met.TranscriptChars.Record(ctx, float64(transcript.Chars), attrs)
	obs.Met.Chunks.Record(ctx, float64(run.ChunkCount), attrs)
	obs.Met.CostUSD.Record(ctx, run.SummarizeCostUSD+run.TranslateCostUSD, attrs)
}

// failAndPublish requeues (or terminally fails) the job and publishes the
// matching SSE event. queue.Fail requeues while attempts remain; only the
// terminal attempt parks the job as failed. A terminal "failed" is emitted
// only then, so the web UI (which closes its SSE stream on "failed") keeps
// streaming across auto-retries and sees "retrying" instead. metricCtx is
// the job span's context (see process); jobs_total is recorded only for the
// terminal outcome, not for each in-progress retry.
func failAndPublish(ctx, metricCtx context.Context, pool *pgxpool.Pool, bus *api.Bus, job *queue.Job, owner, model, msg string) {
	if err := queue.Fail(ctx, pool, job.ID, msg, owner); err != nil {
		log.Printf("worker: job %d fail: %v", job.ID, err)
		return
	}
	stage := "retrying"
	if job.Attempts >= queue.MaxAttempts {
		stage = "failed"
		recordJobsTotal(metricCtx, job.Tier, model, stage)
	}
	bus.Publish(job.ID, module.Event{Stage: stage, Detail: msg})
}

// completeWithRun writes the job_runs record and completes the job in ONE
// transaction: either both land, or neither does. job_runs is written only
// on this path (success), so retries never leave noise rows in the eval
// store, and a fenced-out completion (done=false) rolls back the INSERT too
// - a worker that lost its lease must not add a duplicate run row for a job
// another worker is now (or already has) running.
func completeWithRun(ctx context.Context, pool *pgxpool.Pool, job *queue.Job, res module.Result, owner, traceID string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	// Backstop for the commit-failure path: if tx.Commit below fails, this
	// releases the pooled connection. A Rollback after a successful Commit is
	// a harmless no-op (returns an already-closed-tx error we ignore).
	defer tx.Rollback(ctx)

	run := res.Run
	run.JobID = job.ID
	run.Tier = job.Tier
	run.TraceID = traceID
	m, found := catalog.Get(run.Model)
	if !found {
		log.Printf("worker: job %d: model %q not in catalog, recording zero cost/non-reproducible run", job.ID, run.Model)
	}
	run.SummarizeCostUSD = m.PriceInPerMtok*float64(run.InputTokens)/1e6 + m.PriceOutPerMtok*float64(run.OutputTokens)/1e6
	run.Reproducible = m.Tier == "local"

	done := false
	if err = store.InsertRun(ctx, tx, run); err == nil {
		done, err = queue.CompleteOwnedTx(ctx, tx, job.ID, res.Markdown, owner)
	}

	if err != nil || !done {
		if rerr := tx.Rollback(ctx); rerr != nil {
			log.Printf("worker: job %d rollback: %v", job.ID, rerr)
		}
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
