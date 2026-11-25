import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { IssueStateName } from '@/lib/priorities/derive'
import { Block } from './Block'

afterEach(cleanup)

const STATES: IssueStateName[] = ['done', 'active', 'ready', 'needs_mind', 'needs_hands']
const UNDERLINED: IssueStateName[] = ['needs_mind', 'needs_hands']

const block = () => screen.getByTestId('block')

describe('Block chip', () => {
  it.each(STATES)('shows the bare number on a light chip (%s)', (state) => {
    render(<Block state={state} color="#2f5bd8" title="t" label={1234} />)
    const chip = screen.getByText('1234')
    expect(chip.textContent).toBe('1234')
    expect(chip.className).toContain('bg-[var(--lh-chip-bg)]')
    expect(chip.className).toContain('text-[var(--lh-chip-fg)]')
    expect(chip.className).not.toContain('bg-background/70')
  })
})

describe('Block shapes', () => {
  it.each(STATES)('sets data-state and no data-needs-human (%s)', (state) => {
    render(<Block state={state} color="#2f5bd8" title="t" label={7} />)
    expect(block().getAttribute('data-state')).toBe(state)
    expect(block().hasAttribute('data-needs-human')).toBe(false)
  })

  it.each(STATES)('underlines the number only for needs_mind and needs_hands (%s)', (state) => {
    render(<Block state={state} color="#2f5bd8" title="t" label={7} />)
    const num = screen.getByText('7')
    if (UNDERLINED.includes(state)) {
      expect(num.getAttribute('data-underlined')).toBe('true')
      expect(num.className).toContain('underline underline-offset-2 decoration-2')
    } else {
      expect(num.hasAttribute('data-underlined')).toBe(false)
      expect(num.className).not.toContain('underline')
    }
  })

  it('draws needs_mind with a 1.5px dashed foreground outline, kept next to a solid border', () => {
    render(<Block state="needs_mind" color="#2f5bd8" title="t" borderColor="rgb(18, 52, 86)" />)
    expect(block().style.outline).toBe('1.5px dashed var(--foreground)')
    expect(block().style.outlineOffset).toBe('-1.5px')
    expect(block().style.border).toBe('1.5px solid rgb(18, 52, 86)')
  })

  it('draws needs_hands as stripes with an inset 2px foreground shadow', () => {
    render(<Block state="needs_hands" color="#2f5bd8" title="t" />)
    expect(block().className).toContain('lh-stripe')
    expect(block().style.boxShadow).toBe('inset 0 0 0 2px var(--foreground)')
    expect(block().style.getPropertyValue('--ms-color')).toBe('#2f5bd8')
  })

  it('draws a blockade as a vector cross that stretches with the block', () => {
    render(<Block state="ready" blocked color="#2f5bd8" title="t" label={3} />)
    const svg = block().querySelector('svg')!
    expect(svg.getAttribute('preserveAspectRatio')).toBe('none')
    const lines = svg.querySelectorAll('line')
    expect(lines).toHaveLength(2)
    lines.forEach((l) => expect(l.getAttribute('vector-effect')).toBe('non-scaling-stroke'))
  })

  it.each(['done', 'active', 'ready', 'needs_mind', 'needs_hands'] as IssueStateName[])(
    'draws no cross without a blockade (%s)',
    (state) => {
      render(<Block state={state} color="#2f5bd8" title="t" />)
      expect(block().querySelector('svg')).toBeNull()
    },
  )
})
