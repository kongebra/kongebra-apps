import { createFileRoute } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useRef, useState } from "react"
import type { Job, ProgressEvent } from "../types"
import { getJob } from "../api"
import { Shell, StatusPill } from "../ui"
import { Markdown } from "../markdown"
import { estimateEta } from "@/lib/eta"
import { VideoCard } from "@/components/video-card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"

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

  const [job, setJob] = useState<Job | null>(initial)
  const [live, setLive] = useState<string>("") // rolling progress line
  const [tokens, setTokens] = useState<string>("") // streamed reduce tokens
  const tokensRef = useRef("")
  const [eta, setEta] = useState<string | null>(null)
  const chunkTiming = useRef<{ start: number; startIdx: number }>({ start: 0, startIdx: 0 })
  // Bumping streamKey (re)opens the SSE stream: on mount, and again after a
  // Retry moves a terminal job back to running.
  const [streamKey, setStreamKey] = useState(0)
  const jobRef = useRef(job)
  jobRef.current = job

  useEffect(() => {
    // Guard on the CURRENT job via a ref (not `job` in deps): depending on
    // `job` would reconnect on every snapshot (the first SSE message is a full
    // Job) -> infinite loop. streamKey is the explicit reopen trigger.
    const cur = jobRef.current
    if (!cur || isTerminal(cur.status)) return
    tokensRef.current = ""
    setTokens("")
    chunkTiming.current = { start: 0, startIdx: 0 }
    setEta(null)
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
        const m = ev.detail?.match(/chunk (\d+)\/(\d+)/)
        if (m) {
          const i = Number(m[1]), n = Number(m[2]), now = Date.now()
          if (chunkTiming.current.start === 0) chunkTiming.current = { start: now, startIdx: i }
          else setEta(estimateEta(chunkTiming.current.start, now, i - chunkTiming.current.startIdx, n - i))
        }
      }
      if (ev.stage === "done" || ev.stage === "failed") {
        es.close()
        // pull the final job (result_markdown / error); if it came back
        // non-terminal (a requeue), reopen the stream for the next attempt.
        getJobClient(Number(id)).then((j) => {
          if (!j) return
          setJob(j)
          if (!isTerminal(j.status)) setStreamKey((k) => k + 1)
        })
      }
    }
    es.onerror = () => es.close()
    return () => es.close()
  }, [id, streamKey])

  // After a successful retry: reflect running immediately + reopen the stream.
  async function onRetried() {
    const j = await getJobClient(Number(id))
    if (!j) return
    setLive("")
    setJob(j)
    setStreamKey((k) => k + 1)
  }

  if (!job) {
    return (
      <Shell>
        <p className="text-destructive">Job {id} not found.</p>
      </Shell>
    )
  }

  const title = typeof job.input.url === "string" ? job.input.url : `job ${job.id}`
  const url = typeof job.input.url === "string" ? job.input.url : null
  const safeHref = url && /^https?:\/\//i.test(url) ? url : undefined

  return (
    <Shell>
      {typeof job.input.url === "string" && <VideoCard url={job.input.url} />}

      <div className="mb-4 flex items-center gap-3">
        <StatusPill status={job.status} />
        {safeHref ? (
          <a href={safeHref} target="_blank" rel="noreferrer" className="break-all text-primary underline-offset-4 hover:underline">
            {title}
          </a>
        ) : (
          <span className="break-all">{title}</span>
        )}
      </div>

      {!isTerminal(job.status) && (
        <div className="mb-4">
          <p className="text-muted-foreground">{live || job.progress || job.status}{eta ? ` - ${eta} left` : ""}</p>
          {tokens && <pre className="whitespace-pre-wrap rounded-lg bg-muted p-3 text-sm">{tokens}</pre>}
        </div>
      )}

      {job.status === "failed" && (
        <div className="mb-4">
          <p className="text-destructive">{job.error ?? "failed"}</p>
          <RetryButton id={job.id} onRetried={onRetried} />
        </div>
      )}

      {job.status === "done" && job.result_markdown && <SummaryView job={job} />}
    </Shell>
  )
}

function SummaryView({ job }: { job: Job }) {
  const [showNo, setShowNo] = useState(false)
  const [translated, setTranslated] = useState<string | null>(job.translated_markdown ?? null)
  const [loading, setLoading] = useState(false)

  async function toNorwegian() {
    if (translated) {
      setShowNo(true)
      return
    }
    setLoading(true)
    const res = await fetch(`/api/jobs/${job.id}/translate`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ lang: "no" }),
    })
    setLoading(false)
    if (res.ok) {
      const { translated_markdown } = (await res.json()) as { translated_markdown: string }
      setTranslated(translated_markdown)
      setShowNo(true)
    }
  }

  const body = showNo && translated ? translated : job.result_markdown!
  return (
    <div>
      <div className="mb-4 flex gap-2">
        <Button variant={showNo ? "outline" : "default"} size="sm" onClick={() => setShowNo(false)}>
          English
        </Button>
        <Button variant={showNo ? "default" : "outline"} size="sm" onClick={toNorwegian} disabled={loading}>
          {loading ? "Translating..." : "Norsk"}
        </Button>
      </div>
      {loading ? (
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      ) : (
        <Markdown source={body} />
      )}
    </div>
  )
}

// Client-side fetch of the full job (browser -> same-origin /api).
async function getJobClient(id: number): Promise<Job | null> {
  const res = await fetch(`/api/jobs/${id}`)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
  return (await res.json()) as Job
}

function RetryButton({ id, onRetried }: { id: number; onRetried: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  return (
    <button
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        const res = await fetch(`/api/jobs/${id}/retry`, { method: "POST" })
        if (res.ok) await onRetried()
        setBusy(false)
      }}
      className="rounded-md bg-primary px-5 py-2 font-semibold text-primary-foreground disabled:opacity-50"
    >
      {busy ? "Retrying..." : "Retry"}
    </button>
  )
}
