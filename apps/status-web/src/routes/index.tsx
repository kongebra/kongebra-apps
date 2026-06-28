import { createFileRoute, useRouter } from "@tanstack/react-router"
import { createServerFn } from "@tanstack/react-start"
import { useEffect, useState } from "react"
import type { StatusResponse, Service } from "../types"

// Eneste sted som kjenner CHECKER_URL + gjør fetch. Kjører server-side under SSR
// OG eksponeres som RPC for client-refresh. Nettleseren når aldri checker.
const fetchStatus = createServerFn({ method: "GET" }).handler(async (): Promise<StatusResponse> => {
  const base = process.env.CHECKER_URL
  if (!base) throw new Error("CHECKER_URL ikke satt")
  const res = await fetch(`${base}/api/status`)
  if (!res.ok) throw new Error(`checker svarte ${res.status}`)
  return res.json()
})

export const Route = createFileRoute("/")({
  component: StatusPage,
  loader: () => fetchStatus(),
  errorComponent: () => (
    <main style={{ padding: 24, fontFamily: "system-ui" }}>
      <h1>status.newb.no</h1>
      <p style={{ color: "#b00" }}>Kan ikke nå checker.</p>
    </main>
  ),
})

function StatusPage() {
  const initial = Route.useLoaderData()
  const router = useRouter()

  // Client auto-refresh: invalider loader hvert 30s (samme server-fn, én datavei).
  useEffect(() => {
    const id = setInterval(() => router.invalidate(), 30_000)
    return () => clearInterval(id)
  }, [router])

  return (
    <main style={{ padding: 24, fontFamily: "system-ui", maxWidth: 720, margin: "0 auto" }}>
      <h1>status.newb.no</h1>
      <p style={{ color: "#666" }}>Sjekket: {initial.checked_at}</p>
      <div style={{ display: "grid", gap: 12 }}>
        {initial.services.map((s) => (
          <ServiceCard key={s.name} s={s} />
        ))}
      </div>
    </main>
  )
}

const COLOR: Record<string, string> = { up: "#1a7f37", down: "#b00", unknown: "#999" }

function ServiceCard({ s }: { s: Service }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12, padding: 16, border: "1px solid #ddd", borderRadius: 8 }}>
      <span style={{ width: 12, height: 12, borderRadius: "50%", background: COLOR[s.status] }} aria-label={s.status} />
      <strong style={{ flex: 1 }}>{s.name}</strong>
      <span style={{ color: "#666" }}>
        {s.status === "up" && s.latency_ms != null ? `${s.latency_ms} ms` : s.reason ?? s.status}
      </span>
      <LastChecked iso={s.last_checked} />
    </div>
  )
}

// Render absolutt tid server-side; humaniser FØRST etter mount (unngår hydration-mismatch).
function LastChecked({ iso }: { iso: string | null }) {
  const [rel, setRel] = useState<string | null>(null)
  useEffect(() => {
    if (!iso) return
    const secs = Math.round((Date.now() - new Date(iso).getTime()) / 1000)
    setRel(`for ${secs}s siden`)
  }, [iso])
  return <small style={{ color: "#999", minWidth: 90, textAlign: "right" }}>{rel ?? iso ?? "-"}</small>
}
