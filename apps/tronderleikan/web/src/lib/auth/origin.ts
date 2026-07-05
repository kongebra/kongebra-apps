import { authConfig } from '@/lib/config'

// Utleder den offentlige origin-en (scheme://host) requesten kom inn på.
// Bak Traefik er request.url ofte intern http; vi respekterer derfor
// X-Forwarded-* og en eksplisitt WEB_BASE_URL. redirect_uri må matche det som
// ble registrert i Zitadel og brukt i autorisasjonssteget.
export function resolveOrigin(request: Request): string {
  const { baseUrl } = authConfig()
  if (baseUrl) return baseUrl

  const url = new URL(request.url)
  const proto =
    request.headers.get('x-forwarded-proto')?.split(',')[0]?.trim() ??
    url.protocol.replace(':', '')
  const host =
    request.headers.get('x-forwarded-host')?.split(',')[0]?.trim() ??
    request.headers.get('host') ??
    url.host
  return `${proto}://${host}`
}

export function callbackUri(request: Request): string {
  return `${resolveOrigin(request)}/auth/callback`
}
