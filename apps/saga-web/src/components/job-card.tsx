import { Loader2 } from "lucide-react"
import type { Job } from "@/types"
import type { Model } from "@/lib/catalog"
import { videoId, thumbUrl } from "@/lib/youtube"
import { StatusPill } from "@/ui"
import { cn } from "@/lib/utils"

function modelLabel(job: Job, models: Model[]): string | null {
  const id = typeof job.input.model === "string" ? job.input.model : null
  if (!id) return null
  const m = models.find((x) => x.id === id)
  if (!m) return id
  return `${m.label} - ${m.tier === "cloud" ? "Turbo" : "Lokal"}`
}

export function JobCard({
  job,
  models,
  onOpen,
  highlighted,
}: {
  job: Job
  models: Model[]
  onOpen: () => void
  highlighted?: boolean
}) {
  const rawUrl = typeof job.input.url === "string" ? job.input.url : ""
  const vid = rawUrl ? videoId(rawUrl) : null
  const title = job.video_title || rawUrl || `job ${job.id}`

  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "group flex flex-col overflow-hidden rounded-xl border bg-card text-left transition-shadow hover:shadow-md",
        highlighted && "ring-2 ring-[var(--brand)]",
      )}
    >
      <div className="relative aspect-video w-full overflow-hidden bg-muted">
        {vid ? (
          <img src={thumbUrl(vid)} alt="" className="size-full object-cover" />
        ) : (
          <div className="grid size-full place-items-center text-muted-foreground">YouTube</div>
        )}
        {job.status === "running" && (
          <div className="absolute inset-x-0 bottom-0 h-1 overflow-hidden bg-black/20">
            <div className="h-full w-1/3 animate-pulse bg-[var(--status-running)]" />
          </div>
        )}
      </div>

      <div className="flex flex-1 flex-col gap-2 p-4">
        <p className="line-clamp-2 font-medium leading-snug">{title}</p>
        <div className="mt-auto flex items-center gap-2 text-xs text-muted-foreground">
          <StatusPill status={job.status} />
          {job.status === "queued" && <span className="inline-flex items-center gap-1"><Loader2 className="size-3 animate-spin" /> I kø</span>}
          {job.status === "running" && <span className="tabular-nums">{job.progress || "kjører"}</span>}
          {job.status === "failed" && <span className="line-clamp-1 text-destructive">{job.error ?? "feilet"}</span>}
          {job.status === "done" && modelLabel(job, models) && <span className="tabular-nums">laget med {modelLabel(job, models)}</span>}
        </div>
      </div>
    </button>
  )
}
