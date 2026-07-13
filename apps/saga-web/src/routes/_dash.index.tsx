import { createFileRoute } from "@tanstack/react-router"

// The dashboard lives in the _dash layout; the index leaf renders nothing
// (drawer closed). Deep routes (/jobs/$id) render the drawer via the layout's
// Outlet while this same layout keeps the dashboard mounted behind it.
export const Route = createFileRoute("/_dash/")({
  component: () => null,
})
