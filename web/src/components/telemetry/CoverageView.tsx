import type { JSX } from 'react'
import { forgeErrorText, type TelemetryFilter } from '@/lib/api'
import { useTelemetryCoverage } from '@/lib/queries'

// Go's notes of an unmeasured line start with "not measured"; the view says that once.
const NOT_MEASURED = /^not measured[;:,]?\s*/

// A null count is "not measured" with the rest of its note, never 0.
export function CoverageView({ filter }: { filter: TelemetryFilter }): JSX.Element {
  const q = useTelemetryCoverage(filter)
  const lines = q.data?.lines ?? []
  return (
    <div className="space-y-3">
      {q.error !== null && q.error !== undefined && <p role="alert">{forgeErrorText(q.error)}</p>}
      {(q.isPending || q.isPlaceholderData) && <p>Loading…</p>}
      {q.data?.not_migrated && (
        <p>
          database not migrated
          {lines[0]?.note && <span className="ml-2 text-xs text-muted-foreground">{lines[0].note}</span>}
        </p>
      )}
      {q.data && !q.data.not_migrated && lines.length === 0 && <p>No coverage lines</p>}
      {q.data && !q.data.not_migrated && lines.length > 0 && (
        <ul className="space-y-1 text-sm">
          {lines.map((l) => (
            <li key={l.key} className="flex flex-wrap gap-x-2">
              <span className="font-medium">{l.reason}</span>
              {l.count === null ? (
                <span className="text-muted-foreground">
                  <span>not measured</span>
                  {l.note.replace(NOT_MEASURED, '') !== '' && <span className="ml-1 text-xs">{l.note.replace(NOT_MEASURED, '')}</span>}
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
