import { createFileRoute, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { Pencil, Plus, Trash2, X } from 'lucide-react'

import {
  createPerson,
  deletePerson,
  listPersons,
  updatePerson,
} from '@/lib/api/roster'
import { errorMessage } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import type { Person } from '@/lib/api/types'

export const Route = createFileRoute('/t/$slug/organize/roster')({
  loader: ({ context }) => listPersons({ data: context.tenant.id }),
  component: RosterAdmin,
})

function RosterAdmin() {
  const persons = Route.useLoaderData()
  const { tenant } = Route.useRouteContext()
  const router = useRouter()

  const [editing, setEditing] = useState<Person | null>(null)
  const [name, setName] = useState('')
  const [department, setDepartment] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function reset() {
    setEditing(null)
    setName('')
    setDepartment('')
    setError(null)
  }

  function startEdit(p: Person) {
    setEditing(p)
    setName(p.name)
    setDepartment(p.department ?? '')
    setError(null)
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setPending(true)
    setError(null)
    const input = {
      name: name.trim(),
      department: department.trim() || null,
      avatar_url: null,
    }
    try {
      if (editing) {
        await updatePerson({
          data: { tenantId: tenant.id, id: editing.id, input },
        })
      } else {
        await createPerson({ data: { tenantId: tenant.id, input } })
      }
      reset()
      await router.invalidate()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPending(false)
    }
  }

  async function remove(p: Person) {
    if (!confirm(`Slette ${p.name}?`)) return
    try {
      await deletePerson({ data: { tenantId: tenant.id, id: p.id } })
      if (editing?.id === p.id) reset()
      await router.invalidate()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[20rem_1fr]">
      <Card className="h-fit">
        <CardHeader>
          <CardTitle className="text-base">
            {editing ? 'Rediger person' : 'Ny person'}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="name">Navn</Label>
              <Input
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Ola Nordmann"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="department">Avdeling (valgfritt)</Label>
              <Input
                id="department"
                value={department}
                onChange={(e) => setDepartment(e.target.value)}
                placeholder="Utvikling"
              />
            </div>
            {error ? (
              <p className="text-destructive text-sm" role="alert">
                {error}
              </p>
            ) : null}
            <div className="flex gap-2">
              <Button type="submit" disabled={pending || !name.trim()}>
                {editing ? (
                  'Lagre'
                ) : (
                  <>
                    <Plus className="size-4" />
                    Legg til
                  </>
                )}
              </Button>
              {editing ? (
                <Button type="button" variant="ghost" onClick={reset}>
                  <X className="size-4" />
                  Avbryt
                </Button>
              ) : null}
            </div>
          </form>
        </CardContent>
      </Card>

      <div>
        {persons.length === 0 ? (
          <EmptyState
            title="Tomt roster"
            description="Legg til den første personen med skjemaet."
          />
        ) : (
          <div className="rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Navn</TableHead>
                  <TableHead>Avdeling</TableHead>
                  <TableHead className="w-24 text-right">Handling</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {persons.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="font-medium">{p.name}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {p.department ?? '-'}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => startEdit(p)}
                          aria-label={`Rediger ${p.name}`}
                        >
                          <Pencil className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => remove(p)}
                          aria-label={`Slett ${p.name}`}
                        >
                          <Trash2 className="text-destructive size-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
    </div>
  )
}
