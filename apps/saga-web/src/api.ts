import type { Job } from "./types"

// Server-side only. The browser never calls saga-api through this module; it
// uses relative /api paths (same origin via the ingress). This is the SSR path.
export function apiBase(): string {
  return process.env.SAGA_API_URL ?? "http://localhost:8080"
}

export async function listJobs(): Promise<Job[]> {
  const res = await fetch(`${apiBase()}/api/jobs`)
  if (!res.ok) throw new Error(`saga-api /api/jobs returned ${res.status}`)
  const body = (await res.json()) as { jobs: Job[] }
  return body.jobs ?? []
}

export async function getJob(id: number): Promise<Job | null> {
  const res = await fetch(`${apiBase()}/api/jobs/${id}`)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(`saga-api /api/jobs/${id} returned ${res.status}`)
  return (await res.json()) as Job
}
