// Package queue is the Postgres-backed job queue. The jobs table doubles as
// the job history: queued/running rows are the queue, done/failed rows are
// the archive the UI lists.
package queue

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxAttempts = 1 first run + 2 retries (spec).
const MaxAttempts = 3

type Job struct {
	ID                 int64
	Module             string
	Input              []byte
	Status             string
	Attempts           int
	Progress           string
	Error              *string
	ResultMarkdown     *string
	CreatedAt          time.Time
	StartedAt          *time.Time
	FinishedAt         *time.Time
	TranslatedMarkdown *string
	TranslatedLang     *string
	VideoTitle         *string
	VideoDescription   *string
	// Tier routes the job to a capacity pool: "local" (single GPU) or "cloud".
	Tier string
	// LeaseOwner is the fence token of the worker currently holding the lease.
	// A rescued-and-reclaimed job's original worker no-ops on write because its
	// owner no longer matches. NULL while queued/unclaimed.
	LeaseOwner *string
}

const jobCols = `id, module, input, status, attempts, progress, error,
	result_markdown, created_at, started_at, finished_at, translated_markdown, translated_lang,
	video_title, video_description, tier, lease_owner`

func scanJob(row pgx.Row) (*Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Module, &j.Input, &j.Status, &j.Attempts, &j.Progress,
		&j.Error, &j.ResultMarkdown, &j.CreatedAt, &j.StartedAt, &j.FinishedAt,
		&j.TranslatedMarkdown, &j.TranslatedLang, &j.VideoTitle, &j.VideoDescription,
		&j.Tier, &j.LeaseOwner)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func Enqueue(ctx context.Context, pool *pgxpool.Pool, module string, input []byte, tier string) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO jobs (module, input, tier) VALUES ($1, $2, $3) RETURNING id`,
		module, input, tier).Scan(&id)
	return id, err
}

// Claim atomically picks the oldest queued job whose tier is one of tiers,
// stamping lease_owner = owner. SKIP LOCKED makes concurrent claimers (the
// dispatcher pool) never double-claim. Returns nil, nil on empty.
func Claim(ctx context.Context, pool *pgxpool.Pool, owner string, tiers []string) (*Job, error) {
	j, err := scanJob(pool.QueryRow(ctx, `
		UPDATE jobs SET status = 'running', attempts = attempts + 1,
			started_at = now(), lease_at = now(), lease_owner = $1
		WHERE id = (
			SELECT id FROM jobs WHERE status = 'queued' AND tier = ANY($2)
			ORDER BY id LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+jobCols, owner, tiers))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// SetProgress updates the human-readable progress AND refreshes the lease,
// so it doubles as the worker heartbeat. Fenced by owner: a worker that lost
// its lease (rescued + reclaimed) no-ops instead of resurrecting a dead lease.
func SetProgress(ctx context.Context, pool *pgxpool.Pool, id int64, progress, owner string) error {
	_, err := pool.Exec(ctx,
		`UPDATE jobs SET progress = $2, lease_at = now() WHERE id = $1 AND lease_owner = $3`,
		id, progress, owner)
	return err
}

// CompleteOwned marks the job done, fenced by owner. It returns true only when
// it affected the row: false means the caller was fenced out (the job was
// rescued and reclaimed by another worker), so the caller must NOT publish a
// stale terminal event. This is the guard against a double-run writing twice.
func CompleteOwned(ctx context.Context, pool *pgxpool.Pool, id int64, markdown, owner string) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE jobs SET status = 'done', result_markdown = $2,
			finished_at = now(), error = NULL, progress = ''
		WHERE id = $1 AND status = 'running' AND lease_owner = $3`, id, markdown, owner)
	return tag.RowsAffected() == 1, err
}

// Fail requeues the job while attempts remain, otherwise parks it as failed.
// Fenced by owner, so a fenced-out worker cannot rewrite a job another worker
// now owns.
func Fail(ctx context.Context, pool *pgxpool.Pool, id int64, msg, owner string) error {
	_, err := pool.Exec(ctx, `
		UPDATE jobs SET
			status = CASE WHEN attempts >= $2 THEN 'failed' ELSE 'queued' END,
			error = $3,
			finished_at = CASE WHEN attempts >= $2 THEN now() ELSE NULL END,
			lease_at = NULL
		WHERE id = $1 AND status = 'running' AND lease_owner = $4`, id, MaxAttempts, msg, owner)
	return err
}

// RequeueStale rescues jobs whose worker died mid-run (pod restart).
// Called by the worker loop on every tick.
func RequeueStale(ctx context.Context, pool *pgxpool.Pool, leaseTimeout time.Duration) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE jobs SET
			status = CASE WHEN attempts >= $1 THEN 'failed' ELSE 'queued' END,
			error = CASE WHEN attempts >= $1 THEN 'lease expired (worker died)' ELSE error END,
			finished_at = CASE WHEN attempts >= $1 THEN now() ELSE finished_at END,
			lease_at = NULL,
			lease_owner = NULL
		WHERE status = 'running' AND lease_at < now() - make_interval(secs => $2)`,
		MaxAttempts, leaseTimeout.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func Get(ctx context.Context, pool *pgxpool.Pool, id int64) (*Job, error) {
	j, err := scanJob(pool.QueryRow(ctx,
		`SELECT `+jobCols+` FROM jobs WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func List(ctx context.Context, pool *pgxpool.Pool, limit int) ([]Job, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+jobCols+` FROM jobs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, rows.Err()
}

// SetVideoMeta persists the video's title + description on the job, so the
// front-page list can show a human-readable title instead of the raw URL.
func SetVideoMeta(ctx context.Context, pool *pgxpool.Pool, id int64, title, description string) error {
	_, err := pool.Exec(ctx,
		`UPDATE jobs SET video_title = $2, video_description = $3 WHERE id = $1`,
		id, title, description)
	return err
}

// SetTranslation caches a translated summary on the job.
func SetTranslation(ctx context.Context, pool *pgxpool.Pool, id int64, lang, markdown string) error {
	_, err := pool.Exec(ctx,
		`UPDATE jobs SET translated_markdown = $2, translated_lang = $3 WHERE id = $1`,
		id, markdown, lang)
	return err
}

// Retry re-arms a failed job from the UI. Returns false if the job is not failed.
func Retry(ctx context.Context, pool *pgxpool.Pool, id int64) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE jobs SET status = 'queued', attempts = 0, error = NULL,
			progress = '', result_markdown = NULL, finished_at = NULL, lease_owner = NULL
		WHERE id = $1 AND status = 'failed'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
