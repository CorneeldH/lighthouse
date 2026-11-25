import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CoverageReport, TimelineReport } from '@/api/lighthouse'
import { newQueryClient } from '@/lib/queries'
import { CoverageView } from './CoverageView'
import { TimelineView } from './TimelineView'

const clients: ReturnType<typeof newQueryClient>[] = []

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((c) => c.clear())
  vi.unstubAllGlobals()
})

function renderWith(body: unknown, ui: React.ReactElement) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  )
  const client = newQueryClient()
  clients.push(client)
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

const week = (start: string, cost: number | null, questions: number | null = null) => ({
  start,
  cost,
  questions,
  closed: null,
  recovered: null,
})

describe('TimelineView', () => {
  it('draws no rect for a null cost and a zero-height rect for cost 0', async () => {
    const body: TimelineReport = { weeks: [week('2026-01-05', null), week('2026-01-12', 0), week('2026-01-19', 4)], markers: [] }
    const { container } = renderWith(body, <TimelineView filter={{}} />)
    await screen.findByRole('img')
    expect(container.querySelector('rect[data-week="2026-01-05"]')).toBeNull()
    const zero = container.querySelector('rect[data-week="2026-01-12"]')
    expect(zero?.getAttribute('height')).toBe('0')
    expect(Number(container.querySelector('rect[data-week="2026-01-19"]')?.getAttribute('height'))).toBeGreaterThan(0)
  })

  it('draws a marker line with its text', async () => {
    const body: TimelineReport = {
      weeks: [week('2026-01-05', 1), week('2026-01-12', 2)],
      markers: [{ kind: 'release', text: 'v1.2 shipped', week: '2026-01-12' }],
    }
    const { container } = renderWith(body, <TimelineView filter={{}} />)
    await screen.findByRole('img')
    expect(container.querySelector('line[data-marker]')).not.toBeNull()
    expect(container.querySelector('svg')?.textContent).toContain('v1.2 shipped')
  })

  it('draws a circle for a single-week and an isolated questions point', async () => {
    const body: TimelineReport = { weeks: [week('2026-01-05', 1, 2)], markers: [] }
    const one = renderWith(body, <TimelineView filter={{}} />)
    await screen.findByRole('img')
    expect(one.container.querySelector('circle[data-questions="2026-01-05"]')).not.toBeNull()
    cleanup()
    const gap: TimelineReport = { weeks: [week('2026-01-05', 1, 2), week('2026-01-12', 1, null), week('2026-01-19', 1, 3)], markers: [] }
    const two = renderWith(gap, <TimelineView filter={{}} />)
    await screen.findByRole('img')
    expect(two.container.querySelectorAll('circle[data-questions]').length).toBe(2)
  })

  it('shows No data yet for empty weeks', async () => {
    renderWith({ weeks: null, markers: null }, <TimelineView filter={{}} />)
    expect(await screen.findByText('No data yet')).toBeTruthy()
  })

  it('offers the same numbers in an sr-only table', async () => {
    const body: TimelineReport = { weeks: [week('2026-01-05', null, 3), week('2026-01-12', 0, 1)], markers: [] }
    const { container } = renderWith(body, <TimelineView filter={{}} />)
    await screen.findByRole('img')
    const table = container.querySelector('table.sr-only')
    expect(table).not.toBeNull()
    expect(table?.textContent).toContain('2026-01-12')
    expect(table?.textContent).toContain('not measured')
  })
})

describe('CoverageView', () => {
  it('shows not measured for a null count, never 0', async () => {
    const body: CoverageReport = {
      not_migrated: false,
      lines: [
        { key: 'cost', reason: 'Cost per run', count: null, note: 'no cost recorded' },
        { key: 'runs', reason: 'Runs seen', count: 7, note: '' },
      ],
    }
    renderWith(body, <CoverageView filter={{}} />)
    expect(await screen.findByText('Cost per run')).toBeTruthy()
    expect(screen.getByText('not measured')).toBeTruthy()
    expect(screen.getByText('no cost recorded')).toBeTruthy()
    expect(screen.getByText('7')).toBeTruthy()
    expect(screen.queryByText('0')).toBeNull()
  })

  it('shows Loading while pending and a message for empty lines', async () => {
    renderWith({ lines: [], not_migrated: false }, <CoverageView filter={{}} />)
    expect(screen.getByText('Loading…')).toBeTruthy()
    expect(await screen.findByText('No coverage lines')).toBeTruthy()
    expect(screen.queryByText('Loading…')).toBeNull()
  })

  it('shows the not-migrated message', async () => {
    renderWith({ lines: null, not_migrated: true }, <CoverageView filter={{}} />)
    expect(await screen.findByText('database not migrated')).toBeTruthy()
  })
})
