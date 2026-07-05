import { createFileRoute } from '@tanstack/react-router'

import { buildLogoutUrl } from '@/lib/auth/oidc'
import { getAppSession } from '@/lib/auth/session'
import { resolveOrigin } from '@/lib/auth/origin'

// GET /auth/logout -> tømmer sessionen og sender til Zitadels end-session
// (RP-initiated logout) hvis tilgjengelig, ellers tilbake til forsiden.
export const Route = createFileRoute('/auth/logout')({
  server: {
    handlers: {
      GET: async ({ request }) => {
        const session = await getAppSession()
        const idToken = session.data.tokens?.idToken
        await session.clear()

        const postLogout = `${resolveOrigin(request)}/`
        const logoutUrl = await buildLogoutUrl(idToken, postLogout)

        return new Response(null, {
          status: 302,
          headers: { Location: logoutUrl ?? '/' },
        })
      },
    },
  },
})
