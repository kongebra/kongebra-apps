import { createFileRoute } from '@tanstack/react-router'

import { completeLogin, deriveUser, toSessionTokens } from '@/lib/auth/oidc'
import { getAppSession } from '@/lib/auth/session'
import { callbackUri } from '@/lib/auth/origin'
import { withBase } from '@/lib/basepath'

// GET /admin/auth/callback -> bytter authorization code mot tokens, forsegler dem
// i sessionen, og sender brukeren tilbake dit hen kom fra (alltid under /admin).
export const Route = createFileRoute('/auth/callback')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const session = await getAppSession()
        const flow = session.data.oauth
        if (!flow) {
          return redirectTo(withBase('/?auth=error'))
        }

        // Rekonstruer den offentlige callback-URL-en så den matcher redirect_uri.
        const incoming = new URL(request.url)
        const currentUrl = new URL(callbackUri(request))
        currentUrl.search = incoming.search

        try {
          const tokens = await completeLogin({
            currentUrl,
            codeVerifier: flow.codeVerifier,
            state: flow.state,
          })
          await session.update({
            tokens: toSessionTokens(tokens),
            user: deriveUser(tokens),
            oauth: undefined,
          })
        } catch {
          await session.update({ oauth: undefined })
          return redirectTo(withBase('/?auth=error'))
        }

        return redirectTo(flow.returnTo)
      },
    },
  },
})

function redirectTo(location: string): Response {
  return new Response(null, { status: 302, headers: { Location: location } })
}
