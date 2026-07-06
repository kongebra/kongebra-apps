import { authConfig } from '@/lib/config'
import { withBase } from '@/lib/basepath'

// Utleder den offentlige origin-en (scheme://host) requesten kom inn på.
// Bak Traefik er request.url ofte intern http; vi respekterer derfor
// X-Forwarded-* og en eksplisitt ADMIN_BASE_URL. redirect_uri må matche det som
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

// callback_uri ligger under /admin (SPEC §10). Denne må registreres som
// redirect_uri på admin-appen i Zitadel: https://leikan.newb.no/admin/auth/callback.
export function callbackUri(request: Request): string {
  return `${resolveOrigin(request)}${withBase('/auth/callback')}`
}

// post-logout-URL: tilbake til admin-forsiden (ikke web-forsiden), delt origin.
export function postLogoutUri(request: Request): string {
  return `${resolveOrigin(request)}${withBase('/')}`
}
