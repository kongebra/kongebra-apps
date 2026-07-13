import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import { Moon, Sun } from "lucide-react"
import type { JobStatus } from "./types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useTheme } from "./theme"
import { cn } from "@/lib/utils"

const STATUS_STYLE: Record<JobStatus, string> = {
  queued: "bg-[var(--status-queued)] text-white",
  running: "bg-[var(--status-running)] text-white",
  done: "bg-[var(--status-done)] text-white",
  failed: "bg-[var(--status-failed)] text-white",
}

const STATUS_LABEL: Record<JobStatus, string> = {
  queued: "I kø",
  running: "Kjører",
  done: "Ferdig",
  failed: "Feilet",
}

export function StatusPill({ status }: { status: JobStatus }) {
  return <Badge className={cn(STATUS_STYLE[status])}>{STATUS_LABEL[status]}</Badge>
}

export function Shell({ children }: { children: ReactNode }) {
  const { theme, toggle } = useTheme()
  return (
    <div className="min-h-screen bg-background text-foreground">
      <div className="mx-auto max-w-3xl px-4 py-6">
        <header className="mb-6 flex items-center justify-between">
          <Link to="/" className="text-2xl font-bold tracking-tight">
            Saga
          </Link>
          <Button variant="ghost" size="icon" onClick={toggle} aria-label="Toggle theme">
            {theme === "dark" ? <Sun className="size-5" /> : <Moon className="size-5" />}
          </Button>
        </header>
        <main>{children}</main>
      </div>
    </div>
  )
}
