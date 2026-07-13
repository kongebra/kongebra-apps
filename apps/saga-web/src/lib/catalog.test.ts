import { test } from "node:test"
import assert from "node:assert/strict"
import { byTier, tierDefault, type Model } from "./catalog"

const M = (over: Partial<Model>): Model => ({
  id: "x", label: "X", tier: "local", norwegian: false, speed: 1,
  precision: 1, priceInPerMtok: 0, priceOutPerMtok: 0, note: "", default: false,
  ...over,
})

const models: Model[] = [
  M({ id: "a", tier: "local", default: false }),
  M({ id: "b", tier: "local", default: true }),
  M({ id: "c", tier: "cloud", default: true }),
  M({ id: "d", tier: "cloud", default: false }),
]

test("byTier filters by tier", () => {
  assert.deepEqual(byTier(models, "local").map((m) => m.id), ["a", "b"])
  assert.deepEqual(byTier(models, "cloud").map((m) => m.id), ["c", "d"])
})

test("tierDefault returns the default row", () => {
  assert.equal(tierDefault(models, "local")?.id, "b")
  assert.equal(tierDefault(models, "cloud")?.id, "c")
})

test("tierDefault falls back to first of tier when none flagged", () => {
  const noneFlagged = models.map((m) => ({ ...m, default: false }))
  assert.equal(tierDefault(noneFlagged, "local")?.id, "a")
})

test("tierDefault is undefined when tier is empty", () => {
  assert.equal(tierDefault([], "local"), undefined)
})
