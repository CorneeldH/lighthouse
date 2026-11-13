import type { JSX, ReactNode } from 'react'
import { formatAgo } from '@/lib/format'
import { useNow } from '@/lib/now'
import { useRuns } from '@/lib/queries'

// The part of the page about the automatic runs, with the age of the last dispatcher tick.
export function ControllerSection({ children }: { children: ReactNode }): JSX.Element {
  const { data: runs } = useRuns()
  const now = useNow()
  const ticks = [...(runs?.runs ?? []), ...(runs?.deferred ?? [])].map((r) => Date.parse(r.at)).filter((t) => !Number.isNaN(t))
  const tickAt = ticks.length === 0 ? 0 : Math.max(...ticks)
  return (
    <section aria-labelledby="controller-heading" className="space-y-6">
      <div className="flex flex-wrap items-baseline justify-between gap-2 border-b-2 border-lh-navy pb-1 dark:border-lh-yellow">
        <h2 id="controller-heading" className="text-lg font-semibold text-lh-navy dark:text-foreground">
          Controller
        </h2>
        {/* No aria-live: the age changes every minute and would be read out each time. */}
        <span className="text-sm">
          {tickAt > 0 && `tick ${formatAgo(now, tickAt)}`}
        </span>
      </div>
      {children}
    </section>
  )
}
