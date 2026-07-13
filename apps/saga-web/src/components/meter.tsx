import { Gauge, Target } from "lucide-react"
import { meterSegments, meterLabel } from "@/lib/meters"
import { cn } from "@/lib/utils"

export function Meter({ kind, value }: { kind: "speed" | "precision"; value: number }) {
  const Icon = kind === "speed" ? Gauge : Target
  const segs = meterSegments(value)
  return (
    <span className="inline-flex items-center gap-1.5 tabular-nums text-muted-foreground">
      <Icon className="size-3" aria-hidden />
      <span role="img" aria-label={meterLabel(kind, value)} className="flex items-center gap-[3px]">
        {segs.map((on, i) => (
          <span
            key={i}
            className={cn("h-3.5 w-1.5 rounded-[2px]", on ? "bg-foreground/85" : "bg-foreground/12")}
          />
        ))}
      </span>
      <span className="text-xs">{Math.round(value)}/4</span>
    </span>
  )
}
