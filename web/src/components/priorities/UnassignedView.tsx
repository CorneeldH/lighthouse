import type { JSX } from 'react'
import type { ViewProps } from './types'

// Placeholder; the real view replaces it.
export function UnassignedView({ selected }: ViewProps): JSX.Element {
  return <div data-testid="view-unassigned" data-repos={selected.size} />
}
