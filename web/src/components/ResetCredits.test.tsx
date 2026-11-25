import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { ResetCredits } from './ResetCredits'

afterEach(cleanup)

const now = new Date('2026-11-25T12:00:00Z')
const t1 = '2026-12-09T12:00:00Z'
const t2 = '2026-12-20T12:00:00Z'

describe('ResetCredits', () => {
  it('renders nothing for unknown credits', () => {
    const { container } = render(<ResetCredits credits={null} now={now} />)
    expect(container.textContent).toBe('')
  })

  it('renders nothing, and no 0, when no reset is available', () => {
    const { container } = render(<ResetCredits credits={{ available: 0, expires: [] }} now={now} />)
    expect(container.textContent).toBe('')
    expect(container.textContent).not.toContain('0')
  })

  it('shows the count and the day and month of the first expiry only', () => {
    const { container } = render(<ResetCredits credits={{ available: 2, expires: [t1, t2] }} now={now} />)
    const text = container.textContent ?? ''
    expect(text).toContain('2 resets available')
    expect(text).toContain('first expires Dec 9')
    expect(text).not.toContain('Dec 20')
    expect(text).not.toMatch(/\d\d:\d\d/)
  })

  it('omits the expiry when none is known', () => {
    const { container } = render(<ResetCredits credits={{ available: 1, expires: [] }} now={now} />)
    const text = container.textContent ?? ''
    expect(text).toContain('1 reset available')
    expect(text).not.toContain('expires')
  })
})
