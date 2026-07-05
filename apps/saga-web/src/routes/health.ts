import { createFileRoute } from "@tanstack/react-router"

// Self-contained health check (never gated on saga-api, so backend downtime
// does not take the UI's own liveness probe down).
export const Route = createFileRoute("/health")({
  server: {
    handlers: {
      GET: async () => Response.json({ status: "ok" }),
    },
  },
})
