import { useEffect, useRef, useState } from "react"
import { ArrowRight } from "lucide-react"
import type { Model, Tier } from "@/lib/catalog"
import { tierDefault } from "@/lib/catalog"
import { loadStored, resolveSelection, saveSelection } from "@/lib/selection"
import { TierToggle } from "@/components/tier-toggle"
import { ModelPicker } from "@/components/model-picker"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { Job, NewJobResponse } from "@/types"

export function Composer({
  models,
  cloudEnabled,
  onOptimistic,
}: {
  models: Model[]
  cloudEnabled: boolean
  onOptimistic: (job: Job) => void
}) {
  const [url, setUrl] = useState("")
  const [lang, setLang] = useState<"no" | "en">("en")
  const [sel, setSel] = useState(() => resolveSelection(models, null))
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Reconcile stored selection against the live catalog once, on mount (client
  // only - localStorage is unavailable during SSR).
  useEffect(() => {
    setSel(resolveSelection(models, loadStored()))
  }, [models])

  function setTier(tier: Tier) {
    const next = { tier, modelId: tierDefault(models, tier)?.id ?? "" }
    setSel(next)
    saveSelection(next)
  }

  function setModel(modelId: string) {
    const m = models.find((x) => x.id === modelId)
    const next = { tier: m?.tier ?? sel.tier, modelId }
    setSel(next)
    saveSelection(next)
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!sel.modelId) {
      setErr("Modellkatalogen er ikke lastet enda. Prøv igjen om et øyeblikk.")
      return
    }
    setSubmitting(true)
    setErr(null)
    try {
      const res = await fetch("/api/jobs", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ module: "yt-summary", input: { url, lang, model: sel.modelId } }),
      })
      if (!res.ok) throw new Error(`saga-api returned ${res.status}`)
      const { id } = (await res.json()) as NewJobResponse
      onOptimistic({
        id,
        module: "yt-summary",
        input: { url, lang, model: sel.modelId },
        status: "queued",
        attempts: 0,
        progress: "",
        error: null,
        created_at: new Date().toISOString(),
        video_title: null,
      })
      setUrl("")
      inputRef.current?.focus()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSubmitting(false)
    }
  }

  const selectedLabel = models.find((m) => m.id === sel.modelId)?.label ?? sel.modelId

  return (
    <form onSubmit={submit} className="mx-auto flex max-w-2xl flex-col items-center gap-4">
      <div className="relative w-full">
        <input
          ref={inputRef}
          type="url"
          required
          aria-label="YouTube-URL"
          placeholder="Lim inn en YouTube-URL"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          className="h-14 w-full rounded-xl border bg-background pl-5 pr-14 text-lg outline-none focus:ring-2 focus:ring-[var(--brand)]"
        />
        <button
          type="submit"
          disabled={submitting}
          aria-label="Oppsummer"
          className="absolute right-2 top-2 grid size-10 place-items-center rounded-lg bg-[var(--brand)] text-white transition-opacity disabled:opacity-50"
        >
          <ArrowRight className="size-5" />
        </button>
      </div>

      <div className="w-full">
        <TierToggle value={sel.tier} onChange={setTier} cloudEnabled={cloudEnabled} />
      </div>

      <div className="flex w-full items-center justify-between gap-3 text-sm">
        <Select value={lang} onValueChange={(v) => setLang(v as "no" | "en")}>
          <SelectTrigger className="h-8 w-32"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="en">English</SelectItem>
            <SelectItem value="no">Norsk</SelectItem>
          </SelectContent>
        </Select>
        <span className="text-muted-foreground">
          Modell: <span className="text-foreground">{selectedLabel}</span>
        </span>
        <ModelPicker models={models} value={sel.modelId} cloudEnabled={cloudEnabled} onChange={setModel} />
      </div>

      {err && <p className="text-sm text-destructive">{err}</p>}
    </form>
  )
}
