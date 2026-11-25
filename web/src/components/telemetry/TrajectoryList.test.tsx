import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ListRow } from '@/api/lighthouse'
import { newQueryClient } from '@/lib/queries'
import { TelemetryTab } from './TelemetryTab'
import { TrajectoryDetail } from './TrajectoryDetail'
import { TrajectoryList } from './TrajectoryList'

const clients: ReturnType<typeof newQueryClient>[] = []

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((c) => c.clear())
  vi.unstubAllGlobals()
  window.location.hash = ''
})

const row = (over: Partial<ListRow>): ListRow => ({
  id: 1,
  repo: 'a/b',
  issue: 10,
  size: 'M',
  components: ['plan'],
  cost: 1,
  cost_kind: 'estimated',
  recovery: 'none',
  closed: true,
  ...over,
})

const json = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

function mount(ui: React.ReactElement, handler: (url: string) => Response | Promise<Response>) {
  const fetchMock = vi.fn().mockImplementation((url: string) => Promise.resolve(handler(String(url))))
  vi.stubGlobal('fetch', fetchMock)
  const client = newQueryClient()
  clients.push(client)
  render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
  return fetchMock
}

const bodyRows = (): HTMLElement[] => screen.getAllByRole('row').slice(1)

describe('TrajectoryList', () => {
  const rows = [row({ id: 1, issue: 1, cost: null }), row({ id: 2, issue: 2, cost: 5 }), row({ id: 3, issue: 3, cost: 2 })]

  it('sorts cost with a null cost always last and shows it as not measured', async () => {
    mount(<TrajectoryList filter={{}} />, () => json({ trajectories: rows }))
    await screen.findByText('not measured')
    const issues = (): string[] => bodyRows().map((r) => within(r).getAllByRole('cell')[1].textContent ?? '')
    fireEvent.click(screen.getByRole('button', { name: /cost/i }))
    expect(issues()).toEqual(['3', '2', '1'])
    fireEvent.click(screen.getByRole('button', { name: /cost/i }))
    expect(issues()).toEqual(['2', '3', '1'])
  })

  it('links each row to its trajectory', async () => {
    mount(<TrajectoryList filter={{}} />, () => json({ trajectories: rows }))
    await screen.findByText('not measured')
    const links = screen.getAllByRole('link').map((a) => a.getAttribute('href'))
    expect(links).toEqual(['#telemetry/trajectory/1', '#telemetry/trajectory/2', '#telemetry/trajectory/3'])
  })

  it('puts the filter and the component select in the call', async () => {
    const f = mount(<TrajectoryList filter={{ repo: 'a/b' }} />, () => json({ trajectories: rows }))
    await screen.findByText('not measured')
    fireEvent.change(screen.getByRole('combobox', { name: /component/i }), { target: { value: 'plan' } })
    await waitFor(() => expect(f.mock.calls.some((c) => String(c[0]) === '/api/telemetry/trajectories?repo=a%2Fb&component=plan')).toBe(true))
  })
})

describe('TrajectoryDetail', () => {
  const detail = {
    id: 7,
    repo: 'a/b',
    issue: 10,
    runs: [{ id: 1, assignment: 'work', outcome: 'ok', started_at: 's', ended_at: null, usage: null }],
    sessions: [],
    reviews: [1, 2, 3].map((i) => ({
      id: i,
      component: 'plan',
      finding_count: 1,
      outcome: 'done',
      ran_at: 'x',
      record_path: `rec/${i}.json`,
      findings: [{ component: 'plan', decision: 'adopted', key: `K${i}`, record_path: `rec/${i}.json`, severity: 'high' }],
    })),
    findings: [],
    recoveries: [{ pr: 5, status: 'recovered', merged_at: 'm', items: [{ at: 'a', kind: 'fix', ref: 'r1' }] }],
    events: [],
    questions: { dispatcher: 2, transcript: 3 },
    by_cause: { review: null, work: 4 },
  }

  it('shows findings of three reviews with record_path and no text', async () => {
    mount(<TrajectoryDetail id={7} />, () => json(detail))
    await screen.findByText('K1')
    expect(screen.getByText('K3')).toBeTruthy()
    expect(screen.getAllByText('rec/2.json').length).toBeGreaterThan(0)
    expect(document.body.textContent).not.toMatch(/"text"/)
    for (const h of ['Runs', 'Sessions', 'Reviews', 'Recovery', 'Events', 'Time by cause']) {
      expect(screen.getByRole('heading', { name: h })).toBeTruthy()
    }
    expect(screen.getByText('review: not measured')).toBeTruthy()
    expect(screen.getByText(/dispatcher: 2/)).toBeTruthy()
    expect(screen.getByText(/transcript: 3/)).toBeTruthy()
  })

  it('shows Trajectory not found on a 404 and goes back to the overview', async () => {
    window.location.hash = '#telemetry/trajectory/7'
    mount(<TrajectoryDetail id={7} />, () => json({ detail: 'nope' }, 404))
    await screen.findByText('Trajectory not found')
    fireEvent.click(screen.getByRole('button', { name: 'Back to overview' }))
    expect(window.location.hash).toBe('#telemetry')
  })
})

describe('TelemetryTab routing', () => {
  it('renders the detail for #telemetry/trajectory/7 and follows hashchange', async () => {
    window.location.hash = '#telemetry/trajectory/7'
    mount(<TelemetryTab />, (url) =>
      url.endsWith('/trajectories/7')
        ? json({ id: 7, repo: 'a/b', issue: 10, runs: null, sessions: null, reviews: null, findings: null, recoveries: null, events: null, questions: { dispatcher: 0, transcript: 0 }, by_cause: {} })
        : json({ trajectories: [], blocks: [], weeks: [], markers: [], options: {} }),
    )
    await screen.findByRole('heading', { name: 'Runs' })
    window.location.hash = '#telemetry'
    window.dispatchEvent(new HashChangeEvent('hashchange'))
    await screen.findByRole('region', { name: 'Trajectories' })
    expect(screen.queryByRole('heading', { name: 'Runs' })).toBeNull()
  })
})
