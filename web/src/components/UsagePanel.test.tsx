import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WindowReport } from '@/api/lighthouse'
import { UsagePanel } from './UsagePanel'

const useUsage = vi.fn()
vi.mock('@/lib/queries', () => ({ useUsage: () => useUsage() }))
vi.mock('@/lib/now', () => ({ useNow: () => Date.parse('2026-11-25T12:00:00Z') }))

afterEach(cleanup)

const win: WindowReport = {
  age_s: null,
  lower_bound: false,
  pct: 10,
  reset_at: '2026-11-25T15:00:00Z',
  state: 'ok',
}

function usage(resetCredits: { available: number; expires: string[] } | null) {
  return {
    data: {
      claude: { session: win, week: win },
      codex: { session: win, week: win, reset_credits: resetCredits },
    },
    error: null,
    isPending: false,
  }
}

describe('UsagePanel', () => {
  it('shows the resets under Codex and not under Claude', () => {
    useUsage.mockReturnValue(usage({ available: 2, expires: ['2026-12-09T12:00:00Z'] }))
    render(<UsagePanel />)
    const el = screen.getByText(/2 resets available/)
    const codexWeek = screen.getByRole('group', { name: 'Codex week' })
    const claudeWeek = screen.getByRole('group', { name: 'Claude week' })
    expect(codexWeek.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(claudeWeek.contains(el)).toBe(false)
    expect(claudeWeek.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('omits the resets when they are unknown', () => {
    useUsage.mockReturnValue(usage(null))
    render(<UsagePanel />)
    expect(screen.queryByText(/available/)).toBeNull()
  })
})
