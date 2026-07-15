import { expect, test } from "bun:test";
import { readingTime } from "./reading-time.ts";

test("floors at 1 minute for short text", () => {
  expect(readingTime("just a few words here")).toBe(1);
});

test("scales with word count", () => {
  const text = Array.from({ length: 600 }, () => "word").join(" ");
  expect(readingTime(text)).toBe(3);
});

test("handles empty input without crashing", () => {
  expect(readingTime("")).toBe(1);
});
