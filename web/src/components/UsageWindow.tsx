import type { JSX } from 'react'
import type { WindowReport } from '@/api/lighthouse'
import { formatAge, formatPct, formatReset } from '@/lib/format'

interface UsageWindowProps {
  label: string
  window: WindowReport
  now: Date
}

export function UsageWindow({ label, window: w, now }: UsageWindowProps): JSX.Element {
  const hasBar = (w.state === 'ok' || w.state === 'stale') && w.pct !== null
  const reset = formatReset(w.reset_at, now)
  // A stale window dims its numbers but not its badge.
  const dim = w.state === 'stale' ? 'opacity-60' : ''
  return (
    <div role="group" aria-label={label} className="space-y-1">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{label}</span>
        {hasBar && (
          <span className={dim} title={w.lower_bound ? 'lower bound' : undefined}>
            {formatPct(w.pct ?? 0, w.lower_bound)}
          </span>
        )}
        {w.state === 'stale' && (
          <span className="rounded bg-amber-100 px-1.5 text-xs font-medium text-amber-900 dark:bg-amber-900/40 dark:text-amber-200">
            stale · {formatAge(w.age_s ?? 0)}
          </span>
        )}
        {w.state === 'not_measurable' && (
          <span className="rounded bg-muted px-1.5 text-xs">not measurable</span>
        )}
      </div>
      {hasBar && (
        <div
          role="progressbar"
          aria-label={label}
          aria-valuenow={w.pct ?? 0}
          aria-valuemin={0}
          aria-valuemax={100}
          className={`h-2 w-full rounded bg-muted ${dim}`}
        >
          <div
            className="h-2 rounded bg-primary"
            style={{ width: `${Math.min(100, Math.max(0, w.pct ?? 0))}%` }}
          />
        </div>
      )}
      {w.state === 'not_measurable' && w.reason !== undefined && (
        <p className="text-sm text-muted-foreground">{w.reason}</p>
      )}
      {w.state !== 'not_measurable' && (
        <p className={`text-sm text-muted-foreground ${dim}`}>
          {reset !== '' && <>resets {reset}</>}
          {w.state === 'ok' && w.age_s !== null && (
            <>
              {reset !== '' && ' · '}measured {formatAge(w.age_s)} ago
            </>
          )}
        </p>
      )}
    </div>
  )
}
