import type { JSX } from 'react'
import { forgeErrorText, type TelemetryFilter } from '@/lib/api'
import { useTelemetryCoverage } from '@/lib/queries'

// A null count is "not measured" with its note or reason, never 0.
export function CoverageView({ filter }: { filter: TelemetryFilter }): JSX.Element {
  const q = useTelemetryCoverage(filter)
  const lines = q.data?.lines ?? []
  return (
    <div className="space-y-3">
      {q.error !== null && q.error !== undefined && <p role="alert">{forgeErrorText(q.error)}</p>}
      {(q.isPending || q.isPlaceholderData) && <p>Loading…</p>}
      {q.data?.not_migrated && <p>database not migrated</p>}
      {q.data && !q.data.not_migrated && (
        <ul className="space-y-1 text-sm">
          {lines.map((l) => (
            <li key={l.key} className="flex flex-wrap gap-x-2">
              <span className="font-medium">{l.reason}</span>
              {l.count === null ? (
                <span className="text-muted-foreground">
                  <span>not measured</span> <span className="text-xs">{l.note || l.reason}</span>
                </span>
              ) : (
                <span>{l.count}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
