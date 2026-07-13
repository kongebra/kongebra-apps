import { tierDefault, type Model, type Tier } from "./catalog"

export interface Selection {
  tier: Tier
  modelId: string
}

export const STORAGE_KEY = "saga.selection"

// Reconcile a stored selection against the live catalog. A stored model that
// still exists wins (and pins the tier to that model's tier); a dropped model
// falls back to the stored tier's default. Guarantees the returned modelId is
// always one the catalog currently serves (or "" for an empty catalog).
export function resolveSelection(models: Model[], stored: Partial<Selection> | null): Selection {
  const storedTier: Tier = stored?.tier === "cloud" ? "cloud" : "local"
  if (stored?.modelId) {
    const hit = models.find((m) => m.id === stored.modelId)
    if (hit) return { tier: hit.tier, modelId: hit.id }
  }
  const def = tierDefault(models, storedTier)
  return { tier: storedTier, modelId: def?.id ?? "" }
}

export function loadStored(): Partial<Selection> | null {
  if (typeof localStorage === "undefined") return null
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as Partial<Selection>) : null
  } catch {
    return null
  }
}

export function saveSelection(sel: Selection): void {
  if (typeof localStorage === "undefined") return
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(sel))
  } catch {
    // ponytail: ignore quota/private-mode write failures; selection just
    // won't persist. No user-facing error for a non-critical preference.
  }
}
