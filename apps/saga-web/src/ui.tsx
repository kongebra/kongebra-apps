import type { ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import type { JobStatus } from "./types"

const STATUS_COLOR: Record<JobStatus, string> = {
  queued: "#999",
  running: "#0969da",
  done: "#1a7f37",
  failed: "#b00",
}

export function StatusPill({ status }: { status: JobStatus }) {
  return (
    <span
      style={{
        display: "inline-block",
        padding: "2px 10px",
        borderRadius: 999,
        fontSize: 12,
        fontWeight: 600,
        color: "#fff",
        background: STATUS_COLOR[status],
      }}
    >
      {status}
    </span>
  )
}

export function Shell({ children }: { children: ReactNode }) {
  return (
    <main style={{ fontFamily: "system-ui, sans-serif", maxWidth: 760, margin: "0 auto", padding: 24 }}>
      <h1 style={{ marginBottom: 20 }}>
        <Link to="/" style={{ color: "inherit", textDecoration: "none" }}>
          Saga
        </Link>
      </h1>
      {children}
    </main>
  )
}
