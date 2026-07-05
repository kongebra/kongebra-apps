import { useSession } from '@tanstack/react-start/server'

import { sessionPassword } from '@/lib/config'

// Rollene fra Zitadel-prosjektet (SPEC §6). Speiler pkg/authn i Go-tjenestene.
export type Role = 'player' | 'organizer' | 'tenant_admin' | 'platform_admin'

export interface SessionUser {
  sub: string
  name: string
  email?: string
  org?: string
  roles: Role[]
}

export interface SessionTokens {
  accessToken: string
  refreshToken?: string
  idToken?: string
  // Absolutt utløp i epoch-millisekunder.
  expiresAt: number
}

// Kortlevd tilstand som holdes mellom /auth/login og /auth/callback.
export interface OAuthFlowState {
  state: string
  codeVerifier: string
  returnTo: string
}

export interface AppSessionData {
  oauth?: OAuthFlowState
  tokens?: SessionTokens
  user?: SessionUser
}

const SESSION_NAME = 'tl-session'

// Forseglet, httpOnly session-cookie (iron under panseret). Rå tokens forlater
// aldri serveren - klienten ser kun avledet brukertilstand via currentUser.
export function getAppSession() {
  return useSession<AppSessionData>({
    name: SESSION_NAME,
    password: sessionPassword(),
    cookie: {
      httpOnly: true,
      sameSite: 'lax',
      secure: process.env.NODE_ENV === 'production',
      path: '/',
    },
  })
}
