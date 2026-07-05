// Små runtime-hjelpere for server-function-validatorer. Vi eier både klient og
// server, så validering er defensiv (ikke full schema) - fanger tomme id-er o.l.

export function str(v: unknown, field: string): string {
  if (typeof v !== 'string' || v.trim() === '') {
    throw new Error(`felt "${field}" må være en ikke-tom streng`)
  }
  return v
}

export function optStr(v: unknown, field: string): string | null {
  if (v === undefined || v === null || v === '') return null
  if (typeof v !== 'string') {
    throw new Error(`felt "${field}" må være streng`)
  }
  return v
}

export function optNum(v: unknown, field: string): number | null {
  if (v === undefined || v === null) return null
  if (typeof v !== 'number' || Number.isNaN(v)) {
    throw new Error(`felt "${field}" må være tall`)
  }
  return v
}

export function bool(v: unknown): boolean {
  return v === true
}

export function obj(v: unknown): Record<string, unknown> {
  if (typeof v !== 'object' || v === null) {
    throw new Error('forventet objekt')
  }
  return v as Record<string, unknown>
}
