import type { CSSProperties, JSX } from 'react'

export type BlockState = 'done' | 'active' | 'ready' | 'blocked'

interface BlockProps {
  state: BlockState
  needsHuman: boolean
  // Milestone colour: fill of a done block, stripe colour of an active one.
  color: string
  title: string
  href?: string
  // Number shown inside the block; undefined when it does not fit.
  label?: number
  // Outside blockers: solid border in the colour of their own milestone.
  borderColor?: string
  testId?: string
  className?: string
}

const SHAPE: Record<BlockState, { className: string; style: (color: string) => CSSProperties }> = {
  done: { className: '', style: (c) => ({ background: c }) },
  active: { className: 'lh-stripe', style: (c) => ({ ['--ms-color' as string]: c }) },
  ready: { className: 'bg-[var(--lh-seg-todo)]', style: () => ({}) },
  blocked: { className: 'lh-cross', style: () => ({}) },
}

// One block of the shape language, shared by the milestone bar, the outside column and the legend.
export function Block({
  state,
  needsHuman,
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
    ...(needsHuman ? { outline: '1.5px dashed var(--foreground)' } : {}),
    ...(borderColor ? { border: `1.5px solid ${borderColor}` } : {}),
  }
  const cls = `flex items-center justify-center overflow-hidden rounded-sm text-[9.5px] font-bold ${shape.className} ${className}`
  const inner = label === undefined ? null : <span className="rounded-sm bg-[var(--lh-chip-bg)] px-0.5 text-[var(--lh-chip-fg)]">{label}</span>
  const common = {
    'data-testid': testId,
    'data-state': state,
    'data-needs-human': needsHuman ? 'true' : 'false',
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
