import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { ArrowRight, Trophy } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export const Route = createFileRoute('/')({
  component: Landing,
})

function Landing() {
  const navigate = useNavigate()
  const [slug, setSlug] = useState('')

  function open(e: React.FormEvent) {
    e.preventDefault()
    const clean = slug.trim().toLowerCase()
    if (clean) navigate({ to: '/t/$slug', params: { slug: clean } })
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-16 sm:px-6 sm:py-24">
      <div className="flex flex-col items-center text-center">
        <span className="bg-accent text-accent-foreground ring-primary/20 mb-6 inline-flex items-center gap-2 rounded-full px-3 py-1 text-xs font-medium ring-1 ring-inset">
          <Trophy className="size-3.5" />
          Turneringer for miljøet ditt
        </span>
        <h1 className="text-4xl font-bold tracking-tight sm:text-5xl">
          Trønder<span className="text-primary">Leikan</span>
        </h1>
        <p className="text-muted-foreground mt-4 max-w-xl text-balance">
          Quiz, dart, sim-racing og rebus - roster, resultater og live
          scoreboards, samlet ett sted. Offentlige tavler er åpne for alle.
        </p>

        <form
          onSubmit={open}
          className="mt-10 flex w-full max-w-md items-center gap-2"
        >
          <div className="flex flex-1 items-center rounded-md border pl-3 focus-within:ring-[3px] focus-within:ring-ring/50">
            <span className="text-muted-foreground text-sm select-none">
              /t/
            </span>
            <Input
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="miljø-slug"
              aria-label="Tenant-slug"
              className="border-0 pl-1 shadow-none focus-visible:ring-0"
            />
          </div>
          <Button type="submit">
            Åpne
            <ArrowRight className="size-4" />
          </Button>
        </form>
        <p className="text-muted-foreground mt-3 text-xs">
          Skriv inn miljøets slug for å se de offentlige tavlene.
        </p>
      </div>
    </div>
  )
}
