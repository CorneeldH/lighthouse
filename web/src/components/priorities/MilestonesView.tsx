import type { JSX } from 'react'
import { milestoneRows, sortMilestoneRows, type GNode, type MilestoneRow } from '@/lib/priorities/derive'
import { NO_MILESTONE_COLOR } from '@/lib/priorities/palette'
import type { ViewProps } from './types'

const HATCH = 'repeating-linear-gradient(135deg, #e5e5e5 0 6px, #f7f7f7 6px 12px)'
const YELLOW_HATCH = 'repeating-linear-gradient(135deg, #facc15 0 4px, #fef9c3 4px 8px)'
// One fixed width for every outside-blocker column, so all bars start at the same x.
const OUTSIDE_COL = 'w-16 shrink-0 flex justify-end items-center gap-0.5'

const colorOf = (colors: Map<string, string>, n: GNode): string =>
  n.milestone ? (colors.get(`${n.repo}#m${n.milestone.number}`) ?? NO_MILESTONE_COLOR) : NO_MILESTONE_COLOR

function meta(row: MilestoneRow): string {
  const base = `${row.done} done · ${row.active.length} active · ${row.todo.length} not yet`
  const n = row.outside.length
  return n === 0 ? base : `${base} · ${n} outside blocker${n === 1 ? '' : 's'}`
}

function Row({ row, colors }: { row: MilestoneRow; colors: Map<string, string> }): JSX.Element {
  const color = colors.get(`${row.repo}#m${row.number}`) ?? NO_MILESTONE_COLOR
  const unshaped = row.empty
  const provisional = row.shaping && !unshaped
  return (
    <div
      data-testid={`ms-${row.repo}#m${row.number}`}
      className="grid grid-cols-[240px_1fr_110px] items-center gap-4 rounded-lg border p-4"
    >
      <div className="flex items-start gap-2.5 font-semibold">
        <i
          data-testid="swatch"
          className="mt-0.5 size-3.5 shrink-0 rounded-sm"
          style={{ background: color }}
        />
        <div>
          {row.title}
          <small className="block text-xs font-normal text-muted-foreground">{meta(row)}</small>
        </div>
      </div>
      <div>
        <div className="flex items-center gap-2">
          <div data-testid="outside-col" className={OUTSIDE_COL}>
            {row.outside.map((n) => {
              const c = colorOf(colors, n)
              return (
                <span
                  key={n.key}
                  data-testid="outside"
                  title={`#${n.number} ${n.title} blocks from outside`}
                  className="flex h-[18px] min-w-[26px] items-center justify-center rounded border-[1.5px] border-dashed px-0.5 text-[9.5px] font-bold"
                  style={{ borderColor: c, color: c, background: n.active ? '#facc15' : undefined }}
                >
                  {n.number}
                </span>
              )
            })}
          </div>
          {unshaped ? (
            <div
              data-testid="unshaped"
              data-hatched="true"
              className="flex h-[18px] flex-1 items-center justify-center rounded text-[11.5px] font-semibold text-muted-foreground"
              style={{ background: HATCH }}
            >
              Not shaped yet — the percentage says nothing
            </div>
          ) : (
            <div className="relative flex h-[18px] flex-1 gap-0.5">
              {Array.from({ length: row.done }, (_, i) => (
                <i
                  key={`done-${i}`}
                  data-testid="seg"
                  data-state="done"
                  title="done"
                  className="block min-w-[3px] flex-1 rounded-sm"
                  style={{ background: color }}
                />
              ))}
              {row.active.map((n) => (
                <i
                  key={n.key}
                  data-testid="seg"
                  data-state="active"
                  data-hatched={n.uphill ? 'true' : 'false'}
                  title={`#${n.number} ${n.title}`}
                  className="block min-w-[3px] flex-1 rounded-sm"
                  style={{ background: n.uphill ? YELLOW_HATCH : '#facc15' }}
                />
              ))}
              {row.todo.map((n) => (
                <i
                  key={n.key}
                  data-testid="seg"
                  data-state="todo"
                  title={`#${n.number} ${n.title}`}
                  className="block min-w-[3px] flex-1 rounded-sm bg-[#ececec]"
                />
              ))}
              {provisional && (
                <em
                  data-testid="shaping-layer"
                  className="pointer-events-none absolute -inset-[3px] rounded-[5px] border-[1.5px] border-dashed border-[#a3a3a3]"
                  style={{
                    background: 'repeating-linear-gradient(135deg,#ffffffc8 0 6px,#ffffff30 6px 12px)',
                  }}
                />
              )}
            </div>
          )}
        </div>
        {!unshaped && row.order.length > 0 && (
          <div data-testid="todo-list" className="mt-2 flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
            to do:
            {row.order.map((n) => (
              <b key={n.key} className="font-semibold">
                #{n.number}
              </b>
            ))}
          </div>
        )}
      </div>
      <div className="text-right">
        {unshaped ? (
          <>
            <b className="text-base text-muted-foreground">—</b>
            <small className="block text-[11.5px] text-muted-foreground">shape first</small>
          </>
        ) : (
          <>
            <b className={`text-[22px] font-bold ${provisional ? 'text-muted-foreground' : ''}`}>
              {Math.round((row.percent ?? 0) * 100)}%
            </b>
            <small className="block text-[11.5px] text-muted-foreground">
              {provisional ? `provisional · ${row.open} to do` : `${row.open} to do`}
            </small>
          </>
        )}
      </div>
    </div>
  )
}

export function MilestonesView({ graph, repos, selected, colors }: ViewProps): JSX.Element {
  const rows = sortMilestoneRows(
    milestoneRows(
      repos.filter((r) => selected.has(r.slug)),
      graph,
    ),
  )
  return (
    <div data-testid="view-milestones" className="flex flex-col gap-3">
      {rows.map((row) => (
        <Row key={`${row.repo}#m${row.number}`} row={row} colors={colors} />
      ))}
    </div>
  )
}
