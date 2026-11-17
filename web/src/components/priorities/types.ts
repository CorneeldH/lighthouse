import type { Repo } from '@/api/lighthouse'
import type { Graph } from '@/lib/priorities/derive'

// Shared by DepsView, MilestonesView and UnassignedView.
export interface ViewProps {
  graph: Graph
  repos: Repo[]
  selected: Set<string>
  // Key "<repo>#m<nr>" -> colour.
  colors: Map<string, string>
}
