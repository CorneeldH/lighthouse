import type { CSSProperties, JSX, MouseEventHandler, PointerEventHandler } from 'react'
import type { GNode } from '@/lib/priorities/derive'
import { Block } from './Block'
import { NODE_H, NODE_W, type DepsOrder } from './layout'

interface Props {
  node: GNode
  // Milestone colour, or NO_MILESTONE_COLOR.
  color: string
  // External card: another repo or outside the filter.
  grey: boolean
  // `nr` or `repo nr`.
  label: string
  testId: string
  dimmed: boolean
  highlighted: boolean
  // The sort order of the view; the pill shows its number. Missing counts as 'crossings'.
  depsOrder?: DepsOrder
  style?: CSSProperties
  onPointerDown?: PointerEventHandler<HTMLAnchorElement>
  onPointerEnter?: PointerEventHandler<HTMLAnchorElement>
  onMouseEnter?: MouseEventHandler<HTMLAnchorElement>
  onMouseLeave?: MouseEventHandler<HTMLAnchorElement>
  onClick?: MouseEventHandler<HTMLAnchorElement>
}

// The number the pill shows for a sort order; 0 means no pill.
function pillValue(node: GNode, order: DepsOrder): number {
  if (order === 'prio') return node.prioWeight
  if (order === 'weight') return node.weight
  return node.waiting >= 2 ? node.waiting : 0
}

// One issue card: a solid milestone strip, one status block of the shape language before the
// number (none on an external card, which has a solid grey border instead).
export function NodeCard({
  node,
  color,
  grey,
  label,
  testId,
  dimmed,
  highlighted,
  depsOrder,
  style,
  onPointerDown,
  onPointerEnter,
  onMouseEnter,
  onMouseLeave,
  onClick,
}: Props): JSX.Element {
  const pill = pillValue(node, depsOrder ?? 'crossings')
  const underlined = node.state === 'needs_mind' || node.state === 'needs_hands'
  return (
    <a
      href={grey && node.url === '' ? undefined : node.url}
      target="_blank"
      rel="noreferrer"
      data-testid={testId}
      data-state={node.state}
      data-external={grey ? 'true' : 'false'}
      data-dimmed={dimmed ? 'true' : 'false'}
      data-depth={node.depth}
      data-cycle={node.cycle ? 'true' : 'false'}
      className={[
        'flex items-center gap-2 overflow-hidden rounded-lg bg-white pr-2.5 text-foreground no-underline transition-opacity dark:bg-card',
        grey ? 'border border-lh-grey' : 'border border-border',
        dimmed ? 'opacity-25' : '',
        highlighted ? 'shadow-[0_0_0_2px_var(--lh-navy)]' : '',
      ].join(' ')}
      style={{
        width: NODE_W,
        height: NODE_H,
        ...style,
      }}
      onPointerDown={onPointerDown}
      onPointerEnter={onPointerEnter}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      onClick={onClick}
    >
      <span data-testid="card-strip" className="w-1.5 flex-none self-stretch" style={{ background: color }} />
      {!grey && <Block testId="card-status" className="size-3.5 flex-none" state={node.state} blocked={node.blocked} color={color} title={node.title} />}
      <span
        data-underlined={underlined && !grey ? 'true' : undefined}
        className={`flex-none text-[13px] font-semibold${underlined && !grey ? ' underline underline-offset-2 decoration-2' : ''}`}
      >
        {label}
      </span>
      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={node.title}>
        {node.title}
      </span>
      {node.cycle && <span className="flex-none text-[10.5px] font-semibold text-muted-foreground">cycle</span>}
      {pill > 0 && (
        <span
          data-testid="card-pill"
          title={`${node.waiting} wait · weight ${node.weight} · factor ${node.factor} · prio-weight ${node.prioWeight}`}
          className="flex-none rounded-full bg-lh-navy px-1.5 text-[11px] font-semibold text-white"
        >
          {pill}
        </span>
      )}
    </a>
  )
}
