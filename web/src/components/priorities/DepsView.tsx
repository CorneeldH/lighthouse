import { type JSX, type MouseEvent, useMemo, useRef, useState } from 'react'
import type { GNode, NodeKey } from '@/lib/priorities/derive'
import { NO_MILESTONE_COLOR } from '@/lib/priorities/palette'
import { COL_GAP, NODE_H, NODE_W, edgePath, layoutGraph } from './layout'
import type { ViewProps } from './types'

const columnName = (depth: number): string =>
  depth === 0 ? 'Can start now' : `Waits on ${depth} ${depth === 1 ? 'step' : 'steps'}`

// Every node reachable over the edges in one direction, the start node excluded.
function reach(start: NodeKey, next: Map<NodeKey, NodeKey[]>): Set<NodeKey> {
  const seen = new Set<NodeKey>()
  const todo = [start]
  while (todo.length > 0) {
    for (const w of next.get(todo.pop() as NodeKey) ?? []) {
      if (!seen.has(w)) {
        seen.add(w)
        todo.push(w)
      }
    }
  }
  return seen
}

export function DepsView({ graph, selected, colors }: ViewProps): JSX.Element {
  const [hover, setHover] = useState<NodeKey | null>(null)
  const [pinned, setPinned] = useState<NodeKey | null>(null)
  const pointer = useRef('mouse')

  const model = useMemo(() => {
    const all = [...graph.nodes.values()]
    const related = new Set<NodeKey>()
    for (const e of graph.edges) {
      related.add(e.from)
      related.add(e.to)
    }
    const inView = (n: GNode) => selected.has(n.repo) && !n.external
    // Dependents come from the filtered repos only; their blockers come along wherever they live.
    const edges = graph.edges.filter((e) => {
      const to = graph.nodes.get(e.to)
      return to !== undefined && inView(to)
    })
    const shown = new Set<NodeKey>(all.filter((n) => inView(n) && related.has(n.key)).map((n) => n.key))
    for (const e of edges) shown.add(e.from)
    const nodes = all.filter((n) => shown.has(n.key))
    const loose = all.filter((n) => inView(n) && !related.has(n.key))
    const down = new Map<NodeKey, NodeKey[]>()
    const up = new Map<NodeKey, NodeKey[]>()
    for (const e of edges) {
      down.set(e.from, [...(down.get(e.from) ?? []), e.to])
      up.set(e.to, [...(up.get(e.to) ?? []), e.from])
    }
    return { nodes, edges, loose, down, up, laid: layoutGraph(nodes, edges) }
  }, [graph, selected])

  const focus = pinned ?? hover
  const chain = useMemo(
    () => (focus === null ? null : new Set([focus, ...reach(focus, model.down), ...reach(focus, model.up)])),
    [focus, model],
  )
  const prefix = selected.size > 1
  const pos = new Map(model.laid.nodes.map((n) => [n.key, n]))
  const depths = [...new Set(model.nodes.map((n) => n.depth))].sort((a, b) => a - b)
  const { loose } = model

  const onClick = (e: MouseEvent, key: NodeKey) => {
    e.stopPropagation()
    // Touch: the first tap lights the chain, the second opens the issue.
    if (pointer.current === 'touch' && pinned !== key) {
      e.preventDefault()
      setPinned(key)
    }
  }

  return (
    <div data-testid="view-deps" className="space-y-4">
      {model.nodes.length === 0 ? (
        <p className="text-sm text-muted-foreground">No open dependencies between these issues.</p>
      ) : (
        <div data-testid="graph-scroll" className="overflow-x-auto pb-2" onClick={() => setPinned(null)}>
          <div className="relative" style={{ width: model.laid.width, height: model.laid.height + 4 }}>
            {depths.map((d) => (
              <div
                key={d}
                className="absolute top-0 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground"
                style={{ left: d * (NODE_W + COL_GAP) }}
              >
                {columnName(d)}
              </div>
            ))}
            <svg
              className="pointer-events-none absolute inset-0 overflow-visible"
              width={model.laid.width}
              height={model.laid.height + 4}
              aria-hidden="true"
            >
              <defs>
                {[
                  ['ah', '#a3a3a3'],
                  ['ahh', 'var(--lh-navy)'],
                ].map(([id, fill]) => (
                  <marker key={id} id={id} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto">
                    <path d="M0,1L9,5L0,9z" fill={fill} stroke="none" />
                  </marker>
                ))}
              </defs>
              {model.laid.edges.map((e) => {
                const on = chain === null || (chain.has(e.from) && chain.has(e.to))
                return (
                  <path
                    key={`${e.from}>${e.to}`}
                    data-from={e.from}
                    data-to={e.to}
                    data-dimmed={on ? 'false' : 'true'}
                    d={edgePath(e.points)}
                    fill="none"
                    stroke={chain !== null && on ? 'var(--lh-navy)' : '#c4c4c4'}
                    strokeWidth={chain !== null && on ? 2.2 : 1.6}
                    opacity={on ? 1 : 0.15}
                    markerEnd={chain !== null && on ? 'url(#ahh)' : 'url(#ah)'}
                  />
                )
              })}
            </svg>
            {model.nodes.map((n) => {
              const p = pos.get(n.key)
              if (!p) return null
              const grey = n.external || !selected.has(n.repo)
              const color =
                grey || !n.milestone ? NO_MILESTONE_COLOR : (colors.get(`${n.repo}#m${n.milestone.number}`) ?? NO_MILESTONE_COLOR)
              const dimmed = chain !== null && !chain.has(n.key)
              return (
                <a
                  key={n.key}
                  href={grey && n.url === '' ? undefined : n.url}
                  target="_blank"
                  rel="noreferrer"
                  data-testid={`node-${n.key}`}
                  data-external={grey ? 'true' : 'false'}
                  data-dimmed={dimmed ? 'true' : 'false'}
                  data-depth={n.depth}
                  data-cycle={n.cycle ? 'true' : 'false'}
                  className={[
                    'absolute flex items-center gap-2 overflow-hidden rounded-lg bg-white pr-2.5 text-foreground no-underline transition-opacity dark:bg-card',
                    n.active ? 'border-2 border-dashed border-lh-navy' : grey ? 'border border-dashed border-lh-grey' : 'border border-border',
                    dimmed ? 'opacity-25' : '',
                    chain?.has(n.key) && n.key !== focus ? 'shadow-[0_0_0_2px_var(--lh-navy)]' : '',
                  ].join(' ')}
                  style={{ left: p.x, top: p.y, width: NODE_W, height: NODE_H }}
                  onPointerDown={(e) => {
                    pointer.current = e.pointerType
                  }}
                  onPointerEnter={(e) => {
                    // A real mouse takes over from a touch pin, or the pin would shadow the hover.
                    if (e.pointerType !== 'touch') setPinned(null)
                  }}
                  onMouseEnter={() => setHover(n.key)}
                  onMouseLeave={() => setHover(null)}
                  onClick={(e) => onClick(e, n.key)}
                >
                  <span className="w-1.5 flex-none self-stretch" style={{ background: color }} />
                  <span className="flex-none text-[13px] font-semibold">
                    {prefix || grey ? `${n.repo}#${n.number}` : `#${n.number}`}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={n.title}>
                    {n.title}
                  </span>
                  {n.cycle && <span className="flex-none text-[10.5px] font-semibold text-muted-foreground">cycle</span>}
                  {n.active && (
                    <span className="flex-none rounded-full bg-lh-yellow px-1.5 text-[10.5px] font-semibold text-lh-navy">
                      picked up
                    </span>
                  )}
                  {n.waiting >= 2 && (
                    <span
                      data-testid="waiting-pill"
                      title={`${n.waiting} issues wait on this`}
                      className="flex-none rounded-full bg-lh-navy px-1.5 text-[11px] font-semibold text-white"
                    >
                      {n.waiting}
                    </span>
                  )}
                </a>
              )
            })}
          </div>
        </div>
      )}
      <div className="border-t border-border pt-3">
        <h4 className="mb-2 text-[13px] font-semibold">No open dependencies ({loose.length})</h4>
        <div className="flex flex-wrap gap-1.5">
          {loose.map((n) => {
            const color = n.milestone ? (colors.get(`${n.repo}#m${n.milestone.number}`) ?? NO_MILESTONE_COLOR) : NO_MILESTONE_COLOR
            return (
              <a
                key={n.key}
                href={n.url}
                target="_blank"
                rel="noreferrer"
                data-testid={`loose-${n.key}`}
                title={n.title}
                className="inline-flex items-center gap-1.5 overflow-hidden rounded-md border border-border bg-white py-0.5 pr-2 text-xs text-foreground no-underline dark:bg-card"
              >
                <span className="w-[5px] self-stretch" style={{ background: color }} />
                <b className="font-semibold">{prefix ? `${n.repo}#${n.number}` : `#${n.number}`}</b>
              </a>
            )
          })}
        </div>
      </div>
    </div>
  )
}
