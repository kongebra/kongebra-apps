export type Tier = "local" | "cloud"

export interface Model {
  id: string
  label: string
  tier: Tier
  norwegian: boolean
  speed: number
  precision: number
  priceInPerMtok: number
  priceOutPerMtok: number
  note: string
  default: boolean
}

// The server owns the model list; the browser never hardcodes it. cloudEnabled
// reflects whether OLLAMA_API_KEY is configured server-side (drives the Turbo
// gate). ponytail: the API adds `cloud_enabled` alongside `models`; until then
// it defaults true (cloud models are always listed). Wired in Task 12's API note.
export async function fetchModels(): Promise<{ models: Model[]; cloudEnabled: boolean }> {
  const res = await fetch("/api/models")
  if (!res.ok) throw new Error(`/api/models returned ${res.status}`)
  const body = (await res.json()) as { models?: Model[]; cloud_enabled?: boolean }
  return { models: body.models ?? [], cloudEnabled: body.cloud_enabled ?? true }
}

export function byTier(models: Model[], tier: Tier): Model[] {
  return models.filter((m) => m.tier === tier)
}

export function tierDefault(models: Model[], tier: Tier): Model | undefined {
  const t = byTier(models, tier)
  return t.find((m) => m.default) ?? t[0]
}
