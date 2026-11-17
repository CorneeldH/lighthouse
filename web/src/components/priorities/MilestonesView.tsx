import type { JSX } from 'react'
import type { ViewProps } from './types'

// Placeholder; the real view replaces it.
export function MilestonesView({ selected }: ViewProps): JSX.Element {
  return <div data-testid="view-milestones" data-repos={selected.size} />
}
