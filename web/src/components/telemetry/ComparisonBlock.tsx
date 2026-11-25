import type { JSX } from 'react'
import type { Block, Outside } from '@/api/lighthouse'
import { MeasureCell } from './MeasureCell'

const OUTSIDE_LABELS: [keyof Outside, string][] = [
  ['before_cohort', 'before cohort'],
  ['mixed_lane', 'mixed lane'],
  ['no_lane', 'no lane'],
  ['no_recovery', 'no recovery'],
  ['running', 'running'],
]

function outsideLine(outside: Outside): string | null {
  const parts = OUTSIDE_LABELS.filter(([k]) => outside[k] > 0).map(([k, label]) => `${outside[k]} ${label}`)
  return parts.length === 0 ? null : `Outside the comparison: ${parts.join(', ')}`
}

const percent = (v: number): string => `${Math.round(v * 100)}%`
const hours = (v: number): string => `${v.toFixed(1)} h`
const cost = (v: number): string => v.toFixed(2)

export function ComparisonBlock({ block }: { block: Block }): JSX.Element {
  const outside = outsideLine(block.outside)
  const groups = block.groups ?? []
  return (
    <section aria-label={block.name} className="space-y-2 rounded-md border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-medium">{block.name}</h3>
        <span className="rounded-full bg-lh-navy px-2 py-0.5 text-xs font-medium text-white ring-1 ring-border">{block.label}</span>
      </div>
      <p className="text-sm text-muted-foreground">{block.label_note}</p>
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
                <th scope="col">Questions</th>
                <th scope="col">Active</th>
                <th scope="col">Human wait</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <tr key={g.name} className="align-top">
                  <th scope="row">{g.name}</th>
                  <td><MeasureCell measure={g.found} /></td>
                  <td><MeasureCell measure={g.adopted} format={percent} /></td>
                  <td>
                    <MeasureCell measure={g.cost} format={cost} />
                    <span className="block text-xs text-muted-foreground">
                      {g.cost_kind}
                      {g.cost_kind === 'mixed' && (
                        <span role="img" aria-label="warning" title="Cost mixes measured and estimated values" className="ml-1 text-amber-600">
                          ⚠
                        </span>
                      )}
                    </span>
                  </td>
                  <td><MeasureCell measure={g.recovered} /></td>
                  <td><MeasureCell measure={g.questions} /></td>
                  <td><MeasureCell measure={g.active} format={hours} /></td>
                  <td><MeasureCell measure={g.human_wait} format={hours} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {outside !== null && <p className="text-sm text-muted-foreground">{outside}</p>}
    </section>
  )
}
