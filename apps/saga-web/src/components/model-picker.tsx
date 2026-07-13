import { Popover } from "radix-ui"
import { ChevronDown, Cloud } from "lucide-react"
import type { Model } from "@/lib/catalog"
import { byTier } from "@/lib/catalog"
import { Meter } from "@/components/meter"
import { cn } from "@/lib/utils"

function Row({ m, selected, onPick }: { m: Model; selected: boolean; onPick: () => void }) {
  return (
    <button
      type="button"
      onClick={onPick}
      className={cn(
        "flex w-full items-center gap-3 rounded-md border-l-2 border-transparent px-3 py-2 text-left transition-colors hover:bg-accent",
        selected && "border-l-[var(--brand)] bg-accent",
      )}
    >
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-center gap-1.5 text-sm font-medium">
          {m.tier === "cloud" && <Cloud className="size-3 text-muted-foreground" aria-hidden />}
          {m.label}
          {m.default && (
            <span className="rounded-full bg-[color-mix(in_oklch,var(--brand)_18%,transparent)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--brand)]">
              Anbefalt
            </span>
          )}
        </span>
        <span className="truncate text-xs text-muted-foreground" title={m.note}>
          {m.note}
        </span>
      </span>
      <span className="flex shrink-0 flex-col items-end gap-1">
        <Meter kind="speed" value={m.speed} />
        <Meter kind="precision" value={m.precision} />
      </span>
    </button>
  )
}

export function ModelPicker({
  models,
  value,
  onChange,
}: {
  models: Model[]
  value: string
  onChange: (id: string) => void
}) {
  const groups: { key: string; label: string; rows: Model[] }[] = [
    { key: "local", label: "Lokal", rows: byTier(models, "local") },
    { key: "cloud", label: "Sky", rows: byTier(models, "cloud") },
  ]
  return (
    <Popover.Root>
      <Popover.Trigger className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        Avansert <ChevronDown className="size-3.5" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={6}
          className="z-50 w-[22rem] rounded-xl border bg-background p-2 shadow-lg"
        >
          <p className="px-3 pb-1 pt-1 text-xs text-muted-foreground">Fart / Presisjon (1-4)</p>
          {groups.map((g) => (
            <div key={g.key} className="mb-1">
              <p className="px-3 py-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{g.label}</p>
              {g.rows.map((m) => (
                <Popover.Close asChild key={m.id}>
                  <Row m={m} selected={m.id === value} onPick={() => onChange(m.id)} />
                </Popover.Close>
              ))}
            </div>
          ))}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  )
}
