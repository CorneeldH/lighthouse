import type { JSX } from 'react'
import type { ViewId } from '@/lib/priorities/prefs'
import { Block, type BlockState } from './Block'

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
}

const STATES: Array<{ key: string; label: string; state: BlockState; needsHuman: boolean }> = [
  { key: 'done', label: 'done', state: 'done', needsHuman: false },
  { key: 'active', label: 'active', state: 'active', needsHuman: false },
  { key: 'ready', label: 'ready', state: 'ready', needsHuman: false },
  { key: 'needs-human', label: 'needs a human', state: 'ready', needsHuman: true },
  { key: 'blocked', label: 'blocked', state: 'blocked', needsHuman: false },
]

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
      <ul aria-label="Legend" className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
        {legend.map((l) => (
          <li key={l.key} className="inline-flex items-center gap-1.5">
            <i aria-hidden className="inline-block size-3 rounded-sm" style={{ background: l.color }} />
            {l.label}
          </li>
        ))}
        {STATES.map((st) => (
          <li key={st.key} data-testid="legend-state" className="inline-flex items-center gap-1.5">
            <Block
              testId="legend-block"
              state={st.state}
              needsHuman={st.needsHuman}
              color="var(--lh-grey)"
              title={st.label}
              className="size-3.5"
            />
            {st.label}
          </li>
        ))}
        <li data-testid="legend-shaping" className="inline-flex items-center gap-1.5">
          <i aria-hidden className="inline-block h-3 w-3.5 rounded-sm outline-[1.5px] outline-dashed outline-lh-grey" />
          not shaped yet
        </li>
      </ul>
    </div>
  )
}
