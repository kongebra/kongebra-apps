import { expect, test } from '@playwright/test'

test('landing rendrer og har login-lenke med returnTo', async ({ page }) => {
  await page.goto('/')
  await expect(
    page.getByRole('heading', { name: /Trønder/i }),
  ).toBeVisible()
  await expect(page.getByPlaceholder('miljø-slug')).toBeVisible()

  const login = page.getByRole('link', { name: /Logg inn/i })
  await expect(login).toBeVisible()
  await expect(login).toHaveAttribute('href', /\/auth\/login\?returnTo=/)
})

test('anonymt offentlig scoreboard rendrer via BFF', async ({ page }) => {
  await page.goto('/t/demo')
  // Tenant-navn fra platform-slug-oppslaget.
  await expect(
    page.getByRole('heading', { name: 'Demo AS' }),
  ).toBeVisible()
  // Game-kort fra competition.
  await expect(page.getByText('Fredagsquiz')).toBeVisible()

  // Inn på scoreboardet.
  await page.getByText('Fredagsquiz').click()
  await expect(page).toHaveURL(/\/t\/demo\/games\//)

  // Navn er slått opp mot roster (join i BFF).
  await expect(page.getByText('Kari Nordmann')).toBeVisible()
  await expect(page.getByText('Ola Nordmann')).toBeVisible()

  // Ties: to deltakere deler 2. plass -> to rank-merker med "2".
  const twos = page.locator('[aria-label="plassering 2"]')
  await expect(twos).toHaveCount(2)
})

test('ukjent tenant gir 404-side', async ({ page }) => {
  await page.goto('/t/finnes-ikke')
  await expect(page.getByText('Fant ikke siden')).toBeVisible()
})

test('healthz svarer ok', async ({ request }) => {
  const res = await request.get('/healthz')
  expect(res.status()).toBe(200)
  expect(await res.json()).toEqual({ status: 'ok' })
})
