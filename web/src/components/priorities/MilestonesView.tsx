import { ChevronDown, ChevronUp } from 'lucide-react'
import { useLayoutEffect, useRef, useState, type JSX } from 'react'
import {
  milestoneRows,
  placeInRepo,
  priorityOrder,
  sortMilestoneRows,
  type Block as BlockData,
  type GNode,
  type MilestoneRow,
} from '@/lib/priorities/derive'
import { canMove } from '@/lib/priorities/moves'
import { NO_MILESTONE_COLOR } from '@/lib/priorities/palette'
import { Block } from './Block'
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

function meta(row: MilestoneRow, own: BlockData[]): string {
  const active = own.filter((b) => b.state === 'active').length
  const notYet = own.filter((b) => b.state !== 'active' && b.state !== 'done').length
  const base = `${row.done} done · ${active} active · ${notYet} not yet`
  const n = row.outside.length
  return n === 0 ? base : `${base} · ${n} outside blocker${n === 1 ? '' : 's'}`
}

// Up/down state of one row in Priority mode; absent in Computed mode.
interface Moves {
  place: number | null
  up: boolean
  down: boolean
  onMove: (dir: 'up' | 'down') => void
}

const MOVE_BTN =
  'flex size-5 items-center justify-center rounded border border-border text-muted-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-40'

function MoveButtons({ moves, title }: { moves: Moves; title: string }): JSX.Element {
  return (
    <div className="flex shrink-0 flex-col gap-0.5">
      <button
        type="button"
        aria-label={`${title} up`}
        title="Up"
        className={MOVE_BTN}
        disabled={!moves.up}
        onClick={() => moves.onMove('up')}
      >
        <ChevronUp aria-hidden className="size-3.5" />
      </button>
      <button
        type="button"
        aria-label={`${title} down`}
        title="Down"
        className={MOVE_BTN}
        disabled={!moves.down}
        onClick={() => moves.onMove('down')}
      >
        <ChevronDown aria-hidden className="size-3.5" />
      </button>
    </div>
  )
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

function Row({ row, colors, moves }: { row: MilestoneRow; colors: Map<string, string>; moves?: Moves }): JSX.Element {
  const color = colors.get(`${row.repo}#m${row.number}`) ?? NO_MILESTONE_COLOR
  const [barRef, barWidth] = useWidth<HTMLDivElement>()
  // Outside blockers have their own column (row.outside); the bar holds the milestone's own blocks.
  const own = row.blocks
  const blockWidth = own.length === 0 ? 0 : (barWidth - BAR_GAP * (own.length - 1)) / own.length
  const showLabel = blockWidth >= MIN_LABEL_WIDTH
  const todo = own.filter((b) => b.state !== 'done')
  return (
    <div
      data-testid={`ms-${row.repo}#m${row.number}`}
      className="grid grid-cols-[240px_1fr_110px] items-center gap-4 rounded-lg border p-4"
    >
      <div className="flex items-start gap-2.5 font-semibold">
        {moves && <MoveButtons moves={moves} title={row.title} />}
        <i
          data-testid="swatch"
          className="mt-0.5 size-3.5 shrink-0 rounded-sm"
          style={{ background: color }}
        />
        <div>
          {row.title}
          {moves && moves.place !== null && (
            <>
              <small
                data-testid="place"
                aria-hidden
                title="place in the repo"
                className="ml-1.5 align-super text-[10px] font-semibold text-muted-foreground"
              >
                {moves.place}
              </small>
              <span className="sr-only">{`, place ${moves.place}`}</span>
            </>
          )}
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
                state={n.state}
                blocked={n.blocked}
                color={colorOf(colors, n)}
                borderColor={colorOf(colors, n)}
                title={`${n.number} ${n.title}`}
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
                  .map((n) => `${n.number} ${n.title}`)
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
                no issues yet
              </span>
            ) : (
              own.map((b) => (
                <Block
                  key={`${b.repo}#${b.nr}`}
                  state={b.state}
                  blocked={b.blocked}
                  color={color}
                  title={`${b.nr} ${b.title}`}
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
                {b.nr}
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
          {row.shaping ? 'provisional' : `${row.open} to do`}
        </small>
      </div>
    </div>
  )
}

export function MilestonesView({
  graph,
  repos,
  selected,
  colors,
  order = 'computed',
  onMove,
  pendingRepos,
}: ViewProps): JSX.Element {
  const computed = sortMilestoneRows(
    milestoneRows(
      repos.filter((r) => selected.has(r.slug)),
      graph,
    ),
  )
  const priority = order === 'priority'
  const rows = priority ? priorityOrder(computed) : computed
  // The repo filter hides whole repos, so the rows of a shown repo are all its milestones.
  const byRepo = new Map<string, MilestoneRow[]>()
  if (priority) {
    for (const r of rows) {
      const l = byRepo.get(r.repo) ?? []
      l.push(r)
      byRepo.set(r.repo, l)
    }
  }
  const movesOf = (row: MilestoneRow): Moves | undefined => {
    if (!priority) return undefined
    const own = byRepo.get(row.repo) as MilestoneRow[]
    const free = onMove !== undefined && !(pendingRepos?.has(row.repo) ?? false)
    return {
      place: placeInRepo(own, row.repo, row.number),
      up: free && canMove(own, row.number, 'up'),
      down: free && canMove(own, row.number, 'down'),
      onMove: (dir) => onMove?.(row, dir),
    }
  }
  return (
    <div data-testid="view-milestones" className="flex flex-col gap-3">
      {rows.map((row) => (
        <Row key={`${row.repo}#m${row.number}`} row={row} colors={colors} moves={movesOf(row)} />
      ))}
    </div>
  )
}
