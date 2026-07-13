package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"saga-api/internal/catalog"
	"saga-api/internal/llm"
	"saga-api/internal/module"
	"saga-api/internal/queue"
	"saga-api/internal/store"
	"saga-api/internal/summarize"
)

type server struct {
	pool           *pgxpool.Pool
	bus            *Bus
	llm            llm.Provider
	translateModel string
}

// New wires the HTTP API. translateModel is the model the interactive
// /translate endpoint uses - it must be the same configured model the
// auto-translate pipeline uses (cfg.TranslateModel, boot-resolved in main.go
// including its no-API-key fallback), so the two paths never diverge in
// translation quality.
func New(pool *pgxpool.Pool, bus *Bus, llmClient llm.Provider, version, translateModel string) http.Handler {
	s := &server{pool: pool, bus: bus, llm: llmClient, translateModel: translateModel}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok %s", version)
	})
	mux.HandleFunc("POST /api/jobs", s.createJob)
	mux.HandleFunc("GET /api/jobs", s.listJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.getJob)
	mux.HandleFunc("POST /api/jobs/{id}/retry", s.retryJob)
	mux.HandleFunc("POST /api/jobs/{id}/rerun", s.rerunJob)
	mux.HandleFunc("POST /api/jobs/{id}/translate", s.translate)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"models": catalog.All()})
	})
	return mux
}

// jobJSON is the wire shape for a job. Result only rides along when full=true.
func jobJSON(j *queue.Job, full bool) map[string]any {
	m := map[string]any{
		"id":          j.ID,
		"module":      j.Module,
		"input":       json.RawMessage(j.Input),
		"status":      j.Status,
		"attempts":    j.Attempts,
		"progress":    j.Progress,
		"error":       j.Error,
		"created_at":  j.CreatedAt.Format(time.RFC3339),
		"video_title": j.VideoTitle,
	}
	if full {
		m["result_markdown"] = j.ResultMarkdown
		m["translated_markdown"] = j.TranslatedMarkdown
		m["translated_lang"] = j.TranslatedLang
		m["video_description"] = j.VideoDescription
	}
	return m
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *server) createJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Module string          `json:"module"`
		Input  json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if _, ok := module.Get(req.Module); !ok {
		http.Error(w, fmt.Sprintf("unknown module %q, have %v", req.Module, module.Names()),
			http.StatusBadRequest)
		return
	}
	if len(req.Input) == 0 {
		req.Input = json.RawMessage(`{}`)
	}
	// Route to a capacity tier by the requested model. Default "local" when the
	// input carries no model or an unknown one (the module also defaults there).
	tier := "local"
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(req.Input, &probe); err == nil && probe.Model != "" {
		if m, ok := catalog.Get(probe.Model); ok {
			tier = m.Tier
		}
	}
	id, err := queue.Enqueue(r.Context(), s.pool, req.Module, req.Input, tier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := queue.List(r.Context(), s.pool, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(jobs))
	for i := range jobs {
		out = append(out, jobJSON(&jobs[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *server) jobFromPath(w http.ResponseWriter, r *http.Request) *queue.Job {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return nil
	}
	job, err := queue.Get(r.Context(), s.pool, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if job == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil
	}
	return job
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	if job := s.jobFromPath(w, r); job != nil {
		writeJSON(w, http.StatusOK, jobJSON(job, true))
	}
}

func (s *server) retryJob(w http.ResponseWriter, r *http.Request) {
	job := s.jobFromPath(w, r)
	if job == nil {
		return
	}
	ok, err := queue.Retry(r.Context(), s.pool, job.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "job is not failed", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rerunJob replays a job on a different model, reusing the transcript its
// most recent run recorded (instead of re-fetching), so a fair A/B between
// models sees byte-identical input. The replay's job_runs row carries
// run_group_id = the original job's id, so the eval store can group them.
func (s *server) rerunJob(w http.ResponseWriter, r *http.Request) {
	job := s.jobFromPath(w, r)
	if job == nil {
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		http.Error(w, "model required", http.StatusBadRequest)
		return
	}
	sha, err := store.LatestRunTranscript(r.Context(), s.pool, job.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sha == "" {
		http.Error(w, "job has no recorded transcript to replay", http.StatusConflict)
		return
	}
	var orig struct {
		URL  string `json:"url"`
		Lang string `json:"lang"`
	}
	if err := json.Unmarshal(job.Input, &orig); err != nil {
		http.Error(w, "cannot parse original job input", http.StatusInternalServerError)
		return
	}
	newInput, err := json.Marshal(map[string]string{
		"url":            orig.URL,
		"lang":           orig.Lang,
		"model":          req.Model,
		"transcript_sha": sha,
		"run_group":      strconv.FormatInt(job.ID, 10),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tier := "local"
	if m, ok := catalog.Get(req.Model); ok {
		tier = m.Tier
	}
	id, err := queue.Enqueue(r.Context(), s.pool, job.Module, newInput, tier)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

// translate runs a done job's summary through the LLM into the target language,
// caches it on the job, and returns it. Uses the shared llm chokepoint (the
// n=1 semaphore serializes it against the worker). ponytail: non-streaming - a
// summary is short (~10-20s); upgrade path is an SSE token stream if it drags.
func (s *server) translate(w http.ResponseWriter, r *http.Request) {
	job := s.jobFromPath(w, r)
	if job == nil {
		return
	}
	var req struct {
		Lang string `json:"lang"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Lang == "" {
		http.Error(w, "lang required", http.StatusBadRequest)
		return
	}
	if job.Status != "done" || job.ResultMarkdown == nil {
		http.Error(w, "job is not done", http.StatusConflict)
		return
	}
	// Already translated to this lang -> return the cache.
	if job.TranslatedLang != nil && *job.TranslatedLang == req.Lang && job.TranslatedMarkdown != nil {
		writeJSON(w, http.StatusOK, map[string]any{"translated_markdown": *job.TranslatedMarkdown})
		return
	}
	res, err := s.llm.Chat(r.Context(), s.translateModel, summarize.TranslatePrompt(req.Lang, *job.ResultMarkdown), llm.ChatOptions{Temperature: llm.DefaultTemperature}, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	md := summarize.CleanMath(res.Text)
	if err := queue.SetTranslation(r.Context(), s.pool, job.ID, req.Lang, md); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"translated_markdown": md})
}

// events streams job progress as SSE: one snapshot event, then live bus
// events until a terminal stage (done/failed) or client disconnect.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("job"), 10, 64)
	if err != nil {
		http.Error(w, "job query param required", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// subscribe BEFORE snapshot so no event falls in the gap
	ch, cancel := s.bus.Subscribe(id)
	defer cancel()

	job, err := queue.Get(r.Context(), s.pool, id)
	if err != nil || job == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	send(jobJSON(job, true)) // snapshot
	if job.Status == "done" || job.Status == "failed" {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			send(ev)
			if ev.Stage == "done" || ev.Stage == "failed" {
				return
			}
		}
	}
}
