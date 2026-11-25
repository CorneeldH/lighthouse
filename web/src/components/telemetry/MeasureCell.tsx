import type { JSX } from 'react'
import type { Measure } from '@/api/lighthouse'

const MIN_N = 5

interface MeasureCellProps {
  measure: Measure
  format?: (value: number) => string
}

// A null value is "not measured", never 0; fewer than MIN_N trajectories is shown grey with a note.
export function MeasureCell({ measure, format }: MeasureCellProps): JSX.Element {
  const { value, n, reason } = measure
  if (value === null) {
    return (
      <span className="text-muted-foreground">
        <span title={reason}>not measured</span> <span className="text-xs">n={n}</span>
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
      {few && <span className="block text-xs text-muted-foreground">too few trajectories</span>}
    </span>
  )
}
