import { test } from "node:test"
import assert from "node:assert/strict"
import { meterSegments, meterLabel } from "./meters"

test("meterSegments fills first N of 4", () => {
  assert.deepEqual(meterSegments(3), [true, true, true, false])
  assert.deepEqual(meterSegments(0), [false, false, false, false])
  assert.deepEqual(meterSegments(4), [true, true, true, true])
})

test("meterSegments clamps out-of-range", () => {
  assert.deepEqual(meterSegments(7), [true, true, true, true])
  assert.deepEqual(meterSegments(-2), [false, false, false, false])
})

test("meterLabel formats kind + value", () => {
  assert.equal(meterLabel("speed", 3), "Speed 3 of 4")
  assert.equal(meterLabel("precision", 4), "Presisjon 4 of 4")
})
