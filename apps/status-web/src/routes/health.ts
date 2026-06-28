import { createFileRoute } from "@tanstack/react-router"

// Selv-avhengig health (aldri gated på checker - ellers tar checker-nedetid ned status-siden).
export const Route = createFileRoute("/health")({
  server: {
    handlers: {
      GET: async () => Response.json({ status: "ok" }),
    },
  },
})
