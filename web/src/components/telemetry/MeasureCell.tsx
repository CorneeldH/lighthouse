import type { JSX } from 'react'
import type { Measure } from '@/api/lighthouse'

const MIN_N = 5
// The reason Go gives a review measure outside a review block (report.ReasonNotApplicable).
const NOT_APPLICABLE = 'not applicable'

interface MeasureCellProps {
  measure: Measure
  format?: (value: number) => string
  // What n counts; trajectories unless the measure counts reviews or findings.
  unit?: string
}

// A null value is "not measured", never 0, or "n/a" when the measure does not apply; fewer
// than MIN_N units is shown grey with a note.
export function MeasureCell({ measure, format, unit = 'trajectories' }: MeasureCellProps): JSX.Element {
  const { value, n, reason } = measure
  if (value === null) {
    return (
      <span className="text-muted-foreground">
        <span title={reason}>{reason === NOT_APPLICABLE ? 'n/a' : 'not measured'}</span> <span className="text-xs">n={n}</span>
      </span>
    )
  }
  const text = format ? format(value) : String(value)
  const few = n < MIN_N
  return (
    <span>
      <span className={few ? 'text-muted-foreground' : undefined}>{text}</span>
      {' '}
      <span className="text-xs text-muted-foreground">n={n}</span>
      {few && <span className="block text-xs text-muted-foreground">too few {unit}</span>}
    </span>
  )
}
