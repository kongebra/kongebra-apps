import { createFileRoute, useRouter, useNavigate } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { Link } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import type { Job, NewJobResponse } from "../types"
import { listJobs } from "../api"
import { Shell, StatusPill } from "../ui"

// SSR + client-refresh data path for the job list (server-side fetch to saga-api).
// ponytail: Job.input is Record<string, unknown> (arbitrary job-module payload);
// createServerFn's compile-time output-serializability check can't prove an
// index signature of `unknown` is serializable even though it round-trips
// fine as JSON at runtime. `strict: { output: false }` opts out of that
// check for this response rather than loosening Job.input in types.ts (Task
// 2). Upgrade path: a recursive JsonValue type for Job.input would let this
// (and Task 4's getJob wrapper) typecheck cleanly under the default strict mode.
const fetchJobs = createServerFn({ method: "GET", strict: { output: false } }).handler(
  async (): Promise<Job[]> => {
    return listJobs()
  },
)

export const Route = createFileRoute("/")({
  component: IndexPage,
  loader: () => fetchJobs(),
  errorComponent: () => (
    <Shell>
      <p style={{ color: "#b00" }}>Cannot reach saga-api.</p>
    </Shell>
  ),
})

const MODELS = ["gemma4:e4b", "qwen3.5:9b"]

function IndexPage() {
  const jobs = Route.useLoaderData()
  const router = useRouter()
  const navigate = useNavigate()

  const [url, setUrl] = useState("")
  const [lang, setLang] = useState<"no" | "en">("no")
  const [model, setModel] = useState(MODELS[0])
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  // Client auto-refresh of the list (same server-fn, single data path).
  useEffect(() => {
    const t = setInterval(() => router.invalidate(), 5_000)
    return () => clearInterval(t)
  }, [router])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setErr(null)
    try {
      // Browser -> same-origin /api -> saga-api (ingress routes /api).
      const res = await fetch("/api/jobs", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ module: "yt-summary", input: { url, lang, model } }),
      })
      if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
      const { id } = (await res.json()) as NewJobResponse
      navigate({ to: "/jobs/$id", params: { id: String(id) } })
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
      setSubmitting(false)
    }
  }

  return (
    <Shell>
      <form onSubmit={submit} style={{ display: "grid", gap: 10, marginBottom: 28 }}>
        <input
          type="url"
          required
          placeholder="YouTube URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          style={{ padding: 10, fontSize: 15, border: "1px solid #ccc", borderRadius: 6 }}
        />
        <div style={{ display: "flex", gap: 10 }}>
          <select value={lang} onChange={(e) => setLang(e.target.value as "no" | "en")} style={{ padding: 8 }}>
            <option value="no">Norwegian</option>
            <option value="en">English</option>
          </select>
          <select value={model} onChange={(e) => setModel(e.target.value)} style={{ padding: 8 }}>
            {MODELS.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
          <button type="submit" disabled={submitting} style={{ padding: "8px 20px", fontWeight: 600 }}>
            {submitting ? "Submitting..." : "Summarize"}
          </button>
        </div>
        {err && <p style={{ color: "#b00", margin: 0 }}>{err}</p>}
      </form>

      <div style={{ display: "grid", gap: 8 }}>
        {jobs.length === 0 && <p style={{ color: "#666" }}>No jobs yet.</p>}
        {jobs.map((j) => (
          <JobRow key={j.id} job={j} />
        ))}
      </div>
    </Shell>
  )
}

function JobRow({ job }: { job: Job }) {
  const title = typeof job.input.url === "string" ? job.input.url : `job ${job.id}`
  return (
    <Link
      to="/jobs/$id"
      params={{ id: String(job.id) }}
      style={{
        display: "flex",
        alignItems: "center",
        gap: 12,
        padding: 14,
        border: "1px solid #ddd",
        borderRadius: 8,
        textDecoration: "none",
        color: "inherit",
      }}
    >
      <StatusPill status={job.status} />
      <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{title}</span>
      <small style={{ color: "#999" }}>{job.status === "running" && job.progress ? job.progress : ""}</small>
    </Link>
  )
}
