import type { CSSProperties, JSX, MouseEventHandler, PointerEventHandler } from 'react'
import type { GNode } from '@/lib/priorities/derive'
import { NODE_H, NODE_W } from './layout'

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
  style?: CSSProperties
  onPointerDown?: PointerEventHandler<HTMLAnchorElement>
  onPointerEnter?: PointerEventHandler<HTMLAnchorElement>
  onMouseEnter?: MouseEventHandler<HTMLAnchorElement>
  onMouseLeave?: MouseEventHandler<HTMLAnchorElement>
  onClick?: MouseEventHandler<HTMLAnchorElement>
}

// One issue card, drawn with the shape language of the milestone bar: striped strip = active,
// dashed outline = needs a human, cross at the number = blocked, solid grey border = external.
export function NodeCard({
  node,
  color,
  grey,
  label,
  testId,
  dimmed,
  highlighted,
  style,
  onPointerDown,
  onPointerEnter,
  onMouseEnter,
  onMouseLeave,
  onClick,
}: Props): JSX.Element {
  // needsHuman is only true on an active issue; a non-active one carries it in its state.
  const showsNeedsHuman = (node.needsHuman || node.state === 'needs_human') && !grey
  const active = node.state === 'active'
  const blocked = node.state === 'blocked'
  return (
    <a
      href={grey && node.url === '' ? undefined : node.url}
      target="_blank"
      rel="noreferrer"
      data-testid={testId}
      data-state={node.state}
      data-needs-human={showsNeedsHuman ? 'true' : 'false'}
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
        ...(showsNeedsHuman ? { outline: '1.5px dashed var(--foreground)', outlineOffset: -1 } : {}),
        ...style,
      }}
      onPointerDown={onPointerDown}
      onPointerEnter={onPointerEnter}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      onClick={onClick}
    >
      <span
        data-testid="card-strip"
        className={`w-1.5 flex-none self-stretch ${active ? 'lh-stripe' : ''}`}
        style={active ? ({ ['--ms-color' as string]: color } as CSSProperties) : { background: color }}
      />
      {blocked && <span data-testid="card-cross" className="lh-cross inline-block size-3 flex-none" />}
      <span className="flex-none text-[13px] font-semibold">{label}</span>
      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={node.title}>
        {node.title}
      </span>
      {node.cycle && <span className="flex-none text-[10.5px] font-semibold text-muted-foreground">cycle</span>}
      {node.waiting >= 2 && (
        <span
          data-testid="waiting-pill"
          title={`${node.waiting} issues wait on this`}
          className="flex-none rounded-full bg-lh-navy px-1.5 text-[11px] font-semibold text-white"
        >
          {node.waiting}
        </span>
      )}
    </a>
  )
}
