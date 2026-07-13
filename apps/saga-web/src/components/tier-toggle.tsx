import { ToggleGroup as TG } from "radix-ui"
import { Zap } from "lucide-react"
import type { Tier } from "@/lib/catalog"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

export function TierToggle({
  value,
  onChange,
  cloudEnabled,
}: {
  value: Tier
  onChange: (t: Tier) => void
  cloudEnabled: boolean
}) {
  const turbo = (
    <TG.Item
      value="cloud"
      disabled={!cloudEnabled}
      aria-label="Turbo"
      className={cn(
        "flex flex-1 flex-col items-start gap-0.5 rounded-lg px-4 py-2.5 text-left transition-colors",
        "data-[state=on]:bg-[color-mix(in_oklch,var(--turbo)_14%,transparent)]",
        "disabled:cursor-not-allowed disabled:opacity-40",
      )}
    >
      <span className={cn("flex items-center gap-1.5 text-sm font-medium", value === "cloud" && "text-[var(--turbo)]")}>
        <Zap className="size-3.5" /> Turbo
      </span>
      <span className="text-xs text-muted-foreground">Raskere og skarpere, via sky</span>
    </TG.Item>
  )

  return (
    <TG.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onChange(v as Tier)}
      className="flex gap-1 rounded-xl border bg-muted/40 p-1"
    >
      <TG.Item
        value="local"
        aria-label="Lokal"
        className="flex flex-1 flex-col items-start gap-0.5 rounded-lg px-4 py-2.5 text-left transition-colors data-[state=on]:bg-background data-[state=on]:shadow-sm"
      >
        <span className="text-sm font-medium">Lokal</span>
        <span className="text-xs text-muted-foreground">Gratis, egen maskin</span>
      </TG.Item>
      {cloudEnabled ? (
        turbo
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="flex-1">{turbo}</span>
          </TooltipTrigger>
          <TooltipContent>Sky ikke konfigurert</TooltipContent>
        </Tooltip>
      )}
    </TG.Root>
  )
}
