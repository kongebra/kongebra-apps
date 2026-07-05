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

// Kun interne stier tillates som returnTo (unngå open redirect).
function safeReturnTo(value: string | null): string {
  if (value && value.startsWith('/') && !value.startsWith('//')) return value
  return '/'
}
