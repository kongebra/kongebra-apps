import { Link, createFileRoute, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { Building2, Check, Plus, X } from 'lucide-react'

import { createTenant, listTenants } from '@/lib/api/platform'
import { errorMessage } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { EmptyState } from '@/components/empty-state'

export const Route = createFileRoute('/_shell/')({
  loader: () => listTenants(),
  component: TenantsOverview,
})

// URL-trygg slug utledet fra navn (matcher platform sitt slugPattern: a-z, 0-9,
// enkelt-bindestrek). Kun et forslag - arrangøren kan overstyre.
function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/æ/g, 'ae')
    .replace(/ø/g, 'o')
    .replace(/å/g, 'a')
    .normalize('NFD')
    .replace(/\p{Diacritic}/gu, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function TenantsOverview() {
  const tenants = Route.useLoaderData()
  const router = useRouter()
  const [open, setOpen] = useState(false)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Tenants</h1>
          <p className="text-muted-foreground text-sm">
            Alle miljøer på plattformen. Provisjoner nye og administrer synlighet.
          </p>
        </div>
        <Button onClick={() => setOpen((v) => !v)} variant={open ? 'ghost' : 'default'}>
          {open ? (
            <>
              <X className="size-4" />
              Avbryt
            </>
          ) : (
            <>
              <Plus className="size-4" />
              Ny tenant
            </>
          )}
        </Button>
      </div>

      {open ? (
        <ProvisionForm
          onDone={async () => {
            setOpen(false)
            await router.invalidate()
          }}
        />
      ) : null}

      {tenants.length === 0 ? (
        <EmptyState
          title="Ingen tenants ennå"
          description="Provisjoner det første miljøet for å komme i gang."
        />
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Navn</TableHead>
                <TableHead>Slug</TableHead>
                <TableHead>Synlighet</TableHead>
                <TableHead>Opprettet</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tenants.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">
                    <Link
                      to="/tenants/$id"
                      params={{ id: t.id }}
                      className="hover:text-primary inline-flex items-center gap-2 transition-colors"
                    >
                      <Building2 className="text-muted-foreground size-4" />
                      {t.name}
                    </Link>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs">
                    /t/{t.slug}
                  </TableCell>
                  <TableCell>
                    {t.public_visibility ? (
                      <Badge variant="secondary">Offentlig</Badge>
                    ) : (
                      <Badge variant="outline">Privat</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-sm">
                    {t.created_at ? t.created_at.slice(0, 10) : '-'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function ProvisionForm({ onDone }: { onDone: () => void | Promise<void> }) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugEdited, setSlugEdited] = useState(false)
  const [publicVisibility, setPublicVisibility] = useState(true)
  const [email, setEmail] = useState('')
  const [givenName, setGivenName] = useState('')
  const [familyName, setFamilyName] = useState('')
  const [password, setPassword] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function onNameChange(value: string) {
    setName(value)
    if (!slugEdited) setSlug(slugify(value))
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setPending(true)
    setError(null)
    try {
      await createTenant({
        data: {
          name: name.trim(),
          slug: slug.trim(),
          public_visibility: publicVisibility,
          admin: {
            email: email.trim(),
            given_name: givenName.trim(),
            family_name: familyName.trim(),
            password,
          },
        },
      })
      await onDone()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPending(false)
    }
  }

  const complete =
    name.trim() &&
    slug.trim() &&
    email.trim() &&
    givenName.trim() &&
    familyName.trim() &&
    password.length >= 8

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Provisjoner ny tenant</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-6">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="name">Miljønavn</Label>
              <Input
                id="name"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder="Inmeta AS"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="slug">Slug</Label>
              <Input
                id="slug"
                value={slug}
                onChange={(e) => {
                  setSlugEdited(true)
                  setSlug(e.target.value)
                }}
                placeholder="inmeta"
                pattern="[a-z0-9]+(-[a-z0-9]+)*"
                title="Kun a-z, 0-9 og enkelt-bindestrek"
                required
              />
              <p className="text-muted-foreground text-xs">
                Brukes i URL-er: /t/{slug || 'slug'}
              </p>
            </div>
          </div>

          <label className="flex items-center gap-2 text-sm font-medium select-none">
            <input
              type="checkbox"
              className="accent-primary size-4"
              checked={publicVisibility}
              onChange={(e) => setPublicVisibility(e.target.checked)}
            />
            Offentlig innsyn (anonyme kan se scoreboards)
          </label>

          <div className="space-y-3">
            <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
              Første org-admin
            </p>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="given_name">Fornavn</Label>
                <Input
                  id="given_name"
                  value={givenName}
                  onChange={(e) => setGivenName(e.target.value)}
                  placeholder="Ola"
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="family_name">Etternavn</Label>
                <Input
                  id="family_name"
                  value={familyName}
                  onChange={(e) => setFamilyName(e.target.value)}
                  placeholder="Nordmann"
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="email">E-post</Label>
                <Input
                  id="email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="ola@inmeta.no"
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="password">Midlertidig passord</Label>
                <Input
                  id="password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="Minst 8 tegn"
                  minLength={8}
                  required
                />
              </div>
            </div>
          </div>

          {error ? (
            <p className="text-destructive text-sm" role="alert">
              {error}
            </p>
          ) : null}

          <div className="flex gap-2">
            <Button type="submit" disabled={pending || !complete}>
              <Check className="size-4" />
              {pending ? 'Provisjonerer...' : 'Provisjoner'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
