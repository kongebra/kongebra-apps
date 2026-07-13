import { test } from "node:test"
import assert from "node:assert/strict"
import { resolveSelection } from "./selection"
import type { Model } from "./catalog"

const M = (id: string, tier: "local" | "cloud", def = false): Model => ({
  id, label: id, tier, norwegian: false, speed: 1, precision: 1,
  priceInPerMtok: 0, priceOutPerMtok: 0, note: "", default: def,
})

const models: Model[] = [
  M("qwen3.5:2b", "local", true),
  M("qwen3.5:4b", "local"),
  M("deepseek-v4-flash:cloud", "cloud", true),
]

test("keeps a still-valid stored model and syncs tier", () => {
  const sel = resolveSelection(models, { tier: "local", modelId: "deepseek-v4-flash:cloud" })
  assert.equal(sel.modelId, "deepseek-v4-flash:cloud")
  assert.equal(sel.tier, "cloud") // tier follows the model, not the stale stored tier
})

test("falls back to stored tier default when model was dropped", () => {
  const sel = resolveSelection(models, { tier: "cloud", modelId: "gone:cloud" })
  assert.equal(sel.modelId, "deepseek-v4-flash:cloud")
  assert.equal(sel.tier, "cloud")
})

test("defaults to local default when nothing stored", () => {
  const sel = resolveSelection(models, null)
  assert.equal(sel.tier, "local")
  assert.equal(sel.modelId, "qwen3.5:2b")
})

test("empty catalog yields local tier with empty modelId (no crash)", () => {
  const sel = resolveSelection([], null)
  assert.equal(sel.tier, "local")
  assert.equal(sel.modelId, "")
})
