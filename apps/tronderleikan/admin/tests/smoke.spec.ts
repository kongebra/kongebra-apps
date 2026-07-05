import { expect, test } from '@playwright/test'

// Admin-planet er 100% gatet på platform_admin (SPEC §6). Uten ekte Zitadel kan
// vi ikke forge en innlogget sesjon, så røyken verifiserer det som ER
// bakgrunns-fritt meningsfullt: at gaten sender anonyme til /admin/auth/login
// (den bindende sikkerhetsegenskapen), og at basePath /admin virker (ruter +
// assets + healthz + scoping). Selve OIDC-token-utvekslingen (openid-client mot
// ekte Zitadel over HTTPS) og innlogget tenant-liste/provisjonering hører til
// full E2E i arbeidspakke 1.6.

test('anonym på /admin/ redirectes til /admin/auth/login med returnTo', async ({
  request,
}) => {
  const res = await request.get('/admin/', { maxRedirects: 0 })
  expect([302, 307]).toContain(res.status())
  const loc = res.headers()['location'] ?? ''
  expect(loc).toContain('/admin/auth/login')
  expect(loc).toContain('returnTo=')
})

test('anonym på tenant-detalj er også gatet', async ({ request }) => {
  const res = await request.get('/admin/tenants/some-id', { maxRedirects: 0 })
  expect([302, 307]).toContain(res.status())
  expect(res.headers()['location'] ?? '').toContain('/admin/auth/login')
})

test('healthz svarer ok under basePath', async ({ request }) => {
  const res = await request.get('/admin/healthz')
  expect(res.status()).toBe(200)
  expect(await res.json()).toEqual({ status: 'ok' })
})

test('appen er scopet til /admin (rot-sti treffer den ikke)', async ({
  request,
}) => {
  const res = await request.get('/healthz', { maxRedirects: 0 })
  expect(res.status()).not.toBe(200)
})
