export type JobStatus = "queued" | "running" | "done" | "failed"

export interface Job {
  id: number
  module: string
  input: Record<string, unknown>
  status: JobStatus
  attempts: number
  progress: string
  error: string | null
  created_at: string
  // present only on the single-job endpoint, not in the list
  result_markdown?: string | null
}

// SSE event shape from GET /api/events (after the initial snapshot).
export interface ProgressEvent {
  stage: string
  detail?: string
  token?: string
}

export interface YtSummaryInput {
  url: string
  lang: "no" | "en"
  model: string
}

export interface NewJobResponse {
  id: number
}
