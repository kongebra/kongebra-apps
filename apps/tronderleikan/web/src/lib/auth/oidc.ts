import * as client from 'openid-client'

import { authConfig } from '@/lib/config'
import type { Role, SessionTokens, SessionUser } from '@/lib/auth/session'

// Discovery er nettverk + parsing; cache per issuer så vi ikke slår opp per request.
let configPromise: Promise<client.Configuration> | undefined

export function getOidcConfig(): Promise<client.Configuration> {
  if (!configPromise) {
    const { issuer, clientId } = authConfig()
    // Public client: ingen client secret, token-endpoint auth = none, kun PKCE.
    configPromise = client.discovery(
      new URL(issuer),
      clientId,
      undefined,
      client.None(),
    )
  }
  return configPromise
}

export interface LoginChallenge {
  url: string
  state: string
  codeVerifier: string
}

export async function buildLoginUrl(redirectUri: string): Promise<LoginChallenge> {
  const config = await getOidcConfig()
  const { scopes } = authConfig()
  const codeVerifier = client.randomPKCECodeVerifier()
  const codeChallenge = await client.calculatePKCECodeChallenge(codeVerifier)
  const state = client.randomState()

  const url = client.buildAuthorizationUrl(config, {
    redirect_uri: redirectUri,
    scope: scopes,
    code_challenge: codeChallenge,
    code_challenge_method: 'S256',
    state,
  })

  return { url: url.href, state, codeVerifier }
}

export async function completeLogin(params: {
  currentUrl: URL
  codeVerifier: string
  state: string
}): Promise<client.TokenEndpointResponse> {
  const config = await getOidcConfig()
  return client.authorizationCodeGrant(config, params.currentUrl, {
    pkceCodeVerifier: params.codeVerifier,
    expectedState: params.state,
  })
}

export async function refresh(
  refreshToken: string,
): Promise<client.TokenEndpointResponse> {
  const config = await getOidcConfig()
  return client.refreshTokenGrant(config, refreshToken)
}

export async function buildLogoutUrl(
  idToken: string | undefined,
  postLogoutRedirectUri: string,
): Promise<string | undefined> {
  const config = await getOidcConfig()
  const meta = config.serverMetadata()
  if (!meta.end_session_endpoint) return undefined
  const url = client.buildEndSessionUrl(config, {
    post_logout_redirect_uri: postLogoutRedirectUri,
    ...(idToken ? { id_token_hint: idToken } : {}),
  })
  return url.href
}

export function toSessionTokens(
  tokens: client.TokenEndpointResponse,
): SessionTokens {
  const expiresInSec = tokens.expires_in ?? 3600
  return {
    accessToken: tokens.access_token,
    refreshToken: tokens.refresh_token,
    idToken: tokens.id_token,
    expiresAt: Date.now() + expiresInSec * 1000,
  }
}

const KNOWN_ROLES: readonly Role[] = [
  'player',
  'organizer',
  'tenant_admin',
  'platform_admin',
]

// Zitadel legger prosjektroller i access-token-claimet
// `urn:zitadel:iam:org:project:roles` (map: role -> {orgId: domain}). Vi leser
// derfor rollene fra access-token, likt Go-tjenestene (pkg/authn).
export function deriveUser(tokens: client.TokenEndpointResponse): SessionUser {
  const idClaims = readJwtClaims(tokens.id_token)
  const accessClaims = readJwtClaims(tokens.access_token)

  const roleClaim = accessClaims['urn:zitadel:iam:org:project:roles']
  const roles = KNOWN_ROLES.filter(
    (r) =>
      typeof roleClaim === 'object' &&
      roleClaim !== null &&
      r in (roleClaim as Record<string, unknown>),
  )

  const name =
    asString(idClaims.name) ??
    asString(idClaims.preferred_username) ??
    asString(idClaims.email) ??
    asString(idClaims.sub) ??
    'ukjent'

  return {
    sub: asString(idClaims.sub) ?? asString(accessClaims.sub) ?? '',
    name,
    email: asString(idClaims.email),
    org: asString(accessClaims['urn:zitadel:iam:user:resourceowner:id']),
    roles,
  }
}

function asString(v: unknown): string | undefined {
  return typeof v === 'string' ? v : undefined
}

// Dekoder JWT-payload uten signaturverifisering. Trygt her: token kommer rett
// fra token-endepunktet over TLS, vi bruker det kun til visning/rolleutledning.
function readJwtClaims(token: string | undefined): Record<string, unknown> {
  if (!token) return {}
  const parts = token.split('.')
  if (parts.length < 2) return {}
  try {
    const payload = parts[1]!.replace(/-/g, '+').replace(/_/g, '/')
    const json = Buffer.from(payload, 'base64').toString('utf8')
    return JSON.parse(json) as Record<string, unknown>
  } catch {
    return {}
  }
}
