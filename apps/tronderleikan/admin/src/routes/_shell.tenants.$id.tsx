import { Link, createFileRoute, notFound, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { ArrowLeft, Save } from 'lucide-react'

import { getTenant, updateTenant } from '@/lib/api/platform'
import { ApiError } from '@/lib/api/errors'
import { errorMessage } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/_shell/tenants/$id')({
  loader: async ({ params }) => {
    try {
      return await getTenant({ data: params.id })
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) throw notFound()
      throw err
    }
  },
  component: TenantDetail,
})

function TenantDetail() {
  const tenant = Route.useLoaderData()
  const router = useRouter()

  const [name, setName] = useState(tenant.name)
  const [publicVisibility, setPublicVisibility] = useState(
    tenant.public_visibility,
  )
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const dirty = name.trim() !== tenant.name || publicVisibility !== tenant.public_visibility

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setPending(true)
    setError(null)
    setSaved(false)
    try {
      await updateTenant({
        data: {
          id: tenant.id,
          input: { name: name.trim(), public_visibility: publicVisibility },
        },
      })
      setSaved(true)
      await router.invalidate()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="space-y-6">
      <div>
        <Link
          to="/"
          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm transition-colors"
        >
          <ArrowLeft className="size-4" />
          Alle tenants
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-bold tracking-tight">{tenant.name}</h1>
          {tenant.public_visibility ? (
            <Badge variant="secondary">Offentlig</Badge>
          ) : (
            <Badge variant="outline">Privat</Badge>
          )}
        </div>
        <p className="text-muted-foreground font-mono text-sm">/t/{tenant.slug}</p>
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Innstillinger</CardTitle>
          </CardHeader>
          <CardContent>
            <form onSubmit={submit} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="name">Miljønavn</Label>
                <Input
                  id="name"
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value)
                    setSaved(false)
                  }}
                  required
                />
              </div>
              <label className="flex items-center gap-2 text-sm font-medium select-none">
                <input
                  type="checkbox"
                  className="accent-primary size-4"
                  checked={publicVisibility}
                  onChange={(e) => {
                    setPublicVisibility(e.target.checked)
                    setSaved(false)
                  }}
                />
                Offentlig innsyn (anonyme kan se scoreboards)
              </label>

              {error ? (
                <p className="text-destructive text-sm" role="alert">
                  {error}
                </p>
              ) : null}
              {saved && !dirty ? (
                <p className="text-primary text-sm">Lagret.</p>
              ) : null}

              <Button type="submit" disabled={pending || !dirty || !name.trim()}>
                <Save className="size-4" />
                {pending ? 'Lagrer...' : 'Lagre'}
              </Button>
            </form>
          </CardContent>
        </Card>

        <Card className="h-fit">
          <CardHeader>
            <CardTitle className="text-base">Metadata</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <Meta label="Tenant-ID" value={tenant.id} mono />
            <Meta label="Zitadel-org" value={tenant.zitadel_org_id} mono />
            <Meta
              label="Opprettet"
              value={tenant.created_at ? tenant.created_at.slice(0, 10) : '-'}
            />
            <Meta
              label="Oppdatert"
              value={tenant.updated_at ? tenant.updated_at.slice(0, 10) : '-'}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function Meta({
  label,
  value,
  mono,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="space-y-0.5">
      <p className="text-muted-foreground text-xs">{label}</p>
      <p className={mono ? 'font-mono text-xs break-all' : ''}>{value}</p>
    </div>
  )
}
