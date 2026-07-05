import { createFileRoute } from '@tanstack/react-router'

import { buildLoginUrl } from '@/lib/auth/oidc'
import { getAppSession } from '@/lib/auth/session'
import { callbackUri } from '@/lib/auth/origin'

// GET /auth/login?returnTo=... -> starter Authorization Code + PKCE mot Zitadel.
// Ligger utenfor /api (Traefik ruter /api til Go-tjenestene, SPEC §10).
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

// Kun interne stier tillates som returnTo (unngå open redirect). Vi parser mot et
// dummy-opphav og krever at det RESOLVER til samme opphav - fanger absolutte URL-er,
// protokoll-relative (//host), og backslash-triks (/\host) som browseren normaliserer.
// Vi returnerer den PARSEDE stien (ikke rå input), så det ikke finnes noe
// parser-differential mellom denne sjekken og browserens tolkning av Location.
function safeReturnTo(value: string | null): string {
  if (!value) return '/'
  try {
    const base = 'http://localhost'
    const u = new URL(value, base)
    if (u.origin !== base) return '/'
    const rel = `${u.pathname}${u.search}${u.hash}`
    return rel.startsWith('/') ? rel : '/'
  } catch {
    return '/'
  }
}
