import type { JSX } from 'react'
import type { ViewId } from '@/lib/priorities/prefs'
import type { DepsOrder } from './layout'
import type { IssueStateName } from '@/lib/priorities/derive'
import { Block } from './Block'

export interface LegendItem {
  key: string
  label: string
  color: string
}

interface ToolbarProps {
  view: ViewId
  onView: (v: ViewId) => void
  counts: Record<ViewId, number>
  repos: string[]
  selected: Set<string>
  onToggleRepo: (slug: string) => void
  legend: LegendItem[]
  msOrder: 'computed' | 'priority'
  onMsOrder: (o: 'computed' | 'priority') => void
  depsOrder: DepsOrder
  onDepsOrder: (o: DepsOrder) => void
}

const STATES: Array<{ key: IssueStateName; label: string }> = [
  { key: 'needs_mind', label: 'needs mind' },
  { key: 'ready', label: 'to do' },
  { key: 'active', label: 'active' },
  { key: 'needs_hands', label: 'needs hands' },
  { key: 'done', label: 'done' },
]

const UNDERLINED: ReadonlySet<IssueStateName> = new Set(['needs_mind', 'needs_hands'])

const HEADING = 'text-[0.65rem] font-semibold uppercase tracking-wider text-muted-foreground'

const VIEWS: Array<{ id: ViewId; label: string }> = [
  { id: 'deps', label: 'Dependencies' },
  { id: 'milestones', label: 'Milestones' },
  { id: 'unassigned', label: 'Without milestone' },
]

// The computed order sorts on % done; its button shows only the sign, its name says it in full.
const ORDERS: Array<{ id: 'computed' | 'priority'; label: string; name: string }> = [
  { id: 'computed', label: '%', name: '% done' },
  { id: 'priority', label: 'Priority', name: 'Priority' },
]

const SORTS: Array<{ id: DepsOrder; label: string }> = [
  { id: 'prio', label: 'Prio-weight' },
  { id: 'weight', label: 'Weight' },
  { id: 'crossings', label: 'Fewest crossings' },
]

export function Toolbar({
  view,
  onView,
  counts,
  repos,
  selected,
  onToggleRepo,
  legend,
  msOrder,
  onMsOrder,
  depsOrder,
  onDepsOrder,
}: ToolbarProps): JSX.Element {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-4">
        <div role="group" aria-label="View" className="inline-flex overflow-hidden rounded-md border border-lh-navy">
          {VIEWS.map((v) => {
            const on = v.id === view
            return (
              <button
                key={v.id}
                type="button"
                aria-pressed={on}
                className={`px-3 py-1 text-sm font-medium ${on ? 'bg-lh-navy text-white' : 'bg-card text-foreground hover:bg-muted'}`}
                onClick={() => onView(v.id)}
              >
                {v.label} <span className="opacity-70">{counts[v.id]}</span>
              </button>
            )
          })}
        </div>
        {view === 'milestones' && (
          <div role="group" aria-label="Order" className="inline-flex overflow-hidden rounded-md border border-lh-navy">
            {ORDERS.map((o) => {
              const on = o.id === msOrder
              return (
                <button
                  key={o.id}
                  type="button"
                  aria-pressed={on}
                  aria-label={o.name}
                  title={o.name}
                  className={`px-3 py-1 text-sm font-medium ${on ? 'bg-lh-navy text-white' : 'bg-card text-foreground hover:bg-muted'}`}
                  onClick={() => onMsOrder(o.id)}
                >
                  {o.label}
                </button>
              )
            })}
          </div>
        )}
        {view === 'deps' && (
          <div role="group" aria-label="Sort" className="inline-flex overflow-hidden rounded-md border border-lh-navy">
            {SORTS.map((o) => {
              const on = o.id === depsOrder
              return (
                <button
                  key={o.id}
                  type="button"
                  aria-pressed={on}
                  className={`px-3 py-1 text-sm font-medium ${on ? 'bg-lh-navy text-white' : 'bg-card text-foreground hover:bg-muted'}`}
                  onClick={() => onDepsOrder(o.id)}
                >
                  {o.label}
                </button>
              )
            })}
          </div>
        )}
        <div role="group" aria-label="Repos" className="flex flex-wrap gap-2">
          {repos.map((slug) => {
            const on = selected.has(slug)
            return (
              <button
                key={slug}
                type="button"
                aria-pressed={on}
                className={`rounded-full border px-3 py-0.5 text-xs font-medium ${
                  on ? 'border-lh-navy bg-lh-navy text-white' : 'border-border bg-card text-muted-foreground'
                }`}
                onClick={() => onToggleRepo(slug)}
              >
                {slug}
              </button>
            )
          })}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-xs">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className={HEADING}>Milestones</span>
          <ul aria-label="Milestones" className="flex flex-wrap items-center gap-x-4 gap-y-1">
            {legend.map((l) => (
              <li key={l.key} className="inline-flex items-center gap-1.5">
                <i aria-hidden className="inline-block size-3 rounded-sm" style={{ background: l.color }} />
                {l.label}
              </li>
            ))}
          </ul>
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className={HEADING}>Status</span>
          <ul aria-label="Status" className="flex flex-wrap items-center gap-x-4 gap-y-1">
            {STATES.map((st) => (
              <li key={st.key} data-testid="legend-state" className="inline-flex items-center gap-1.5">
                <Block
                  testId="legend-block"
                  state={st.key}
                  color="var(--lh-grey)"
                  title={st.label}
                  className="size-3.5"
                />
                <span
                  data-underlined={UNDERLINED.has(st.key) ? 'true' : undefined}
                  className={UNDERLINED.has(st.key) ? 'underline underline-offset-2 decoration-2' : undefined}
                >
                  {st.label}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  )
}
