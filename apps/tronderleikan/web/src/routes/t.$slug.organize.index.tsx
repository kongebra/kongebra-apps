import { Link, createFileRoute } from '@tanstack/react-router'
import { ClipboardList, Users } from 'lucide-react'

import { Card, CardContent, CardDescription, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/t/$slug/organize/')({
  component: OrganizeHome,
})

function OrganizeHome() {
  const { slug } = Route.useParams()
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <ActionCard
        to="/t/$slug/organize/roster"
        slug={slug}
        icon={<Users className="size-5" />}
        title="Roster"
        description="Legg til og rediger personer i miljøet."
      />
      <ActionCard
        to="/t/$slug/organize/games"
        slug={slug}
        icon={<ClipboardList className="size-5" />}
        title="Konkurranser"
        description="Opprett Games og punch plasseringer."
      />
    </div>
  )
}

function ActionCard({
  to,
  slug,
  icon,
  title,
  description,
}: {
  to: string
  slug: string
  icon: React.ReactNode
  title: string
  description: string
}) {
  return (
    <Link to={to} params={{ slug }} className="group">
      <Card className="hover:border-primary/50 h-full transition-colors">
        <CardContent className="flex items-start gap-3">
          <div className="bg-accent text-accent-foreground flex size-10 items-center justify-center rounded-md">
            {icon}
          </div>
          <div>
            <CardTitle className="text-base">{title}</CardTitle>
            <CardDescription className="mt-1">{description}</CardDescription>
          </div>
        </CardContent>
      </Card>
    </Link>
  )
}
