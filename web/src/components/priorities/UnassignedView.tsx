import type { JSX } from 'react'
import { MilestoneTitle } from './MilestoneTitle'
import { withoutMilestone, type UnassignedRow } from '@/lib/priorities/derive'
import type { ViewProps } from './types'

function relationText(row: UnassignedRow): string | null {
  if (row.relation === null) return null
  return row.relation === 'blocks' ? `blocks ${row.relatedNumber}` : `waits on ${row.relatedNumber}`
}

function Row({ row }: { row: UnassignedRow }): JSX.Element {
  const { node } = row
  const relation = relationText(row)
  return (
    <li
      data-testid="unassigned-row"
      data-number={node.number}
      className="flex items-center gap-3 rounded-lg border px-4 py-2.5"
    >
      <a
        href={node.url}
        target="_blank"
        rel="noreferrer"
        className="shrink-0 font-semibold text-lh-link hover:underline"
      >
        {node.number}
      </a>
      <span className="min-w-0 flex-1 truncate">{node.title}</span>
      {row.closedMilestone !== null && (
        <span className="shrink-0 text-xs text-muted-foreground"><MilestoneTitle title={row.closedMilestone.title} url={row.closedMilestone.url} /> · closed</span>
      )}
      {relation !== null && (
        <span
          className="shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold"
          style={{ background: 'var(--lh-blue)', color: '#fff' }}
        >
          {relation}
        </span>
      )}
    </li>
  )
}

export function UnassignedView({ graph, repos, selected }: ViewProps): JSX.Element {
  const rows = withoutMilestone(repos, graph).filter((r) => selected.has(r.node.repo))
  return (
    <div data-testid="view-unassigned" className="w-full">
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No open issues without a milestone.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((row) => (
            <Row key={row.node.key} row={row} />
          ))}
        </ul>
      )}
    </div>
  )
}
