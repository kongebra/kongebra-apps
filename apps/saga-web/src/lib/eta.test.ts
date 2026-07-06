import { test } from "node:test"
import assert from "node:assert/strict"
import { formatEta, estimateEta } from "./eta.ts"

test("formatEta", () => {
  assert.equal(formatEta(45_000), "~45s")
  assert.equal(formatEta(150_000), "~2m 30s")
})

test("estimateEta projects from chunk pace", () => {
  // started at t=0, now t=20s, 2 chunks done, 8 remaining -> 10s/chunk * 8 = 80s
  assert.equal(estimateEta(0, 20_000, 2, 8), "~1m 20s")
  assert.equal(estimateEta(0, 20_000, 0, 8), null) // no chunk done yet
})
