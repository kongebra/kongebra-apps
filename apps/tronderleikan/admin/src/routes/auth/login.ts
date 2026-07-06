import { createFileRoute } from '@tanstack/react-router'

import { buildLoginUrl } from '@/lib/auth/oidc'
import { getAppSession } from '@/lib/auth/session'
import { callbackUri } from '@/lib/auth/origin'
import { BASE_PATH } from '@/lib/basepath'

// GET /admin/auth/login?returnTo=... -> starter Authorization Code + PKCE mot
// Zitadel. Ligger utenfor /api (Traefik ruter /api til Go-tjenestene, SPEC §10).
export const Route = createFileRoute('/auth/login')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const url = new URL(request.url)
        const returnTo = safeReturnTo(url.searchParams.get('returnTo'))

        const challenge = await buildLoginUrl(callbackUri(request))
        const session = await getAppSession()
        await session.update({
          oauth: {
            state: challenge.state,
            codeVerifier: challenge.codeVerifier,
            returnTo,
          },
        })

        return new Response(null, {
          status: 302,
          headers: { Location: challenge.url },
        })
      },
    },
  },
})

// Kun interne stier under /admin tillates som returnTo (unngå open redirect og
// hopp ut av admin-planet). Vi parser mot et dummy-opphav, krever samme opphav,
// og krever at den PARSEDE stien starter med basepath. Vi returnerer den parsede
// stien (ikke rå input), så det ikke finnes noe parser-differential mellom denne
// sjekken og browserens tolkning av Location.
function safeReturnTo(value: string | null): string {
  const fallback = `${BASE_PATH}/`
  if (!value) return fallback
  try {
    const base = 'http://localhost'
    const u = new URL(value, base)
    if (u.origin !== base) return fallback
    const rel = `${u.pathname}${u.search}${u.hash}`
    return rel.startsWith(`${BASE_PATH}/`) || rel === BASE_PATH ? rel : fallback
  } catch {
    return fallback
  }
}
