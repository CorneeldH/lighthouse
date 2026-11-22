import type { Repo } from '@/api/lighthouse'
import type { Graph, MilestoneRow } from '@/lib/priorities/derive'

// Shared by DepsView, MilestonesView and UnassignedView.
export interface ViewProps {
  graph: Graph
  repos: Repo[]
  selected: Set<string>
  // Key "<repo>#m<nr>" -> colour.
  colors: Map<string, string>
  // MilestonesView only: the order of the rows, and in 'priority' the up/down buttons.
  order?: 'computed' | 'priority'
  onMove?: (row: MilestoneRow, dir: 'up' | 'down') => void
  // Repos with a rank write in flight: their buttons are off.
  pendingRepos?: Set<string>
}
