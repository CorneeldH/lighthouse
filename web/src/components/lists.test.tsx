import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { splitDeferred } from '@/lib/deferred'
import { newQueryClient } from '@/lib/queries'
import { CleanupList } from './CleanupList'
import { QuestionList } from './QuestionList'
import { ReturnedList } from './ReturnedList'
import { NightBlock, ParkedBlock, QueueBlock, RunningBlock } from './RunBlocks'

let client: QueryClient | undefined

afterEach(() => {
  cleanup()
  client?.clear()
  client = undefined
  vi.unstubAllGlobals()
})

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': status === 200 ? 'application/json' : 'application/problem+json' },
  })
}

function stubFetch(make: () => Response) {
  vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(make())))
}

function renderWithClient(ui: React.ReactElement) {
  client = newQueryClient()
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

const questions = [
  { repo: 'o/a', number: 7, title: 'First', url: 'https://x/7', question: 'why?', answer_url: 'https://x/7#a' },
  { repo: 'o/b', number: 9, title: 'Second', url: 'https://x/9', question: 'how?', answer_url: 'https://x/9#a' },
]

describe('QuestionList', () => {
  it('shows repo and number, title link, question text and answer link per item', async () => {
    stubFetch(() => json({ questions }))
    renderWithClient(<QuestionList />)

    expect(await screen.findByText('o/a#7')).toBeTruthy()
    const region = screen.getByRole('region', { name: 'Open questions' })
    expect(region.id).toBe('questions')
    expect(within(region).getByText('2')).toBeTruthy()
    expect(screen.getByText('o/b#9')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'First' }).getAttribute('href')).toBe('/api/latest-comment?repo=o%2Fa&issue=7')
    expect(screen.getByRole('link', { name: 'o/a#7' }).getAttribute('href')).toBe('/api/latest-comment?repo=o%2Fa&issue=7')
    expect(screen.getByRole('link', { name: 'Second' }).getAttribute('href')).toBe('/api/latest-comment?repo=o%2Fb&issue=9')
    expect(screen.getByText('why?')).toBeTruthy()
    expect(screen.getByText('how?')).toBeTruthy()
    const answers = screen.getAllByRole('link', { name: 'Answer' })
    expect(answers.map((a) => a.getAttribute('href'))).toEqual(['https://x/7#a', 'https://x/9#a'])
    for (const a of answers) {
      expect(a.getAttribute('target')).toBe('_blank')
      expect(a.getAttribute('rel')).toBe('noopener noreferrer')
    }
    expect(screen.getByRole('link', { name: 'First' }).getAttribute('target')).toBe('_blank')
  })

  it('renders question text as text, never as markup', async () => {
    const text = 'regel 1\nregel 2 <script>x</script>'
    stubFetch(() => json({ questions: [{ ...questions[0], question: text }] }))
    const { container } = renderWithClient(<QuestionList />)

    const el = await screen.findByText((_, node) => node?.textContent === text && node.children.length === 0)
    expect(el.className).toContain('whitespace-pre-wrap')
    expect(container.querySelector('script')).toBeNull()
  })

  it('shows the count pill 1 for a single question', async () => {
    stubFetch(() => json({ questions: [questions[0]] }))
    renderWithClient(<QuestionList />)
    const region = screen.getByRole('region', { name: 'Open questions' })
    await within(region).findByText('o/a#7')
    expect(within(region).getByText('1')).toBeTruthy()
  })

  it('shows the empty text', async () => {
    stubFetch(() => json({ questions: [] }))
    renderWithClient(<QuestionList />)
    expect(await screen.findByText('No open questions')).toBeTruthy()
  })

  it('shows the not-configured alert on 503', async () => {
    stubFetch(() => json({ detail: 'forge not configured' }, 503))
    renderWithClient(<QuestionList />)
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Forge not configured — set GH_TOKEN, and --repo or [dispatch] repos in host.toml',
    )
    expect(screen.queryByText('No open questions')).toBeNull()
  })

  it('keeps old items and shows an alert when a refetch fails with 502', async () => {
    let res = () => json({ questions })
    stubFetch(() => res())
    renderWithClient(<QuestionList />)
    expect(await screen.findByText('o/a#7')).toBeTruthy()

    res = () => json({ detail: 'bad gateway' }, 502)
    await act(async () => {
      await client?.refetchQueries()
    })

    expect(screen.getByText('o/a#7')).toBeTruthy()
    expect((await screen.findByRole('alert')).textContent).toMatch(/^Forge unreachable: /)
  })
})

describe('ReturnedList', () => {
  it('shows repo and number, title link and reason', async () => {
    stubFetch(() =>
      json({ returned: [{ repo: 'o/a', number: 3, title: 'Back', url: 'https://x/3', reason: 'unclear AC' }] }),
    )
    renderWithClient(<ReturnedList />)

    expect(await screen.findByText('o/a#3')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back' }).getAttribute('href')).toBe('/api/latest-comment?repo=o%2Fa&issue=3')
    expect(screen.getByRole('link', { name: 'o/a#3' }).getAttribute('href')).toBe('/api/latest-comment?repo=o%2Fa&issue=3')
    expect(screen.getByText('unclear AC')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back' }).getAttribute('target')).toBe('_blank')
  })

  it('shows the empty text', async () => {
    stubFetch(() => json({ returned: [] }))
    renderWithClient(<ReturnedList />)
    expect(await screen.findByText('No returned issues')).toBeTruthy()
  })

  it('shows the not-configured alert on 503', async () => {
    stubFetch(() => json({ detail: 'x' }, 503))
    renderWithClient(<ReturnedList />)
    expect((await screen.findByRole('alert')).textContent).toBe(
      'Forge not configured — set GH_TOKEN, and --repo or [dispatch] repos in host.toml',
    )
  })
})

describe('CleanupList', () => {
  it('shows repo, issue, workspace id, path and reason', async () => {
    stubFetch(() =>
      json({
        cleanup: [
          { repo: 'o/r', issue: 7, workspace_id: 'ws-1', path: '/w/ws-1', reason: 'dirty worktree' },
        ],
      }),
    )
    renderWithClient(<CleanupList />)

    expect(await screen.findByText('o/r#7')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'o/r#7' }).getAttribute('href')).toBe('/api/latest-comment?repo=o%2Fr&issue=7')
    expect(screen.getByRole('link', { name: 'o/r#7' }).getAttribute('target')).toBe('_blank')
    expect(screen.getByText('ws-1')).toBeTruthy()
    const path = screen.getByText('/w/ws-1')
    expect(path.tagName).toBe('CODE')
    expect(screen.getByText('dirty worktree')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Workspaces to clean up' })).toBeTruthy()
  })

  it('shows the empty text', async () => {
    stubFetch(() => json({ cleanup: [] }))
    renderWithClient(<CleanupList />)
    expect(await screen.findByText('No workspaces to clean up')).toBeTruthy()
  })

  it('shows a neutral alert on a 500, not the forge text', async () => {
    stubFetch(() => json({ detail: 'db closed' }, 500))
    renderWithClient(<CleanupList />)
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toBe('Cleanup list unavailable: db closed')
    expect(alert.textContent).not.toContain('Forge unreachable')
  })
})

const run = {
  repo: 'o/a',
  issue: 3,
  title: 'Add the runs card',
  state: 'open',
  command: '/lighthouse-start-issue',
  trigger: 'board',
  profile: 'software',
  workspace_id: 'ws-3',
  started_at: '2026-11-01T09:00:00Z',
  stand: 'working',
  at: '2026-11-01T09:05:00Z',
  phase: 'build',
  task: 3,
  tasks: 7,
  step: 'build',
}

function body(over: Record<string, unknown> = {}) {
  return { runs: [run], deferred: [], workspace_url: 'http://127.0.0.1:8091', night: '21:00-08:00', ...over }
}

function deferred(issue: number, reason: string) {
  return { repo: 'o/a', issue, title: `Deferred ${issue}`, reason, at: '2026-11-01T09:05:00Z' }
}

async function renderRunning(b: unknown = body()) {
  stubFetch(() => json(b))
  renderWithClient(<RunningBlock />)
  const region = screen.getByRole('region', { name: 'Running runs' })
  await within(region).findByText('Add the runs card').catch(() => undefined)
  return region
}

describe('RunningBlock', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-11-01T11:05:00Z'))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows progress, stand badge and duration of an SDD run', async () => {
    const region = await renderRunning()
    expect(within(region).getByText('3/7')).toBeTruthy()
    expect(within(region).getByLabelText('task 3 of 7 · build')).toBeTruthy()
    expect(within(region).getByText('working')).toBeTruthy()
    expect(within(region).getByText('2 h 5 min')).toBeTruthy()
    expect(within(region).getByText('2 h 5 min').closest('[title]')?.getAttribute('title')).toMatch(/^running since \d\d:\d\d$/)
  })

  it('shows review and final review progress', async () => {
    const region = await renderRunning(
      body({ runs: [{ ...run, step: 'final-review', task: 5, tasks: 5 }] }),
    )
    expect(within(region).getByText('5/5')).toBeTruthy()
    expect(within(region).getByLabelText('final review')).toBeTruthy()
  })

  it('labels a review step', async () => {
    const region = await renderRunning(body({ runs: [{ ...run, step: 'review', task: 5, tasks: 6 }] }))
    expect(within(region).getByLabelText('task 5 of 6 · review')).toBeTruthy()
  })

  it('shows only the phase when there are no tasks', async () => {
    const region = await renderRunning(body({ runs: [{ ...run, tasks: 0, task: 0, step: '', phase: 'plan-review' }] }))
    expect(within(region).getByText('plan-review')).toBeTruthy()
    expect(within(region).queryByText(/^\d+\/\d+$/)).toBeNull()
  })

  it('shows the done and not-measured stands', async () => {
    const region = await renderRunning(
      body({ runs: [{ ...run, stand: 'done' }, { ...run, issue: 4, stand: '' }] }),
    )
    const done = within(region).getByText('done · next tick decides')
    expect(done.closest('[title]')?.getAttribute('title')).toBe(
      'Claude finished its turn; the next tick decides: handoff, done, question or returned',
    )
    expect(within(region).getByText('not measured')).toBeTruthy()
  })

  it('links the workspace in a new tab', async () => {
    const region = await renderRunning()
    const link = within(region).getByRole('link', { name: 'ws-3, opens kenn-forge in a new tab' })
    expect(link.getAttribute('href')).toBe('http://127.0.0.1:8091/workspaces/ws-3')
    expect(link.getAttribute('target')).toBe('_blank')
  })

  it('shows the workspace without a link when there is no url', async () => {
    const region = await renderRunning(body({ workspace_url: '' }))
    const code = within(region).getByText('ws-3')
    expect(code.tagName).toBe('CODE')
    expect(within(region).queryByRole('link', { name: /ws-3/ })).toBeNull()
  })

  it('keeps the command in the tooltip, not in the visible row', async () => {
    const region = await renderRunning()
    const tip = within(region).getByRole('tooltip')
    expect(within(tip).getByText('/lighthouse-start-issue')).toBeTruthy()
    expect(tip.textContent).toContain('board')
    expect(tip.textContent).toContain('software')
    expect(tip.textContent).toMatch(/started \d\d:\d\d/)
    expect(within(region).getByRole('button', { name: 'Run details' }).getAttribute('aria-describedby')).toBe(tip.id)
    const row = within(region).getByRole('listitem')
    const clone = row.cloneNode(true) as HTMLElement
    clone.querySelector('[role="tooltip"]')?.remove()
    expect(clone.textContent).not.toContain('/lighthouse-start-issue')
  })

  it('shows no open or parked chip', async () => {
    const region = await renderRunning()
    const row = within(region).getByRole('listitem')
    expect(within(row).queryByText('open')).toBeNull()
    expect(within(row).queryByText('parked')).toBeNull()
  })

  it('keeps a parked run out of Running and in Parked', async () => {
    stubFetch(() => json(body({ runs: [{ ...run, state: 'parked' }] })))
    renderWithClient(
      <>
        <RunningBlock />
        <ParkedBlock />
      </>,
    )
    const parked = screen.getByRole('region', { name: 'Parked runs' })
    expect(await within(parked).findByText('o/a#3')).toBeTruthy()
    const running = screen.getByRole('region', { name: 'Running runs' })
    expect(within(running).queryByText('o/a#3')).toBeNull()
    expect(within(running).getByText('No running runs')).toBeTruthy()
    expect(within(parked).getByText(/Stopped mid-run by the usage brake/)).toBeTruthy()
  })

  it('shows the empty texts', async () => {
    stubFetch(() => json(body({ runs: [] })))
    renderWithClient(
      <>
        <RunningBlock />
        <ParkedBlock />
        <QueueBlock />
        <NightBlock />
      </>,
    )
    expect(await screen.findByText('No running runs')).toBeTruthy()
    expect(screen.getByText('No parked runs')).toBeTruthy()
    expect(screen.getByText('Nothing in the queue')).toBeTruthy()
    expect(screen.getByText('Nothing waiting for the night')).toBeTruthy()
  })

  it('shows the runs error in Running and a short text elsewhere', async () => {
    stubFetch(() => json({ detail: 'db closed' }, 500))
    renderWithClient(
      <>
        <RunningBlock />
        <QueueBlock />
      </>,
    )
    expect(await within(screen.getByRole('region', { name: 'Running runs' })).findByRole('alert')).toBeTruthy()
    expect(screen.getAllByRole('alert').map((a) => a.textContent)).toEqual([
      'Runs unavailable: db closed',
      'Runs unavailable',
    ])
  })
})

describe('Queue and Night', () => {
  const list = [deferred(4, 'night'), deferred(5, 'night window'), deferred(6, 'slots: 4 of 4 in use')]

  it('splits deferred on the exact reason night', () => {
    const { queue, night } = splitDeferred(list)
    expect(night.map((d) => d.issue)).toEqual([4])
    expect(queue.map((d) => d.issue)).toEqual([5, 6])
  })

  it('shows queue rows with the reason badge and night rows apart', async () => {
    stubFetch(() => json(body({ deferred: list })))
    renderWithClient(
      <>
        <QueueBlock />
        <NightBlock />
      </>,
    )
    const queue = screen.getByRole('region', { name: 'Queue' })
    const night = screen.getByRole('region', { name: 'Night' })
    expect(await within(queue).findAllByRole('listitem')).toHaveLength(2)
    expect(within(night).getAllByRole('listitem')).toHaveLength(1)
    expect(within(queue).getByText('slots: 4 of 4 in use')).toBeTruthy()
    expect(within(queue).getByText('night window')).toBeTruthy()
    expect(within(queue).getByText('Ready to start, held by a brake. The next tick with room starts it.')).toBeTruthy()
    expect(within(night).getByText('Deferred 4')).toBeTruthy()
    expect(within(night).getByText('Handed off for the night window. Starts from 21:00.')).toBeTruthy()
  })

  it('omits the start time when night is empty', async () => {
    stubFetch(() => json(body({ deferred: list, night: '' })))
    renderWithClient(<NightBlock />)
    expect(await screen.findByText('Handed off for the night window.')).toBeTruthy()
  })
})
