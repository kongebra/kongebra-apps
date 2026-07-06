import { createFileRoute, useRouter, useNavigate, Link } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useState } from "react"
import type { Job, NewJobResponse } from "../types"
import { listJobs } from "../api"
import { Shell, StatusPill } from "../ui"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

const fetchJobs = createServerFn({ method: "GET", strict: { output: false } }).handler(
  async (): Promise<Job[]> => listJobs(),
)

export const Route = createFileRoute("/")({
  component: IndexPage,
  loader: () => fetchJobs(),
  errorComponent: () => (
    <Shell>
      <p className="text-destructive">Cannot reach saga-api.</p>
    </Shell>
  ),
})

const MODELS = ["gemma4:e4b", "qwen3.5:9b"]

function IndexPage() {
  const jobs = Route.useLoaderData()
  const router = useRouter()
  const navigate = useNavigate()

  const [url, setUrl] = useState("")
  const [lang, setLang] = useState<"no" | "en">("en") // default English (v1.1)
  const [model, setModel] = useState(MODELS[0])
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    const t = setInterval(() => router.invalidate(), 5_000)
    return () => clearInterval(t)
  }, [router])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setErr(null)
    try {
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
      <form onSubmit={submit} className="mb-8 grid gap-3">
        <Input
          type="url"
          required
          placeholder="YouTube URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
        />
        <div className="flex flex-wrap gap-3">
          <Select value={lang} onValueChange={(v) => setLang(v as "no" | "en")}>
            <SelectTrigger className="w-36"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="en">English</SelectItem>
              <SelectItem value="no">Norwegian</SelectItem>
            </SelectContent>
          </Select>
          <Select value={model} onValueChange={setModel}>
            <SelectTrigger className="w-40"><SelectValue /></SelectTrigger>
            <SelectContent>
              {MODELS.map((m) => (
                <SelectItem key={m} value={m}>{m}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="submit" disabled={submitting} className="ml-auto">
            {submitting ? "Submitting..." : "Summarize"}
          </Button>
        </div>
        {err && <p className="text-sm text-destructive">{err}</p>}
      </form>

      <div className="grid gap-2">
        {jobs.length === 0 && <p className="text-muted-foreground">No jobs yet.</p>}
        {jobs.map((j) => (
          <JobRow key={j.id} job={j} />
        ))}
      </div>
    </Shell>
  )
}

function JobRow({ job }: { job: Job }) {
  const title = job.video_title || (typeof job.input.url === "string" ? job.input.url : `job ${job.id}`)
  return (
    <Link to="/jobs/$id" params={{ id: String(job.id) }}>
      <Card className="flex flex-row items-center gap-3 p-4 transition-colors hover:bg-accent">
        <StatusPill status={job.status} />
        <div className="min-w-0 flex-1">
          <p className="truncate">{title}</p>
          {job.video_description && (
            <p className="truncate text-sm text-muted-foreground">{job.video_description}</p>
          )}
        </div>
        <span className="text-sm text-muted-foreground">
          {job.status === "running" && job.progress ? job.progress : ""}
        </span>
      </Card>
    </Link>
  )
}
