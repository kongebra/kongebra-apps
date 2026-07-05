// Server-only konfigurasjon. Alt leses fra env - issuer/klient-id/URL-er
// hardkodes ALDRI (SPEC §5). Denne fila importeres kun fra server-kode
// (server functions / server routes), aldri fra klientkomponenter.

function required(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`Mangler påkrevd miljøvariabel: ${name}`)
  }
  return value
}

function optional(name: string, fallback: string): string {
  return process.env[name] ?? fallback
}

// Admin-planet snakker kun med platform-tjenesten (SPEC §7/§10): tenant-registry
// + Zitadel-provisjonering. BFF-en kaller den interne URL-en direkte.
export function serviceUrls() {
  return {
    platform: required('PLATFORM_URL').replace(/\/$/, ''),
  }
}

// OIDC/Zitadel-oppsett for admin-klienten. EGEN Zitadel-app (egen AUTH_CLIENT_ID),
// separat fra web (SPEC §10). Authorization Code + PKCE, public client.
export function authConfig() {
  return {
    issuer: required('AUTH_ISSUER').replace(/\/$/, ''),
    clientId: required('AUTH_CLIENT_ID'),
    // Audience for API-tokens. Zitadel: prosjekt-id-scope avgjør at access-token
    // får med prosjektroller. Wiring verifiseres ende-til-ende i 1.6 mot seed.
    audience: process.env.AUTH_AUDIENCE ?? '',
    scopes: optional('AUTH_SCOPES', 'openid profile email offline_access'),
    // Valgfri eksplisitt base-URL for redirect_uri. Utledes ellers fra requesten.
    baseUrl: process.env.ADMIN_BASE_URL?.replace(/\/$/, '') ?? '',
  }
}

export function sessionPassword(): string {
  const secret = required('SESSION_SECRET')
  if (secret.length < 32) {
    throw new Error('SESSION_SECRET må være minst 32 tegn')
  }
  return secret
}
