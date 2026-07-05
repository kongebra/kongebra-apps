import { createServerFn } from '@tanstack/react-start'

import { serviceUrls } from '@/lib/config'
import { serviceFetch } from '@/lib/api/http'
import { str } from '@/lib/api/validate'
import type { PublicTenant } from '@/lib/api/types'

// Anonymt slug-oppslag (SPEC §10): web utleder tenant-UUID fra slug uten token.
export const getTenantBySlug = createServerFn({ method: 'GET' })
  .validator((slug: unknown) => str(slug, 'slug'))
  .handler(async ({ data: slug }): Promise<PublicTenant> => {
    const { platform } = serviceUrls()
    return serviceFetch<PublicTenant>(
      platform,
      `/api/platform/tenants/by-slug/${encodeURIComponent(slug)}`,
      { auth: 'anon' },
    )
  })
