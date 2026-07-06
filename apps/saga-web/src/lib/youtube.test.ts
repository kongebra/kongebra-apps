import { test } from "node:test"
import assert from "node:assert/strict"
import { videoId, embedUrl, thumbUrl } from "./youtube.ts"

test("videoId parses the three URL forms", () => {
  assert.equal(videoId("https://www.youtube.com/watch?v=aircAruvnKk"), "aircAruvnKk")
  assert.equal(videoId("https://youtu.be/aircAruvnKk"), "aircAruvnKk")
  assert.equal(videoId("https://www.youtube.com/embed/aircAruvnKk"), "aircAruvnKk")
  assert.equal(videoId("https://example.com/x"), null)
})

test("embed + thumb urls", () => {
  assert.equal(embedUrl("abc"), "https://www.youtube-nocookie.com/embed/abc")
  assert.equal(thumbUrl("abc"), "https://i.ytimg.com/vi/abc/hqdefault.jpg")
})
