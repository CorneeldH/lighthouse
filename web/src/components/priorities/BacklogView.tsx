import type { JSX } from 'react'
import { MilestoneTitle } from './MilestoneTitle'
import { withoutMilestone, type UnassignedRow } from '@/lib/priorities/derive'
import { NO_MILESTONE_COLOR } from '@/lib/priorities/palette'
import { Block } from './Block'
import type { ViewProps } from './types'

function relationText(row: UnassignedRow): string | null {
  if (row.relation === null) return null
  return row.relation === 'blocks' ? `blocks ${row.relatedNumber}` : `waits on ${row.relatedNumber}`
}

function Row({ row, colors }: { row: UnassignedRow; colors: ViewProps['colors'] }): JSX.Element {
  const { node } = row
  const relation = relationText(row)
  const underlined = node.state === 'needs_mind' || node.state === 'needs_hands'
  // A closed milestone keeps its colour; the rest is grey.
  const color = node.milestone ? (colors.get(`${node.repo}#m${node.milestone.number}`) ?? NO_MILESTONE_COLOR) : NO_MILESTONE_COLOR
  return (
    <li
      data-testid="backlog-row"
      data-number={node.number}
      className="flex items-center gap-3 rounded-lg border px-4 py-2.5"
    >
      <Block
        testId="backlog-status"
        className="size-3.5 flex-none"
        state={node.state}
        blocked={node.blocked}
        color={color}
        title={node.title}
      />
      <a
        data-underlined={underlined ? 'true' : undefined}
        href={node.url}
        target="_blank"
        rel="noreferrer"
        className={`shrink-0 font-semibold text-lh-link hover:underline${underlined ? ' underline underline-offset-2 decoration-2' : ''}`}
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

export function BacklogView({ graph, repos, selected, colors }: ViewProps): JSX.Element {
  const rows = withoutMilestone(repos, graph).filter((r) => selected.has(r.node.repo))
  return (
    <div data-testid="view-backlog" className="w-full">
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No open issues in the backlog.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((row) => (
            <Row key={row.node.key} row={row} colors={colors} />
          ))}
        </ul>
      )}
    </div>
  )
}
