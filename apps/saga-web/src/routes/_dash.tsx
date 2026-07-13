import { createFileRoute, Outlet, useNavigate, useRouter } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useMemo, useState } from "react"
import type { Job, JobStatus } from "../types"
import type { Model } from "@/lib/catalog"
import { fetchModels } from "@/lib/catalog"
import { listJobs } from "../api"
import { Shell } from "../ui"
import { Composer } from "@/components/composer"
import { JobCard } from "@/components/job-card"
import { TooltipProvider } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

const fetchJobs = createServerFn({ method: "GET", strict: { output: false } }).handler(
  async (): Promise<Job[]> => listJobs(),
)

export const Route = createFileRoute("/_dash")({
  component: DashLayout,
  loader: () => fetchJobs(),
  errorComponent: () => (
    <Shell>
      <p className="text-destructive">Får ikke kontakt med saga-api.</p>
    </Shell>
  ),
})

type Filter = "all" | JobStatus
const FILTERS: { key: Filter; label: string }[] = [
  { key: "all", label: "Alle" },
  { key: "running", label: "Kjører" },
  { key: "done", label: "Ferdig" },
  { key: "failed", label: "Feilet" },
]

function DashLayout() {
  const loaded = Route.useLoaderData()
  const router = useRouter()
  const navigate = useNavigate()

  const [jobs, setJobs] = useState<Job[]>(loaded)
  const [optimistic, setOptimistic] = useState<Job[]>([])
  const [highlightId, setHighlightId] = useState<number | null>(null)
  const [models, setModels] = useState<Model[]>([])
  const [cloudEnabled, setCloudEnabled] = useState(true)
  const [filter, setFilter] = useState<Filter>("all")
  const [q, setQ] = useState("")

  // Server loader reruns on router.invalidate; keep local state in sync and
  // drop optimistic rows the server now knows about.
  useEffect(() => {
    setJobs(loaded)
    setOptimistic((opt) => opt.filter((o) => !loaded.some((j) => j.id === o.id)))
  }, [loaded])

  useEffect(() => {
    fetchModels()
      .then((r) => {
        setModels(r.models)
        setCloudEnabled(r.cloudEnabled)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    const t = setInterval(() => router.invalidate(), 5_000)
    return () => clearInterval(t)
  }, [router])

  const merged = useMemo(() => {
    const seen = new Set(jobs.map((j) => j.id))
    return [...optimistic.filter((o) => !seen.has(o.id)), ...jobs]
  }, [jobs, optimistic])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return merged.filter((j) => {
      if (filter !== "all" && j.status !== filter) return false
      if (!needle) return true
      const t = (j.video_title || "") + " " + (typeof j.input.url === "string" ? j.input.url : "")
      return t.toLowerCase().includes(needle)
    })
  }, [merged, filter, q])

  const hasJobs = merged.length > 0

  return (
    <TooltipProvider delayDuration={200}>
      <Shell>
        <section className={cn("transition-all", hasJobs ? "py-6" : "py-20")}>
          <Composer
            models={models}
            cloudEnabled={cloudEnabled}
            onOptimistic={(job) => {
              setOptimistic((o) => [job, ...o])
              setHighlightId(job.id)
            }}
          />
        </section>

        {hasJobs && (
          <>
            <div className="mb-4 flex flex-wrap items-center gap-2">
              {FILTERS.map((f) => (
                <button
                  key={f.key}
                  type="button"
                  onClick={() => setFilter(f.key)}
                  className={cn(
                    "rounded-full border px-3 py-1 text-sm transition-colors",
                    filter === f.key ? "border-[var(--brand)] bg-accent" : "text-muted-foreground hover:bg-accent",
                  )}
                >
                  {f.label}
                </button>
              ))}
              <input
                type="text"
                placeholder="Filtrer på tittel"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                className="ml-auto h-8 w-48 rounded-md border bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-[var(--brand)]"
              />
            </div>

            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {shown.map((j) => (
                <JobCard
                  key={j.id}
                  job={j}
                  models={models}
                  highlighted={j.id === highlightId}
                  onOpen={() => navigate({ to: "/jobs/$id", params: { id: String(j.id) } })}
                />
              ))}
            </div>
            {shown.length === 0 && <p className="text-muted-foreground">Ingen jobber matcher filteret.</p>}
          </>
        )}
      </Shell>

      <Outlet />
    </TooltipProvider>
  )
}
