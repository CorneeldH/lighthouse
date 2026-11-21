import { useLayoutEffect, useRef, useState, type JSX } from 'react'
import {
  milestoneRows,
  sortMilestoneRows,
  type Block as BlockData,
  type GNode,
  type MilestoneRow,
} from '@/lib/priorities/derive'
import { NO_MILESTONE_COLOR } from '@/lib/priorities/palette'
import { Block, type BlockState } from './Block'
import type { ViewProps } from './types'

// Below this width a block shows no number; the hover title stays.
export const MIN_LABEL_WIDTH = 28
const BAR_GAP = 2

// One fixed width for every outside-blocker column, so all bars start at the same x. The column
// holds at most MAX_OUTSIDE blocks; the rest collapse into one "+N" block.
const OUTSIDE_COL = 'w-24 shrink-0 flex justify-end items-center gap-0.5'
const MAX_OUTSIDE = 3

const colorOf = (colors: Map<string, string>, n: GNode): string =>
  n.milestone ? (colors.get(`${n.repo}#m${n.milestone.number}`) ?? NO_MILESTONE_COLOR) : NO_MILESTONE_COLOR

const isOutside = (row: MilestoneRow, b: { repo: string; nr: number }): boolean =>
  row.outside.some((n) => n.repo === b.repo && n.number === b.nr)

// "needs human" is a ready (or active) block with the dashed flag.
function shapeOf(state: BlockData['state'], needsHuman: boolean): { state: BlockState; needsHuman: boolean } {
  return state === 'needs_human' ? { state: 'ready', needsHuman: true } : { state, needsHuman }
}

function meta(row: MilestoneRow, own: BlockData[]): string {
  const active = own.filter((b) => b.state === 'active').length
  const notYet = own.filter((b) => b.state !== 'active' && b.state !== 'done').length
  const base = `${row.done} done · ${active} active · ${notYet} not yet`
  const n = row.outside.length
  return n === 0 ? base : `${base} · ${n} outside blocker${n === 1 ? '' : 's'}`
}

function useWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const measure = () => setWidth(el.getBoundingClientRect().width)
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, width]
}

function Row({ row, colors }: { row: MilestoneRow; colors: Map<string, string> }): JSX.Element {
  const color = colors.get(`${row.repo}#m${row.number}`) ?? NO_MILESTONE_COLOR
  const [barRef, barWidth] = useWidth<HTMLDivElement>()
  // Outside blockers have their own column; the bar holds the milestone's own blocks.
  const own = row.blocks.filter((b) => !isOutside(row, b))
  const blockWidth = own.length === 0 ? 0 : (barWidth - BAR_GAP * (own.length - 1)) / own.length
  const showLabel = blockWidth >= MIN_LABEL_WIDTH
  const todo = own.filter((b) => b.state !== 'done')
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
          <small className="block text-xs font-normal text-muted-foreground">{meta(row, own)}</small>
        </div>
      </div>
      <div>
        <div className="flex items-center gap-2">
          <div data-testid="outside-col" className={OUTSIDE_COL}>
            {(row.outside.length > MAX_OUTSIDE ? row.outside.slice(0, MAX_OUTSIDE - 1) : row.outside).map((n) => (
              <Block
                key={n.key}
                testId="outside"
                {...shapeOf(n.state, n.needsHuman)}
                color={colorOf(colors, n)}
                borderColor={colorOf(colors, n)}
                title={`#${n.number} ${n.title}`}
                href={n.url || undefined}
                label={n.number}
                className="h-[18px] min-w-[26px] px-0.5"
              />
            ))}
            {row.outside.length > MAX_OUTSIDE && (
              <span
                data-testid="outside-more"
                title={row.outside
                  .slice(MAX_OUTSIDE - 1)
                  .map((n) => `#${n.number} ${n.title}`)
                  .join('\n')}
                className="flex h-[18px] min-w-[26px] items-center justify-center rounded border-[1.5px] border-lh-grey px-0.5 text-[9.5px] font-bold text-lh-grey"
              >
                +{row.outside.length - (MAX_OUTSIDE - 1)}
              </span>
            )}
          </div>
          <div
            ref={barRef}
            data-testid="bar"
            data-shaping={row.shaping ? 'true' : 'false'}
            className={`relative flex h-[18px] flex-1 gap-0.5 ${
              row.shaping ? 'rounded-sm outline-[1.5px] outline-dashed outline-offset-2 outline-lh-grey' : ''
            }`}
          >
            {own.length === 0 ? (
              <span data-testid="no-issues" className="text-xs text-muted-foreground">
                nog geen issues
              </span>
            ) : (
              own.map((b) => (
                <Block
                  key={`${b.repo}#${b.nr}`}
                  {...shapeOf(b.state, b.needsHuman)}
                  color={color}
                  title={`#${b.nr} ${b.title}`}
                  href={b.url || undefined}
                  label={showLabel ? b.nr : undefined}
                  className="h-full min-w-[3px] flex-1"
                />
              ))
            )}
          </div>
        </div>
        {todo.length > 0 && (
          <div data-testid="todo-list" className="mt-2 flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
            to do:
            {todo.map((b) => (
              <b key={`${b.repo}#${b.nr}`} className="font-semibold">
                #{b.nr}
              </b>
            ))}
          </div>
        )}
      </div>
      <div className="text-right">
        <b className={`text-[22px] font-bold ${row.shaping ? 'text-muted-foreground' : ''}`}>
          {Math.round(row.percent * 100)}%
        </b>
        <small className="block text-[11.5px] text-muted-foreground">
          {row.shaping ? 'voorlopig' : `${row.open} to do`}
        </small>
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
