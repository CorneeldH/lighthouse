import type { JSX } from 'react'
import type { ViewProps } from './types'

// Placeholder; the real view replaces it.
export function DepsView({ selected }: ViewProps): JSX.Element {
  return <div data-testid="view-deps" data-repos={selected.size} />
}
