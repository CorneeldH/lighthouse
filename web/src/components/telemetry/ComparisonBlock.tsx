import type { JSX } from 'react'
import type { Block, Group, Outside } from '@/api/lighthouse'
import { hours, money, percent } from './format'
import { MeasureCell } from './MeasureCell'

const OUTSIDE_LABELS: [keyof Outside, string][] = [
  ['before_cohort', 'before cohort'],
  ['mixed_lane', 'mixed lane'],
  ['no_lane', 'no lane'],
  ['no_recovery', 'no recovery'],
  ['running', 'running'],
]

// A review block compares with and without a review; only it has reviews and findings.
const REVIEW_KIND = 'toggle'

function outsideLine(outside: Outside): string | null {
  const parts = OUTSIDE_LABELS.filter(([k]) => outside[k] > 0).map(([k, label]) => `${outside[k]} ${label}`)
  return parts.length === 0 ? null : `Outside the comparison: ${parts.join(', ')}`
}

function sizeLine(g: Group, review: boolean): string {
  return review ? `${g.trajectories} trajectories · ${g.reviews} reviews` : `${g.trajectories} trajectories`
}

function findingsLine(g: Group): string {
  const severities = Object.entries(g.by_severity ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([s, n]) => `${s} ${n}`)
  const split = severities.length > 0 ? ` (${severities.join(', ')})` : ''
  return `${g.name}: ${g.findings} findings${split}, ${g.left_open} left open, ${g.undecided} undecided`
}

const mean = (v: number): string => v.toFixed(1)

export function ComparisonBlock({ block }: { block: Block }): JSX.Element {
  const outside = outsideLine(block.outside)
  const groups = block.groups ?? []
  const review = block.kind === REVIEW_KIND
  return (
    <section aria-label={block.name} className="space-y-2 rounded-md border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-medium">{block.name}</h3>
        {block.label !== '' && (
          <span title="Comparison label" className="rounded-full bg-lh-navy px-2 py-0.5 text-xs font-medium text-white ring-1 ring-border">
            {block.label}
          </span>
        )}
      </div>
      {block.label_note !== '' && <p className="text-sm text-muted-foreground">{block.label_note}</p>}
      {block.kind === 'unmeasured' ? (
        <p>{block.reason}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr>
                <th scope="col">Group</th>
                <th scope="col">Found</th>
                <th scope="col">Adopted</th>
                <th scope="col">Cost</th>
                <th scope="col">Recovered</th>
                <th scope="col">Recovery items</th>
                <th scope="col">Questions</th>
                <th scope="col">Active</th>
                <th scope="col">Human wait</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.name} className="align-top">
                  <th scope="row">
                    {g.name}
                    <span className="block text-xs font-normal text-muted-foreground">{sizeLine(g, review)}</span>
                  </th>
                  <td><MeasureCell measure={g.found} format={percent} unit="reviews" /></td>
                  <td><MeasureCell measure={g.adopted} format={percent} unit="decided findings" /></td>
                  <td>
                    <MeasureCell measure={g.cost} format={money} />
                    <span className="block text-xs text-muted-foreground">
                      {g.cost_kind}
                      {g.cost_kind === 'mixed' && (
                        <span role="img" aria-label="warning" title="Cost mixes measured and estimated values" className="ml-1 text-amber-600">
                          ⚠
                        </span>
                      )}
                    </span>
                  </td>
                  <td><MeasureCell measure={g.recovered} format={percent} /></td>
                  <td><MeasureCell measure={g.recovery_items} format={mean} /></td>
                  <td><MeasureCell measure={g.questions} /></td>
                  <td><MeasureCell measure={g.active} format={hours} /></td>
                  <td><MeasureCell measure={g.human_wait} format={hours} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {review && groups.length > 0 && (
        <ul className="text-sm text-muted-foreground">
          {groups.map((g) => (
            <li key={g.name}>{findingsLine(g)}</li>
          ))}
        </ul>
      )}
      {block.kind !== 'unmeasured' && block.reason && <p className="text-sm text-muted-foreground">{block.reason}</p>}
      {outside !== null && <p className="text-sm text-muted-foreground">{outside}</p>}
    </section>
  )
}
