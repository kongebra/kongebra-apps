// Live ETA for map-reduce summaries, projected from how long completed chunks
// took. Rough by design (chunk cost varies); shown with a leading "~".
export function formatEta(ms: number): string {
  const s = Math.round(ms / 1000)
  if (s < 60) return `~${s}s`
  return `~${Math.floor(s / 60)}m ${s % 60}s`
}

export function estimateEta(
  startMs: number,
  nowMs: number,
  chunksDone: number,
  chunksRemaining: number,
): string | null {
  if (chunksDone <= 0) return null
  const perChunk = (nowMs - startMs) / chunksDone
  return formatEta(perChunk * chunksRemaining)
}
