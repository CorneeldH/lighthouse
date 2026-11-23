import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { Block, type BlockState } from './Block'

afterEach(cleanup)

const CASES: [BlockState, boolean][] = [
  ['done', false],
  ['active', false],
  ['ready', false],
  ['blocked', false],
  ['ready', true],
]

describe('Block chip', () => {
  it.each(CASES)('shows the bare number on a light chip (%s, needsHuman %s)', (state, needsHuman) => {
    render(<Block state={state} needsHuman={needsHuman} color="#2f5bd8" title="t" label={1234} />)
    const chip = screen.getByText('1234')
    expect(chip.textContent).toBe('1234')
    expect(chip.className).toContain('bg-[var(--lh-chip-bg)]')
    expect(chip.className).toContain('text-[var(--lh-chip-fg)]')
    expect(chip.className).not.toContain('bg-background/70')
  })
})
