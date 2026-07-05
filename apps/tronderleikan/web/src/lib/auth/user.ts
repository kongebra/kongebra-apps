import { createServerFn } from '@tanstack/react-start'

import { getAppSession } from '@/lib/auth/session'
import type { SessionUser } from '@/lib/auth/session'

// Klient-synlig brukertilstand. Rå tokens eksponeres aldri - kun sub/navn/roller.
export const fetchCurrentUser = createServerFn({ method: 'GET' }).handler(
  async (): Promise<SessionUser | null> => {
    const session = await getAppSession()
    return session.data.user ?? null
  },
)
