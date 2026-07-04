import { createFileRoute, useRouter } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useRef, useState } from "react"
import type { Job, ProgressEvent } from "../types"
import { getJob } from "../api"
import { Shell, StatusPill } from "../ui"
import { Markdown } from "../markdown"

// ponytail: Job.input is Record<string, unknown> (arbitrary job-module payload);
// createServerFn's compile-time output-serializability check can't prove an
// index signature of `unknown` is serializable even though it round-trips
// fine as JSON at runtime. `strict: { output: false }` opts out of that
// check for this response rather than loosening Job.input in types.ts (Task
// 2). Upgrade path: a recursive JsonValue type for Job.input would let this
// (and index.tsx's fetchJobs wrapper) typecheck cleanly under the default strict mode.
const fetchJob = createServerFn({ method: "GET", strict: { output: false } })
  .validator((id: unknown): number => Number(id))
  .handler(async ({ data: id }): Promise<Job | null> => {
    return getJob(id)
  })

export const Route = createFileRoute("/jobs/$id")({
  component: JobPage,
  loader: ({ params }) => fetchJob({ data: Number(params.id) }),
})

function isTerminal(s: Job["status"]): boolean {
  return s === "done" || s === "failed"
}

function JobPage() {
  const initial = Route.useLoaderData()
  const { id } = Route.useParams()
  const router = useRouter()

  const [job, setJob] = useState<Job | null>(initial)
  const [live, setLive] = useState<string>("") // rolling progress line
  const [tokens, setTokens] = useState<string>("") // streamed reduce tokens
  const tokensRef = useRef("")

  // Open an SSE stream while the job is not terminal. The browser reaches
  // /api/events directly (same origin via ingress). On terminal, re-fetch the
  // full job to get result_markdown, then close.
  useEffect(() => {
    if (!job || isTerminal(job.status)) return
    const es = new EventSource(`/api/events?job=${id}`)
    let snapshotSeen = false
    es.onmessage = (e) => {
      const data = JSON.parse(e.data) as Job | ProgressEvent
      // First event is a full Job snapshot; the rest are ProgressEvents.
      if (!snapshotSeen && "status" in data) {
        snapshotSeen = true
        setJob(data as Job)
        return
      }
      const ev = data as ProgressEvent
      if (ev.token) {
        tokensRef.current += ev.token
        setTokens(tokensRef.current)
      } else if (ev.stage) {
        setLive(ev.detail ? `${ev.stage}: ${ev.detail}` : ev.stage)
      }
      if (ev.stage === "done" || ev.stage === "failed") {
        es.close()
        // pull the final job (result_markdown / error) via the loader path
        router.invalidate().then(() => {
          getJobClient(Number(id)).then((j) => j && setJob(j))
        })
      }
    }
    es.onerror = () => es.close()
    return () => es.close()
  }, [id, job, router])

  if (!job) {
    return (
      <Shell>
        <p style={{ color: "#b00" }}>Job {id} not found.</p>
      </Shell>
    )
  }

  const title = typeof job.input.url === "string" ? job.input.url : `job ${job.id}`

  return (
    <Shell>
      <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 16 }}>
        <StatusPill status={job.status} />
        <a href={title} target="_blank" rel="noreferrer" style={{ color: "#0969da", wordBreak: "break-all" }}>
          {title}
        </a>
      </div>

      {!isTerminal(job.status) && (
        <div style={{ marginBottom: 16 }}>
          <p style={{ color: "#666" }}>{live || job.progress || job.status}</p>
          {tokens && (
            <pre style={{ whiteSpace: "pre-wrap", background: "#f6f8fa", padding: 12, borderRadius: 8 }}>{tokens}</pre>
          )}
        </div>
      )}

      {job.status === "failed" && (
        <div style={{ marginBottom: 16 }}>
          <p style={{ color: "#b00" }}>{job.error ?? "failed"}</p>
          <RetryButton id={job.id} onRetried={() => router.invalidate()} />
        </div>
      )}

      {job.status === "done" && job.result_markdown && <Markdown source={job.result_markdown} />}
    </Shell>
  )
}

// Client-side fetch of the full job (browser -> same-origin /api).
async function getJobClient(id: number): Promise<Job | null> {
  const res = await fetch(`/api/jobs/${id}`)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
  return (await res.json()) as Job
}

function RetryButton({ id, onRetried }: { id: number; onRetried: () => void }) {
  const [busy, setBusy] = useState(false)
  return (
    <button
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        const res = await fetch(`/api/jobs/${id}/retry`, { method: "POST" })
        setBusy(false)
        if (res.ok) onRetried()
      }}
      style={{ padding: "8px 20px", fontWeight: 600 }}
    >
      {busy ? "Retrying..." : "Retry"}
    </button>
  )
}
