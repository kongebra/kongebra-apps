import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useRef, useState } from "react"
import type { Job, ProgressEvent } from "../types"
import { getJob } from "../api"
import { StatusPill } from "../ui"
import { Markdown } from "../markdown"
import { estimateEta } from "@/lib/eta"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ExternalLink, Copy, Download } from "lucide-react"

const fetchJob = createServerFn({ method: "GET", strict: { output: false } })
  .validator((id: unknown): number => Number(id))
  .handler(async ({ data: id }): Promise<Job | null> => getJob(id))

export const Route = createFileRoute("/_dash/jobs/$id")({
  component: JobDrawer,
  loader: ({ params }) => fetchJob({ data: Number(params.id) }),
})

function isTerminal(s: Job["status"]): boolean {
  return s === "done" || s === "failed"
}

async function getJobClient(id: number): Promise<Job | null> {
  const res = await fetch(`/api/jobs/${id}`)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
  return (await res.json()) as Job
}

function JobDrawer() {
  const initial = Route.useLoaderData()
  const { id } = Route.useParams()
  const navigate = useNavigate()

  const [job, setJob] = useState<Job | null>(initial)
  const [live, setLive] = useState("")
  const [tokens, setTokens] = useState("")
  const tokensRef = useRef("")
  const [eta, setEta] = useState<string | null>(null)
  const chunkTiming = useRef<{ start: number; startIdx: number }>({ start: 0, startIdx: 0 })
  const [streamKey, setStreamKey] = useState(0)
  const jobRef = useRef(job)
  jobRef.current = job

  useEffect(() => {
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

  function close() {
    navigate({ to: "/" })
  }

  const rawUrl = job && typeof job.input.url === "string" ? job.input.url : null
  const safeHref = rawUrl && /^https?:\/\//i.test(rawUrl) ? rawUrl : undefined
  const title = job?.video_title || rawUrl || `job ${id}`

  return (
    <Sheet open onOpenChange={(o) => !o && close()}>
      <SheetContent>
        <SheetHeader>
          {job && <StatusPill status={job.status} />}
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>Jobbdetaljer</SheetDescription>
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          {!job ? (
            <p className="text-destructive">Fant ikke jobb {id}.</p>
          ) : (
            <>
              <div className="mb-4 flex flex-wrap gap-2">
                {safeHref && (
                  <a href={safeHref} target="_blank" rel="noreferrer">
                    <Button variant="outline" size="sm"><ExternalLink className="size-4" /> Åpne på YouTube</Button>
                  </a>
                )}
              </div>

              {!isTerminal(job.status) && (
                <div className="mb-4">
                  <p className="text-muted-foreground">
                    {live || job.progress || job.status}{eta ? ` - ${eta} igjen` : ""}
                  </p>
                  {tokens && <pre className="mt-2 whitespace-pre-wrap rounded-lg bg-muted p-3 text-sm">{tokens}</pre>}
                </div>
              )}

              {job.status === "failed" && <FailedView job={job} onRetried={() => setStreamKey((k) => k + 1)} setJob={setJob} setLive={setLive} />}
              {job.status === "done" && job.result_markdown && <SummaryView job={job} />}
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function FailedView({
  job,
  onRetried,
  setJob,
  setLive,
}: {
  job: Job
  onRetried: () => void
  setJob: (j: Job) => void
  setLive: (s: string) => void
}) {
  const [busy, setBusy] = useState(false)
  return (
    <div className="mb-4">
      <p className="text-destructive">{job.error ?? "feilet"}</p>
      <Button
        className="mt-2"
        size="sm"
        disabled={busy}
        onClick={async () => {
          setBusy(true)
          const res = await fetch(`/api/jobs/${job.id}/retry`, { method: "POST" })
          if (res.ok) {
            const j = await getJobClient(job.id)
            if (j) {
              setLive("")
              setJob(j)
              onRetried()
            }
          }
          setBusy(false)
        }}
      >
        {busy ? "Prøver igjen..." : "Prøv igjen"}
      </Button>
    </div>
  )
}

function SummaryView({ job }: { job: Job }) {
  const [showNo, setShowNo] = useState(false)
  const [translated, setTranslated] = useState<string | null>(job.translated_markdown ?? null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState<string | null>(null)

  async function toNorwegian() {
    if (translated) { setShowNo(true); return }
    setErr(null)
    setLoading(true)
    try {
      const res = await fetch(`/api/jobs/${job.id}/translate`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ lang: "no" }),
      })
      if (!res.ok) throw new Error(`oversetting feilet: ${res.status}`)
      const { translated_markdown } = (await res.json()) as { translated_markdown: string }
      setTranslated(translated_markdown)
      setShowNo(true)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  const body = showNo && translated ? translated : job.result_markdown!
  return (
    <div>
      <div className="mb-4 flex flex-wrap gap-2">
        <Button variant={showNo ? "outline" : "default"} size="sm" onClick={() => setShowNo(false)}>English</Button>
        <Button variant={showNo ? "default" : "outline"} size="sm" onClick={toNorwegian} disabled={loading}>
          {loading ? "Oversetter..." : "Norsk"}
        </Button>
        <Button variant="outline" size="sm" onClick={() => navigator.clipboard?.writeText(body)}>
          <Copy className="size-4" /> Kopier
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            const blob = new Blob([body], { type: "text/markdown" })
            const a = document.createElement("a")
            a.href = URL.createObjectURL(blob)
            a.download = `saga-${job.id}.md`
            a.click()
            URL.revokeObjectURL(a.href)
          }}
        >
          <Download className="size-4" /> Last ned
        </Button>
      </div>
      {err && <p className="text-sm text-destructive">{err}</p>}
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
