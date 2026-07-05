import { Link } from '@tanstack/react-router'

import { Button } from '@/components/ui/button'

export function NotFound() {
  return (
    <div className="mx-auto flex min-h-[60vh] max-w-md flex-col items-center justify-center gap-4 px-6 text-center">
      <p className="text-primary text-sm font-semibold tracking-widest uppercase">
        404
      </p>
      <h1 className="text-2xl font-bold tracking-tight">Fant ikke siden</h1>
      <p className="text-muted-foreground text-sm">
        Siden finnes ikke, eller så er lenka utdatert.
      </p>
      <Button asChild>
        <Link to="/">Til forsiden</Link>
      </Button>
    </div>
  )
}
