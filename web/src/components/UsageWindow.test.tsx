import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { WindowReport } from '@/api/lighthouse'
import { UsageWindow } from './UsageWindow'

afterEach(cleanup)

const now = new Date(2026, 9, 27, 10, 0)
const resetAt = new Date(2026, 9, 27, 14, 30).toISOString()

function win(over: Partial<WindowReport>): WindowReport {
  return { state: 'ok', pct: 42, lower_bound: false, age_s: 120, reset_at: resetAt, ...over }
}

describe('UsageWindow', () => {
  it('shows an ok window with its age and no badge', () => {
    render(<UsageWindow label="Claude session" window={win({})} now={now} />)
    expect(screen.getByText('42%')).toBeTruthy()
    expect(screen.getByText(/resets 14:30/)).toBeTruthy()
    expect(screen.getByText(/measured 2 min ago/)).toBeTruthy()
    expect(screen.queryByText(/stale/)).toBeNull()
    expect(screen.getAllByRole('progressbar')).toHaveLength(1)
    expect(screen.getByRole('progressbar', { name: 'Claude session' }).getAttribute('aria-valuenow')).toBe('42')
    expect(screen.queryByText('not measurable')).toBeNull()
  })

  it('shows 0% for a measured zero', () => {
    render(<UsageWindow label="Claude week" window={win({ pct: 0 })} now={now} />)
    expect(screen.getByText('0%')).toBeTruthy()
  })

  it('shows a stale badge with the age', () => {
    render(
      <UsageWindow label="Codex week" window={win({ state: 'stale', pct: 80, age_s: 3600 })} now={now} />,
    )
    expect(screen.getByText('80%')).toBeTruthy()
    expect(screen.getByText('stale · 1 h')).toBeTruthy()
    expect(screen.getAllByRole('progressbar')).toHaveLength(1)
  })

  it('shows a not-measurable window without a percentage', () => {
    const { container } = render(
      <UsageWindow
        label="Claude session"
        window={win({ state: 'not_measurable', pct: null, age_s: null, reason: 'no Claude usage cache' })}
        now={now}
      />,
    )
    expect(screen.getByText('not measurable')).toBeTruthy()
    expect(screen.getByText('no Claude usage cache')).toBeTruthy()
    expect(container.textContent).not.toMatch(/%/)
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  it('marks a lower bound', () => {
    render(<UsageWindow label="Codex session" window={win({ lower_bound: true })} now={now} />)
    expect(screen.getByText('≥ 42%').getAttribute('title')).toBe('lower bound')
  })
})
