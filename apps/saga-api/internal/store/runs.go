package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Run is one row in job_runs: a single successful model execution of a job,
// captured for the durable eval store.
type Run struct {
	JobID              int64
	RunGroupID         string
	TranscriptSha256   string
	Model              string
	ModelBuild         string
	Tier               string
	PromptVersion      string
	TargetLang         string
	SummarizeLang      string
	TranslateModel     string
	Reproducible       bool
	Temperature        float64
	Seed               int
	NumCtx             int
	InputTokens        int
	OutputTokens       int
	GenTokS            float64
	SummarizeMs        int
	TranslateMs        int
	TotalMs            int
	SummarizeCostUSD   float64
	TranslateCostUSD   float64
	ChunkCount         int
	ResultMarkdown     string
	TranslatedMarkdown string
	EvalSetTag         string
	TraceID            string
}

// InsertRun writes a Run inside the caller's transaction (the Complete
// transaction, added in a later task).
func InsertRun(ctx context.Context, tx pgx.Tx, r Run) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO job_runs (job_id, run_group_id, transcript_sha256, model, model_build,
			tier, prompt_version, target_lang, summarize_lang, translate_model, reproducible,
			temperature, seed, num_ctx, input_tokens, output_tokens, gen_tok_s,
			summarize_ms, translate_ms, total_ms, summarize_cost_usd, translate_cost_usd,
			chunk_count, result_markdown, translated_markdown, eval_set_tag, trace_id)
		VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''),$11,
			$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,NULLIF($24,''),NULLIF($25,''),NULLIF($26,''),NULLIF($27,''))`,
		r.JobID, r.RunGroupID, r.TranscriptSha256, r.Model, r.ModelBuild,
		r.Tier, r.PromptVersion, r.TargetLang, r.SummarizeLang, r.TranslateModel, r.Reproducible,
		r.Temperature, r.Seed, r.NumCtx, r.InputTokens, r.OutputTokens, r.GenTokS,
		r.SummarizeMs, r.TranslateMs, r.TotalMs, r.SummarizeCostUSD, r.TranslateCostUSD,
		r.ChunkCount, r.ResultMarkdown, r.TranslatedMarkdown, r.EvalSetTag, r.TraceID)
	return err
}
