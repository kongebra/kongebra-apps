import { cn } from '@/lib/utils'

// Plasseringsmerke. Gull/sølv/bronse for topp 3, nøytralt ellers. Ties deler
// samme rank og får derfor samme merke (SPEC §2/§3).
export function RankBadge({ rank }: { rank: number }) {
  const podium =
    rank === 1
      ? 'bg-gold/20 text-foreground ring-gold/50'
      : rank === 2
        ? 'bg-silver/25 text-foreground ring-silver/60'
        : rank === 3
          ? 'bg-bronze/20 text-foreground ring-bronze/50'
          : 'bg-muted text-muted-foreground ring-border'

  return (
    <span
      className={cn(
        'inline-flex size-8 items-center justify-center rounded-full text-sm font-bold tabular-nums ring-1 ring-inset',
        podium,
      )}
      aria-label={`plassering ${rank}`}
    >
      {rank}
    </span>
  )
}
