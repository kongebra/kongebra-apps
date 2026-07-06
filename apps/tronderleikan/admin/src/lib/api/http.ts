import { getAppSession } from '@/lib/auth/session'
import { deriveUser, refresh, toSessionTokens } from '@/lib/auth/oidc'
import { ApiError } from '@/lib/api/errors'
import type { Role, SessionTokens } from '@/lib/auth/session'

export { ApiError }

// Fornyer access-token hvis det utløper snart. Skriver oppdaterte tokens tilbake
// til den forseglede sessionen. Returnerer gyldig token eller undefined (anonym).
async function validAccessToken(): Promise<string | undefined> {
  const session = getAppSession()
  const s = await session
  const tokens = s.data.tokens
  if (!tokens) return undefined

  const stillValid = tokens.expiresAt - Date.now() > 60_000
  if (stillValid) return tokens.accessToken

  if (!tokens.refreshToken) return tokens.accessToken
  try {
    const fresh = await refresh(tokens.refreshToken)
    const next: SessionTokens = {
      ...toSessionTokens(fresh),
      // Behold gammelt refresh-token hvis serveren ikke roterte det.
      refreshToken: fresh.refresh_token ?? tokens.refreshToken,
    }
    await s.update({ tokens: next, user: deriveUser(fresh) })
    return next.accessToken
  } catch {
    // Fornyelse feilet - la kallet gå anonymt/ugyldig, kalleren håndterer 401.
    return tokens.accessToken
  }
}

interface FetchOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  // auth: 'anon' = ikke send token, 'optional' = send hvis innlogget,
  // 'required' = krev token (kaster 401 hvis mangler).
  auth?: 'anon' | 'optional' | 'required'
}

// Enkelt BFF-kall mot en Go-tjeneste. baseUrl er tjenestens interne URL;
// path inkluderer /api/<service>-prefikset (rutene definerer det selv).
export async function serviceFetch<T>(
  baseUrl: string,
  path: string,
  opts: FetchOptions = {},
): Promise<T> {
  const { method = 'GET', body, auth = 'optional' } = opts
  const headers: Record<string, string> = { Accept: 'application/json' }

  if (auth !== 'anon') {
    const token = await validAccessToken()
    if (token) {
      headers.Authorization = `Bearer ${token}`
    } else if (auth === 'required') {
      throw new ApiError(401, 'ikke innlogget')
    }
  }

  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }

  const res = await fetch(`${baseUrl}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (res.status === 204) {
    return undefined as T
  }

  const text = await res.text()
  const parsed: unknown = text ? JSON.parse(text) : undefined

  if (!res.ok) {
    const msg =
      parsed &&
      typeof parsed === 'object' &&
      'error' in parsed &&
      typeof (parsed as { error: unknown }).error === 'string'
        ? (parsed as { error: string }).error
        : `HTTP ${res.status}`
    throw new ApiError(res.status, msg)
  }

  return parsed as T
}

// Autorisasjonssjekk ved BFF-grensen (TanStack-guide: beskytt data-grensen).
// Kaster hvis brukeren mangler rollen. Ruter/UI gater i tillegg, men dette er
// den bindende sjekken.
export async function requireRole(role: Role): Promise<void> {
  const s = await getAppSession()
  const user = s.data.user
  if (!user) throw new ApiError(401, 'ikke innlogget')
  if (!user.roles.includes(role)) {
    throw new ApiError(403, `krever rollen ${role}`)
  }
}
