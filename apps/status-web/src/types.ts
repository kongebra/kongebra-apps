export type Status = "up" | "down" | "unknown"

export interface Service {
  name: string
  url: string
  status: Status
  latency_ms: number | null
  http_code: number | null
  reason: string | null
  last_checked: string | null
}

export interface StatusResponse {
  checked_at: string
  services: Service[]
}
