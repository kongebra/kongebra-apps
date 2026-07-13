export function meterSegments(value: number, max = 4): boolean[] {
  const n = Math.max(0, Math.min(max, Math.round(value)))
  return Array.from({ length: max }, (_, i) => i < n)
}

export function meterLabel(kind: "speed" | "precision", value: number, max = 4): string {
  const label = kind === "speed" ? "Speed" : "Presisjon"
  return `${label} ${Math.max(0, Math.min(max, Math.round(value)))} of ${max}`
}
