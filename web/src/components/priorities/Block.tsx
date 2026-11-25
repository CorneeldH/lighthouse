import type { CSSProperties, JSX } from 'react'
import type { IssueStateName } from '@/lib/priorities/derive'

export type BlockState = IssueStateName

interface BlockProps {
  state: BlockState
  // A blockade lies on the issue: the cross over the shape.
  blocked?: boolean
  // Milestone colour: fill of a done block, stripe colour of an active one.
  color: string
  title: string
  href?: string
  // Number shown inside the block; undefined when it does not fit.
  label?: number | string
  // Outside blockers: solid border in the colour of their own milestone.
  borderColor?: string
  testId?: string
  className?: string
}

const STRIPES = { className: 'lh-stripe', style: (c: string): CSSProperties => ({ ['--ms-color' as string]: c }) }

const SHAPE: Record<BlockState, { className: string; style: (color: string) => CSSProperties }> = {
  done: { className: '', style: (c) => ({ background: c }) },
  active: STRIPES,
  ready: { className: 'bg-[var(--lh-seg-todo)]', style: () => ({}) },
  // The dashed line is an outline, so a solid borderColor (outside blockers) cannot replace it.
  needs_mind: {
    className: 'bg-[var(--background)]',
    style: () => ({ outline: '1.5px dashed var(--foreground)', outlineOffset: '-1.5px' }),
  },
  needs_hands: {
    className: STRIPES.className,
    style: (c) => ({ ...STRIPES.style(c), boxShadow: 'inset 0 0 0 2px var(--foreground)' }),
  },
}

// A vector cross corner to corner: it stretches with the block and keeps its stroke width.
function Cross(): JSX.Element {
  return (
    <svg
      aria-hidden
      viewBox="0 0 10 10"
      preserveAspectRatio="none"
      className="pointer-events-none absolute inset-0 size-full"
      style={{ stroke: 'var(--lh-cross)' }}
    >
      <line x1="0" y1="0" x2="10" y2="10" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
      <line x1="10" y1="0" x2="0" y2="10" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

// One block of the shape language, shared by the milestone bar, the outside column and the legend.
export function Block({
  state,
  blocked = false,
  color,
  title,
  href,
  label,
  borderColor,
  testId = 'block',
  className = '',
}: BlockProps): JSX.Element {
  const shape = SHAPE[state]
  const style: CSSProperties = {
    ...shape.style(color),
    ...(borderColor ? { border: `1.5px solid ${borderColor}` } : {}),
  }
  const cls = `${blocked ? 'relative ' : ''}flex items-center justify-center overflow-hidden rounded-sm text-[9.5px] font-bold ${shape.className} ${className}`
  const underlined = state === 'needs_mind' || state === 'needs_hands'
  const chip =
    label === undefined ? null : (
      <span
        data-underlined={underlined ? 'true' : undefined}
        className={`relative rounded-sm bg-[var(--lh-chip-bg)] px-0.5 text-[var(--lh-chip-fg)]${
          underlined ? ' underline underline-offset-2 decoration-2' : ''
        }`}
      >
        {label}
      </span>
    )
  const inner = (
    <>
      {blocked && <Cross />}
      {chip}
    </>
  )
  const common = {
    'data-testid': testId,
    'data-state': state,
    title,
    className: cls,
    style,
  }
  return href ? (
    <a {...common} href={href} target="_blank" rel="noreferrer">
      {inner}
    </a>
  ) : (
    <i {...common}>{inner}</i>
  )
}
